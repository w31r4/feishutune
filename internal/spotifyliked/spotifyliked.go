// Package spotifyliked reports whether a track is in the user's Liked Songs.
//
// The local Spotify AppleScript surface can't read liked status (its `starred`
// property is dead) and the public Web API isn't an option, so this package does
// what the Spotify web player does: from the long-lived `sp_dc` login cookie it
// mints a short-lived access token (via Spotify's TOTP-guarded /api/token) and a
// client-token, then asks the private GraphQL `areEntitiesInLibrary` whether each
// track is saved. Both short-lived tokens are cached on disk and auto-refreshed
// from `sp_dc`; `sp_dc` itself lasts ~1 year and is re-pasted via `spotify-login`.
//
// Every step is best-effort from the caller's side: callers treat any error as
// "not liked", so a hiccup or a rotated secret drops the ♡ rather than failing
// the bio. With no sp_dc configured the client is disabled and Liked is a no-op.
package spotifyliked

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Durden-T/feishutune/internal/bio"
)

const (
	tokenURL       = "https://open.spotify.com/api/token"
	clientTokenURL = "https://clienttoken.spotify.com/v1/clienttoken"
	pathfinderURL  = "https://api-partner.spotify.com/pathfinder/v2/query"

	// webClientID and webAppVersion identify the web player; the pathfinder 403s
	// requests missing the app-platform / spotify-app-version headers. libraryHash
	// is areEntitiesInLibrary's persisted-query id. All three track the web-player
	// build and may need re-capturing if requests start to 4xx.
	webClientID   = "d8a5ed958d274c2e8ee717e6a4b0971d"
	webAppVersion = "1.2.93.31.g4cc9048e"
	libraryHash   = "134337999233cc6fdd6b1e6dbf94841409f04a946c5c7b744b09ba0dfe5a85ed"

	userAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) " +
		"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36"

	trackPrefix = "spotify:track:"
)

// Client answers liked-status queries for one account, identified by its sp_dc
// cookie. An empty cookie disables lookups: Liked then always returns false.
type Client struct {
	spDC string
	http *http.Client
	now  func() time.Time

	// Endpoints, overridable in tests.
	tokenURL       string
	clientTokenURL string
	pathfinderURL  string
}

// New returns a Client authenticating with the given sp_dc cookie. An empty
// cookie yields a disabled client whose Liked is a no-op (false, nil).
func New(spDC string) *Client {
	return &Client{
		spDC:           strings.TrimSpace(spDC),
		http:           &http.Client{Timeout: 15 * time.Second},
		now:            time.Now,
		tokenURL:       tokenURL,
		clientTokenURL: clientTokenURL,
		pathfinderURL:  pathfinderURL,
	}
}

// Liked reports whether the track is in the user's Liked Songs, keyed by its
// Spotify URI (track.ID, "spotify:track:<base62>"). It returns false without
// error when lookups are disabled (no sp_dc) or the track is not a Spotify track
// (ad, local file, or a non-Spotify player). A still-fresh cached result skips
// the network; otherwise it refreshes tokens as needed, queries Spotify, and
// caches the result.
func (c *Client) Liked(ctx context.Context, track bio.Track) (bool, error) {
	trackURI := track.ID
	if c.spDC == "" || !strings.HasPrefix(trackURI, trackPrefix) {
		return false, nil
	}
	cache := loadCache()
	if saved, ok := cache.fresh(trackURI, c.now()); ok {
		return saved, nil
	}
	access, err := c.ensureAccessToken(ctx, cache)
	if err != nil {
		return false, err
	}
	clientTok, err := c.ensureClientToken(ctx, cache)
	if err != nil {
		return false, err
	}
	saved, err := c.queryLiked(ctx, access, clientTok, trackURI)
	if err != nil {
		return false, err
	}
	cache.Tracks[trackURI] = trackEntry{Saved: saved, CheckedAt: c.now()}
	_ = cache.save() // a failed cache write only costs a re-query next run
	return saved, nil
}

// ensureAccessToken returns a cached access token while it is still valid, else
// mints a fresh one from sp_dc and stores it back in cache.
func (c *Client) ensureAccessToken(ctx context.Context, cache *cache) (string, error) {
	if cache.AccessToken != "" && c.now().Before(cache.AccessExpiry.Add(-tokenSkew)) {
		return cache.AccessToken, nil
	}
	tok, exp, err := c.mintAccessToken(ctx)
	if err != nil {
		return "", err
	}
	cache.AccessToken, cache.AccessExpiry = tok, exp
	return tok, nil
}

// ensureClientToken returns a cached client-token while valid, else mints one
// against the cache's stable device id (generated once on first use).
func (c *Client) ensureClientToken(ctx context.Context, cache *cache) (string, error) {
	if cache.ClientToken != "" && c.now().Before(cache.ClientExpiry.Add(-tokenSkew)) {
		return cache.ClientToken, nil
	}
	if cache.DeviceID == "" {
		cache.DeviceID = randomDeviceID()
	}
	tok, exp, err := c.mintClientToken(ctx, cache.DeviceID)
	if err != nil {
		return "", err
	}
	cache.ClientToken, cache.ClientExpiry = tok, exp
	return tok, nil
}

