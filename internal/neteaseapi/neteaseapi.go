// Package neteaseapi provides the optional NetEase Cloud Music Open Platform
// enhancement layer. The local desktop app remains the source of truth for
// now-playing state; this client only enriches metadata and liked status when
// credentials and verified endpoint paths are configured.
package neteaseapi

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Durden-T/feishutune/internal/bio"
	"github.com/Durden-T/feishutune/internal/neteaseauth"
)

const (
	defaultBaseURL = "https://openapi.music.163.com"
	trackPrefix    = "netease:track:"
	signType       = "RSA_SHA256"

	defaultDetailPath = "/openapi/music/basic/song/detail/get/v2"
	defaultSearchPath = "/openapi/music/basic/search/song/get/v2"

	envBaseURL    = "NETEASE_API_BASE_URL"
	envDetailPath = "NETEASE_API_SONG_DETAIL_PATH"
	envSearchPath = "NETEASE_API_SEARCH_PATH"
	envLikedPath  = "NETEASE_API_LIKED_PATH"
)

// Endpoints holds the Open Platform API paths used by this enhancement layer.
// Song detail and song search default to paths whose existence is verified
// against the Open Platform gateway. LikedSongs intentionally remains
// caller-configured until the red-heart playlist path is confirmed.
type Endpoints struct {
	SongDetail string
	Search     string
	LikedSongs string
}

// Client calls NetEase Open Platform APIs using user-provided credentials.
type Client struct {
	creds     neteaseauth.Credentials
	http      *http.Client
	baseURL   string
	endpoints Endpoints
	now       func() time.Time
	device    map[string]string
}

// Option customizes a Client, primarily for tests and for verified endpoint
// configuration.
type Option func(*Client)

// WithBaseURL overrides the Open Platform origin.
func WithBaseURL(s string) Option {
	return func(c *Client) {
		if s = strings.TrimRight(strings.TrimSpace(s), "/"); s != "" {
			c.baseURL = s
		}
	}
}

// WithEndpoints sets the Open Platform paths used by Enhance and LikedStatus.
func WithEndpoints(e Endpoints) Option {
	return func(c *Client) { c.endpoints = e.trimmed() }
}

// WithHTTPClient overrides the default HTTP client.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.http = h
		}
	}
}

// WithNow overrides the clock used for timestamps.
func WithNow(now func() time.Time) Option {
	return func(c *Client) {
		if now != nil {
			c.now = now
		}
	}
}

