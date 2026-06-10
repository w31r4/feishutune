package spotifyliked

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/Durden-T/feishutune/internal/store"
)

const (
	cacheFile = "spotify-cache.json"

	// likedTTL is how long a track's saved status is trusted before re-checking.
	// Day-scale staleness is acceptable, so a newly liked or unliked song
	// converges within a few days without a network call on every run.
	likedTTL = 72 * time.Hour

	// tokenSkew re-mints a token slightly before its stated expiry so it can't
	// lapse mid-request.
	tokenSkew = 30 * time.Second
)

// cache is the on-disk state backing liked lookups across one-shot runs: the two
// short-lived tokens (auto-refreshed from sp_dc) and each track's last known
// saved status. Written 0600 because the tokens are bearer credentials.
type cache struct {
	AccessToken  string                `json:"access_token,omitempty"`
	AccessExpiry time.Time             `json:"access_expiry,omitzero"`
	ClientToken  string                `json:"client_token,omitempty"`
	ClientExpiry time.Time             `json:"client_expiry,omitzero"`
	DeviceID     string                `json:"device_id,omitempty"`
	Tracks       map[string]trackEntry `json:"tracks,omitempty"`
}

type trackEntry struct {
	Saved     bool      `json:"saved"`
	CheckedAt time.Time `json:"checked_at"`
}

// loadCache reads the cache; a missing or unreadable file yields an empty cache
// (every track a miss) rather than an error, so a corrupt cache self-heals.
func loadCache() *cache {
	empty := func() *cache { return &cache{Tracks: map[string]trackEntry{}} }
	path, err := cachePath()
	if err != nil {
		return empty()
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return empty()
	}
	c := empty()
	if err := json.Unmarshal(b, c); err != nil {
		return empty()
	}
	if c.Tracks == nil {
		c.Tracks = map[string]trackEntry{}
	}
	return c
}

// save writes the cache (0600), creating the data directory if needed.
func (c *cache) save() error {
	dir, err := store.Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, cacheFile), b, 0o600)
}

// fresh returns the cached saved status for uri when present and newer than
// likedTTL as of now.
func (c *cache) fresh(uri string, now time.Time) (saved, ok bool) {
	e, ok := c.Tracks[uri]
	if !ok || now.Sub(e.CheckedAt) >= likedTTL {
		return false, false
	}
	return e.Saved, true
}

func cachePath() (string, error) {
	dir, err := store.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, cacheFile), nil
}