func (c *Client) mintAccessToken(ctx context.Context) (string, time.Time, error) {
	code, err := totp(c.now())
	if err != nil {
		return "", time.Time{}, err
	}
	q := url.Values{
		"reason":      {"init"},
		"productType": {"web-player"},
		"totp":        {code},
		"totpServer":  {code},
		"totpVer":     {totpVersion},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.tokenURL+"?"+q.Encode(), nil)
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Cookie", "sp_dc="+c.spDC)
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Origin", "https://open.spotify.com")
	req.Header.Set("Referer", "https://open.spotify.com/")

	var r struct {
		AccessToken string `json:"accessToken"`
		ExpiresMs   int64  `json:"accessTokenExpirationTimestampMs"`
		IsAnonymous bool   `json:"isAnonymous"`
	}
	if err := c.doJSON(req, &r); err != nil {
		return "", time.Time{}, fmt.Errorf("spotify liked: mint access token: %w", err)
	}
	// An anonymous (or empty) token means sp_dc was not accepted — i.e. expired.
	if r.AccessToken == "" || r.IsAnonymous {
		return "", time.Time{}, fmt.Errorf("spotify liked: sp_dc rejected — run `feishutune spotify-login` with a fresh sp_dc cookie")
	}
	exp := time.UnixMilli(r.ExpiresMs)
	if r.ExpiresMs == 0 {
		exp = c.now().Add(5 * time.Minute)
	}
	return r.AccessToken, exp, nil
}

func (c *Client) mintClientToken(ctx context.Context, deviceID string) (string, time.Time, error) {
	body, err := json.Marshal(clientTokenRequest{ClientData: clientData{
		ClientVersion: webAppVersion,
		ClientID:      webClientID,
		JSSDKData:     jsSDKData{DeviceBrand: "Apple", OS: "macos", DeviceID: deviceID, DeviceType: "computer"},
	}})
	if err != nil {
		return "", time.Time{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.clientTokenURL, bytes.NewReader(body))
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	var r struct {
		GrantedToken struct {
			Token        string `json:"token"`
			ExpiresAfter int64  `json:"expires_after_seconds"`
			RefreshAfter int64  `json:"refresh_after_seconds"`
		} `json:"granted_token"`
	}
	if err := c.doJSON(req, &r); err != nil {
		return "", time.Time{}, fmt.Errorf("spotify liked: mint client-token: %w", err)
	}
	if r.GrantedToken.Token == "" {
		return "", time.Time{}, fmt.Errorf("spotify liked: empty client-token")
	}
	secs := r.GrantedToken.RefreshAfter
	if secs == 0 {
		secs = r.GrantedToken.ExpiresAfter
	}
	if secs == 0 {
		secs = 600
	}
	return r.GrantedToken.Token, c.now().Add(time.Duration(secs) * time.Second), nil
}

// queryLiked asks areEntitiesInLibrary whether trackURI is saved.
func (c *Client) queryLiked(ctx context.Context, access, clientTok, trackURI string) (bool, error) {
	body, err := json.Marshal(graphQLRequest{
		Variables:     variables{URIs: []string{trackURI}},
		OperationName: "areEntitiesInLibrary",
		Extensions:    extensions{PersistedQuery: persistedQuery{Version: 1, SHA256Hash: libraryHash}},
	})
	if err != nil {
		return false, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.pathfinderURL, bytes.NewReader(body))
	if err != nil {
		return false, err
	}
	req.Header.Set("authorization", "Bearer "+access)
	req.Header.Set("client-token", clientTok)
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json")
	req.Header.Set("app-platform", "WebPlayer")
	req.Header.Set("spotify-app-version", webAppVersion)
	req.Header.Set("User-Agent", userAgent)

	var r struct {
		Data struct {
			Lookup []struct {
				Data struct {
					Saved bool `json:"saved"`
				} `json:"data"`
			} `json:"lookup"`
		} `json:"data"`
	}
	if err := c.doJSON(req, &r); err != nil {
		return false, fmt.Errorf("spotify liked: areEntitiesInLibrary: %w", err)
	}
	if len(r.Data.Lookup) == 0 {
		return false, fmt.Errorf("spotify liked: areEntitiesInLibrary returned no lookup for %s", trackURI)
	}
	return r.Data.Lookup[0].Data.Saved, nil
}

// doJSON sends req, requires a 2xx, and decodes the JSON body into v. A non-2xx
// returns an error carrying the status and a short body snippet.
func (c *Client) doJSON(req *http.Request, v any) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// randomDeviceID returns a UUIDv4-shaped id, generated once and persisted so the
// client-token stays tied to a stable device like the web player's.
func randomDeviceID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "00000000-0000-4000-8000-000000000000"
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

type clientTokenRequest struct {
	ClientData clientData `json:"client_data"`
}

type clientData struct {
	ClientVersion string    `json:"client_version"`
	ClientID      string    `json:"client_id"`
	JSSDKData     jsSDKData `json:"js_sdk_data"`
}

type jsSDKData struct {
	DeviceBrand string `json:"device_brand"`
	OS          string `json:"os"`
	DeviceID    string `json:"device_id"`
	DeviceType  string `json:"device_type"`
}

type graphQLRequest struct {
	Variables     variables  `json:"variables"`
	OperationName string     `json:"operationName"`
	Extensions    extensions `json:"extensions"`
}

type variables struct {
	URIs []string `json:"uris"`
}

type extensions struct {
	PersistedQuery persistedQuery `json:"persistedQuery"`
}

type persistedQuery struct {
	Version    int    `json:"version"`
	SHA256Hash string `json:"sha256Hash"`
}