// New returns an API client. Missing credentials produce a disabled client.
func New(creds neteaseauth.Credentials, opts ...Option) *Client {
	c := &Client{
		creds:     creds,
		http:      &http.Client{Timeout: 15 * time.Second},
		baseURL:   envOrDefault(envBaseURL, defaultBaseURL),
		endpoints: endpointsFromEnv(),
		now:       time.Now,
		device: map[string]string{
			"deviceType": "mac",
			"os":         "macos",
			"appVer":     "feishutune",
			"channel":    "desktop",
			"model":      "mac",
			"brand":      "Apple",
			"osVer":      "macos",
			"deviceId":   "feishutune-mac",
			"clientIp":   "127.0.0.1",
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	c.baseURL = strings.TrimRight(c.baseURL, "/")
	c.endpoints = c.endpoints.trimmed()
	return c
}

// Enabled reports whether the required credentials are available. Individual
// operations also require their endpoint path to be configured.
func (c *Client) Enabled() bool { return c != nil && c.creds.Enabled() }

// Enhance fills a NetEase track id and standard metadata when configured. It
// returns the original track unchanged when disabled or when no relevant endpoint
// is configured.
func (c *Client) Enhance(ctx context.Context, track bio.Track) (bio.Track, error) {
	if !track.Playing || !c.Enabled() {
		return track, nil
	}
	if id, ok := trackID(track.ID); ok {
		if c.endpoints.SongDetail == "" {
			return track, nil
		}
		detail, err := c.SongDetail(ctx, id)
		if err != nil {
			return track, err
		}
		return merge(track, detail), nil
	}
	if c.endpoints.Search == "" || track.Name == "" || track.Artist == "" {
		return track, nil
	}
	match, err := c.Resolve(ctx, track)
	if err != nil {
		return track, err
	}
	out := merge(track, match)
	if id, ok := trackID(out.ID); ok && c.endpoints.SongDetail != "" {
		detail, err := c.SongDetail(ctx, id)
		if err != nil {
			return out, err
		}
		out = merge(out, detail)
	}
	return out, nil
}

// Resolve searches by title and artist and returns one strict metadata match.
func (c *Client) Resolve(ctx context.Context, track bio.Track) (bio.Track, error) {
	candidates, err := c.SearchSongs(ctx, track.Name+" "+track.Artist)
	if err != nil {
		return bio.Track{}, err
	}
	var matches []bio.Track
	for _, cand := range candidates {
		if metadataMatch(track, cand) {
			matches = append(matches, cand)
		}
	}
	switch len(matches) {
	case 0:
		return bio.Track{}, fmt.Errorf("netease api: no strict match for %q by %q", track.Name, track.Artist)
	case 1:
		return matches[0], nil
	default:
		return bio.Track{}, fmt.Errorf("netease api: ambiguous strict match for %q by %q", track.Name, track.Artist)
	}
}

// SongDetail fetches one song's standard metadata.
func (c *Client) SongDetail(ctx context.Context, id string) (bio.Track, error) {
	if !c.Enabled() {
		return bio.Track{}, errors.New("netease api: disabled")
	}
	if c.endpoints.SongDetail == "" {
		return bio.Track{}, errors.New("netease api: song detail endpoint not configured")
	}
	raw, err := c.call(ctx, c.endpoints.SongDetail, map[string]string{"songId": id})
	if err != nil {
		return bio.Track{}, fmt.Errorf("netease api: song detail: %w", err)
	}
	tracks := tracksFromRaw(raw)
	if len(tracks) == 0 {
		return bio.Track{}, fmt.Errorf("netease api: song detail returned no track")
	}
	return tracks[0], nil
}

// SearchSongs searches songs by keyword.
func (c *Client) SearchSongs(ctx context.Context, keyword string) ([]bio.Track, error) {
	if !c.Enabled() {
		return nil, errors.New("netease api: disabled")
	}
	if c.endpoints.Search == "" {
		return nil, errors.New("netease api: search endpoint not configured")
	}
	raw, err := c.call(ctx, c.endpoints.Search, map[string]any{
		"keyword": strings.TrimSpace(keyword),
		"limit":   10,
		"offset":  0,
	})
	if err != nil {
		return nil, fmt.Errorf("netease api: search: %w", err)
	}
	return tracksFromRaw(raw), nil
}

// LikedStatus reports official liked status when both credentials and a liked
// endpoint are configured. ok=false means callers should fall back to local
// cache lookup.
func (c *Client) LikedStatus(ctx context.Context, track bio.Track) (liked bool, ok bool, err error) {
	if !c.Enabled() || c.endpoints.LikedSongs == "" {
		return false, false, nil
	}
	id, hasID := trackID(track.ID)
	if !hasID {
		return false, false, nil
	}
	raw, err := c.call(ctx, c.endpoints.LikedSongs, map[string]any{"limit": 1000, "offset": 0})
	if err != nil {
		return false, false, fmt.Errorf("netease api: liked songs: %w", err)
	}
	for _, got := range tracksFromRaw(raw) {
		if gotID, ok := trackID(got.ID); ok && gotID == id {
			return true, true, nil
		}
	}
	for _, gotID := range idsFromRaw(raw) {
		if gotID == id {
			return true, true, nil
		}
	}
	return false, true, nil
}

func (c *Client) call(ctx context.Context, path string, biz any) (json.RawMessage, error) {
	reqURL, err := c.requestURL(path, biz)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "feishutune")
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("http %d: %s", res.StatusCode, shortBody(body))
	}
	var env envelope
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&env); err != nil {
		return nil, fmt.Errorf("decode JSON: %w", err)
	}
	if env.Code != 0 && env.Code != http.StatusOK {
		return nil, fmt.Errorf("code %d: %s", env.Code, env.Message)
	}
	if len(env.Data) > 0 && string(env.Data) != "null" {
		return env.Data, nil
	}
	if len(env.Result) > 0 && string(env.Result) != "null" {
		return env.Result, nil
	}
	return body, nil
}

type envelope struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
	Result  json.RawMessage `json:"result"`
}

