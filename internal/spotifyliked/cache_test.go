package spotifyliked

import (
	"testing"
	"time"
)

// TestCacheFresh pins the TTL boundary: an entry newer than likedTTL is fresh, an
// older one is stale, and a missing one is never fresh.
func TestCacheFresh(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	c := &cache{Tracks: map[string]trackEntry{
		"recent": {Saved: true, CheckedAt: now.Add(-time.Hour)},
		"edge":   {Saved: true, CheckedAt: now.Add(-likedTTL + time.Second)},
		"stale":  {Saved: true, CheckedAt: now.Add(-likedTTL - time.Minute)},
	}}
	if saved, ok := c.fresh("recent", now); !ok || !saved {
		t.Fatalf("fresh(recent) = (%v, %v), want (true, true)", saved, ok)
	}
	if _, ok := c.fresh("edge", now); !ok {
		t.Fatal("fresh(edge): an entry just under the TTL must still be fresh")
	}
	if _, ok := c.fresh("stale", now); ok {
		t.Fatal("fresh(stale): an entry past the TTL must not be fresh")
	}
	if _, ok := c.fresh("missing", now); ok {
		t.Fatal("fresh(missing): an absent entry must not be fresh")
	}
}

// TestCacheRoundTrip checks that save then loadCache preserves tokens and track
// entries, and that a missing file loads as an empty (all-miss) cache.
func TestCacheRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if c := loadCache(); len(c.Tracks) != 0 {
		t.Fatalf("loadCache() with no file = %+v, want empty", c)
	}
	want := &cache{
		AccessToken: "acc",
		ClientToken: "ctok",
		DeviceID:    "dev",
		Tracks:      map[string]trackEntry{"spotify:track:x": {Saved: true, CheckedAt: time.Unix(1_700_000_000, 0).UTC()}},
	}
	if err := want.save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	got := loadCache()
	if got.AccessToken != "acc" || got.ClientToken != "ctok" || got.DeviceID != "dev" {
		t.Fatalf("loaded tokens = %+v, want acc/ctok/dev", got)
	}
	if e := got.Tracks["spotify:track:x"]; !e.Saved {
		t.Fatalf("loaded track entry = %+v, want Saved", e)
	}
}
