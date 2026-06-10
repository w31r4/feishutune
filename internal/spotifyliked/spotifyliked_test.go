package spotifyliked

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// fakeSpotify stands in for the three Spotify endpoints and counts how often each
// is hit, so tests can assert token caching and cache hits.
type fakeSpotify struct {
	srv                              *httptest.Server
	tokenHits, clientHits, queryHits int
	liked                            map[string]bool
}

func newFakeSpotify(t *testing.T, liked map[string]bool) *fakeSpotify {
	f := &fakeSpotify{liked: liked}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/token", func(w http.ResponseWriter, r *http.Request) {
		f.tokenHits++
		if !strings.Contains(r.Header.Get("Cookie"), "sp_dc=") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		write(w, map[string]any{
			"accessToken":                      "acc",
			"accessTokenExpirationTimestampMs": time.Now().Add(time.Hour).UnixMilli(),
			"isAnonymous":                      false,
		})
	})
	mux.HandleFunc("/v1/clienttoken", func(w http.ResponseWriter, _ *http.Request) {
		f.clientHits++
		write(w, map[string]any{"granted_token": map[string]any{"token": "ctok", "refresh_after_seconds": 1209600}})
	})
	mux.HandleFunc("/pathfinder/v2/query", func(w http.ResponseWriter, r *http.Request) {
		f.queryHits++
		if r.Header.Get("authorization") == "" || r.Header.Get("client-token") == "" || r.Header.Get("app-platform") != "WebPlayer" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		var req struct {
			Variables struct {
				URIs []string `json:"uris"`
			} `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		lookup := make([]map[string]any, 0, len(req.Variables.URIs))
		for _, u := range req.Variables.URIs {
			lookup = append(lookup, map[string]any{"data": map[string]any{"saved": f.liked[u]}})
		}
		write(w, map[string]any{"data": map[string]any{"lookup": lookup}})
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeSpotify) client(spDC string) *Client {
	c := New(spDC)
	c.tokenURL = f.srv.URL + "/api/token"
	c.clientTokenURL = f.srv.URL + "/v1/clienttoken"
	c.pathfinderURL = f.srv.URL + "/pathfinder/v2/query"
	return c
}

func write(w http.ResponseWriter, v any) { _ = json.NewEncoder(w).Encode(v) }

// TestLikedDisabled covers the no-network short-circuits: no cookie, and a
// non-track URI both yield (false, nil) without touching the network.
func TestLikedDisabled(t *testing.T) {
	if got, err := New("").Liked(context.Background(), "spotify:track:abc"); err != nil || got {
		t.Fatalf("Liked without a cookie = (%v, %v), want (false, nil)", got, err)
	}
	if got, err := New("cookie").Liked(context.Background(), "spotify:ad:123"); err != nil || got {
		t.Fatalf("Liked for a non-track URI = (%v, %v), want (false, nil)", got, err)
	}
}

// TestLikedQueriesAndCaches checks the end-to-end flow: a liked and an unliked
// track resolve correctly, the two tokens are minted once and reused, and a
// repeat lookup is served from the fresh cache with no further network.
func TestLikedQueriesAndCaches(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	const liked, unliked = "spotify:track:liked", "spotify:track:unliked"
	f := newFakeSpotify(t, map[string]bool{liked: true})
	c := f.client("cookie")

	if got, err := c.Liked(context.Background(), liked); err != nil || !got {
		t.Fatalf("Liked(liked) = (%v, %v), want (true, nil)", got, err)
	}
	if got, err := c.Liked(context.Background(), unliked); err != nil || got {
		t.Fatalf("Liked(unliked) = (%v, %v), want (false, nil)", got, err)
	}
	if f.tokenHits != 1 || f.clientHits != 1 {
		t.Fatalf("mints = (access %d, client %d), want (1, 1) — tokens should be cached", f.tokenHits, f.clientHits)
	}
	if f.queryHits != 2 {
		t.Fatalf("query hits = %d, want 2 (one per distinct track)", f.queryHits)
	}

	if got, err := c.Liked(context.Background(), liked); err != nil || !got {
		t.Fatalf("cached Liked(liked) = (%v, %v), want (true, nil)", got, err)
	}
	if f.queryHits != 2 {
		t.Fatalf("query hits = %d after a cache hit, want still 2", f.queryHits)
	}
}

// TestLikedCookieRejected checks that an anonymous token (sp_dc rejected) surfaces
// an actionable error pointing at spotify-login.
func TestLikedCookieRejected(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		write(w, map[string]any{"accessToken": "", "isAnonymous": true})
	}))
	t.Cleanup(srv.Close)
	c := New("badcookie")
	c.tokenURL = srv.URL

	_, err := c.Liked(context.Background(), "spotify:track:x")
	if err == nil || !strings.Contains(err.Error(), "spotify-login") {
		t.Fatalf("Liked with a rejected cookie err = %v, want a spotify-login hint", err)
	}
}

// TestLikedLive exercises the real token-mint + areEntitiesInLibrary path against
// Spotify. It is read-only and skips unless SPOTIFY_SP_DC is set, so it never runs
// in CI. The saved value depends on your library, so it only asserts success.
func TestLikedLive(t *testing.T) {
	cookie := os.Getenv("SPOTIFY_SP_DC")
	if cookie == "" {
		t.Skip("SPOTIFY_SP_DC not set; skipping live liked check")
	}
	t.Setenv("HOME", t.TempDir())
	// "Mr. Brightside" — a stable, well-known public track.
	saved, err := New(cookie).Liked(context.Background(), "spotify:track:003vvx7Niy0yvhvHt4a68B")
	if err != nil {
		t.Fatalf("live Liked: %v", err)
	}
	t.Logf("live liked check ok: saved=%v", saved)
}