func (c *Client) requestURL(path string, biz any) (string, error) {
	bizJSON, err := compactJSON(biz)
	if err != nil {
		return "", err
	}
	deviceJSON, err := compactJSON(c.device)
	if err != nil {
		return "", err
	}
	q := url.Values{}
	q.Set("appId", c.creds.AppID)
	q.Set("accessToken", c.creds.AccessToken)
	q.Set("signType", signType)
	q.Set("timestamp", strconv.FormatInt(c.now().UnixMilli(), 10))
	q.Set("device", string(deviceJSON))
	q.Set("bizContent", string(bizJSON))
	sig, err := signValues(c.creds.PrivateKey, q)
	if err != nil {
		return "", err
	}
	q.Set("sign", sig)
	u, err := url.Parse(c.baseURL + "/" + strings.TrimLeft(path, "/"))
	if err != nil {
		return "", err
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func compactJSON(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, b); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func signValues(privateKey string, q url.Values) (string, error) {
	key, err := parsePrivateKey(privateKey)
	if err != nil {
		return "", err
	}
	text := canonical(q)
	sum := sha256.Sum256([]byte(text))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

func parsePrivateKey(s string) (*rsa.PrivateKey, error) {
	s = strings.ReplaceAll(strings.TrimSpace(s), `\n`, "\n")
	block, _ := pem.Decode([]byte(s))
	if block == nil {
		return nil, errors.New("netease api: private_key is not PEM")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("netease api: parse private_key: %w", err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("netease api: private_key is not RSA")
	}
	return key, nil
}

func canonical(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		if k != "sign" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+q.Get(k))
	}
	return strings.Join(parts, "&")
}

func tracksFromRaw(raw json.RawMessage) []bio.Track {
	var v any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil
	}
	var out []bio.Track
	seen := map[string]bool{}
	var walk func(any)
	walk = func(x any) {
		switch x := x.(type) {
		case []any:
			for _, it := range x {
				walk(it)
			}
		case map[string]any:
			if t, ok := trackFromMap(x); ok {
				if !seen[t.ID] {
					seen[t.ID] = true
					out = append(out, t)
				}
			}
			for _, key := range []string{"data", "result", "song", "songs", "songList", "list", "tracks", "trackIds", "items", "resources"} {
				if child, ok := x[key]; ok {
					walk(child)
				}
			}
		}
	}
	walk(v)
	return out
}

func idsFromRaw(raw json.RawMessage) []string {
	var v any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	var walk func(any)
	walk = func(x any) {
		switch x := x.(type) {
		case []any:
			for _, it := range x {
				walk(it)
			}
		case map[string]any:
			if id := numeric(x["id"]); id != "" && (x["name"] != nil || x["songName"] != nil || x["trackIds"] == nil) {
				if !seen[id] {
					seen[id] = true
					out = append(out, id)
				}
			}
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(v)
	return out
}

func trackFromMap(m map[string]any) (bio.Track, bool) {
	id := firstNumeric(m, "id", "songId", "songID", "trackId", "trackID")
	name := firstString(m, "name", "songName", "title")
	if id == "" || name == "" {
		return bio.Track{}, false
	}
	artist := artists(m)
	album := album(m)
	duration := duration(m)
	if artist == "" && album == "" && duration == 0 {
		return bio.Track{}, false
	}
	return bio.Track{
		Name:     name,
		Artist:   artist,
		Album:    album,
		Duration: duration,
		ID:       trackPrefix + id,
	}, true
}

func firstNumeric(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if id := numeric(m[k]); id != "" {
			return id
		}
	}
	return ""
}

func numeric(v any) string {
	switch x := v.(type) {
	case json.Number:
		return numericString(x.String())
	case string:
		return numericString(strings.TrimSpace(x))
	case float64:
		if x > 0 && x == math.Trunc(x) {
			return strconv.FormatInt(int64(x), 10)
		}
	}
	return ""
}

func numericString(s string) string {
	if s == "" {
		return ""
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return ""
		}
	}
	return s
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

func artists(m map[string]any) string {
	if s := firstString(m, "artistName", "artistsName", "singerName", "singersName"); s != "" {
		return s
	}
	for _, key := range []string{"artists", "ar", "singers"} {
		if names := namesFromArray(m[key]); len(names) > 0 {
			return strings.Join(names, ", ")
		}
	}
	return ""
}

func album(m map[string]any) string {
	if s := firstString(m, "albumName"); s != "" {
		return s
	}
	for _, key := range []string{"album", "al"} {
		if mm, ok := m[key].(map[string]any); ok {
			if s := firstString(mm, "name", "albumName", "title"); s != "" {
				return s
			}
		}
	}
	return ""
}

func namesFromArray(v any) []string {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, it := range items {
		switch x := it.(type) {
		case string:
			if s := strings.TrimSpace(x); s != "" {
				out = append(out, s)
			}
		case map[string]any:
			if s := firstString(x, "name", "artistName", "title"); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func duration(m map[string]any) time.Duration {
	for _, key := range []string{"duration", "durationMs", "dt"} {
		if d := durationValue(m[key]); d > 0 {
			return d
		}
	}
	return 0
}

func durationValue(v any) time.Duration {
	var f float64
	switch x := v.(type) {
	case json.Number:
		f, _ = x.Float64()
	case float64:
		f = x
	case string:
		f, _ = strconv.ParseFloat(strings.TrimSpace(x), 64)
	}
	if f <= 0 {
		return 0
	}
	if f > 1000 {
		return time.Duration(f) * time.Millisecond
	}
	return time.Duration(f) * time.Second
}

func merge(base, enriched bio.Track) bio.Track {
	out := base
	if enriched.ID != "" {
		out.ID = enriched.ID
	}
	if enriched.Name != "" {
		out.Name = enriched.Name
	}
	if enriched.Artist != "" {
		out.Artist = enriched.Artist
	}
	if enriched.Album != "" {
		out.Album = enriched.Album
	}
	if enriched.Duration > 0 {
		out.Duration = enriched.Duration
	}
	return out
}

func metadataMatch(track, cand bio.Track) bool {
	if norm(track.Name) == "" || norm(track.Name) != norm(cand.Name) {
		return false
	}
	if !artistsMatch(track.Artist, cand.Artist) {
		return false
	}
	checked := false
	if track.Album != "" && cand.Album != "" {
		checked = true
		if norm(track.Album) != norm(cand.Album) {
			return false
		}
	}
	if track.Duration > 0 && cand.Duration > 0 {
		checked = true
		if math.Abs(float64(track.Duration-cand.Duration)) > float64(2*time.Second) {
			return false
		}
	}
	return checked
}

func artistsMatch(a, b string) bool {
	aa, bb := artistSet(a), artistSet(b)
	if len(aa) == 0 || len(aa) != len(bb) {
		return false
	}
	for i := range aa {
		if aa[i] != bb[i] {
			return false
		}
	}
	return true
}

func artistSet(s string) []string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		switch r {
		case ',', '，', '/', '、', ';', '；', '&':
			return true
		default:
			return false
		}
	})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = norm(p); p != "" {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

func norm(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(s)), " "))
}

func trackID(s string) (string, bool) {
	id, ok := strings.CutPrefix(s, trackPrefix)
	return id, ok && id != "" && numericString(id) != ""
}

func endpointsFromEnv() Endpoints {
	return Endpoints{
		SongDetail: envOrDefault(envDetailPath, defaultDetailPath),
		Search:     envOrDefault(envSearchPath, defaultSearchPath),
		LikedSongs: strings.TrimSpace(getenv(envLikedPath)),
	}.trimmed()
}

func (e Endpoints) trimmed() Endpoints {
	e.SongDetail = "/" + strings.TrimLeft(strings.TrimSpace(e.SongDetail), "/")
	e.Search = "/" + strings.TrimLeft(strings.TrimSpace(e.Search), "/")
	e.LikedSongs = "/" + strings.TrimLeft(strings.TrimSpace(e.LikedSongs), "/")
	if e.SongDetail == "/" {
		e.SongDetail = ""
	}
	if e.Search == "/" {
		e.Search = ""
	}
	if e.LikedSongs == "/" {
		e.LikedSongs = ""
	}
	return e
}

func envOrDefault(key, fallback string) string {
	if s := strings.TrimSpace(getenv(key)); s != "" {
		return s
	}
	return fallback
}

var getenv = os.Getenv

func shortBody(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}
