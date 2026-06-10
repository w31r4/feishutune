package main

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Durden-T/larktune/internal/bio"
	"github.com/Durden-T/larktune/internal/store"
)

type fakePlayer struct {
	track bio.Track
	err   error
}

func (f fakePlayer) NowPlaying(context.Context) (bio.Track, error) { return f.track, f.err }

type fakePublisher struct {
	sets []string
	err  error
}

func (f *fakePublisher) Set(_ context.Context, text string) error {
	if f.err != nil {
		return f.err
	}
	f.sets = append(f.sets, text)
	return nil
}

type fakeIdle struct {
	d   time.Duration
	err error
}

func (f fakeIdle) Idle(context.Context) (time.Duration, error) { return f.d, f.err }

type fakeLiker struct {
	liked bool
	err   error
}

func (f fakeLiker) Liked(context.Context, string) (bool, error) { return f.liked, f.err }

// noLiked is the disabled liker used by tests that don't exercise the ♡: it
// reports every track as not liked, like an unconfigured Spotify cookie.
var noLiked = fakeLiker{}

// atMac and away are idle readings either side of the default 10m threshold; the
// zero fakeIdle reports no idle time at all (firmly at the Mac).
var (
	atMac = fakeIdle{}
	away  = fakeIdle{d: time.Hour}
)

// dayAt returns a time on the given weekday at the given hour, found by walking
// forward from a fixed anchor so the test never hard-codes a calendar date.
func dayAt(wd time.Weekday, hour int) time.Time {
	t := time.Date(2026, 1, 1, hour, 0, 0, 0, time.UTC)
	for t.Weekday() != wd {
		t = t.AddDate(0, 0, 1)
	}
	return t
}

func playingTrack() bio.Track {
	return bio.Track{Playing: true, Name: "Song", Artist: "Artist", Album: "Album"}
}

// playing is the now-playing body; Compose prefixes it with the time, so these
// orchestration tests match on the body rather than the exact signature (the
// exact format, including the time prefix, is covered by package bio's tests).
const playing = `♫ Song · Artist`

func TestUpdatePublishesTrack(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	pub := &fakePublisher{}

	res, err := update(context.Background(), bio.Default(), fakePlayer{track: playingTrack()}, atMac, pub, noLiked, dayAt(time.Monday, 14), io.Discard)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !res.Changed || !strings.Contains(res.Signature, playing) {
		t.Fatalf("result = %+v, want a changed signature containing %q", res, playing)
	}
	if len(pub.sets) != 1 || !strings.Contains(pub.sets[0], playing) {
		t.Fatalf("publishes = %v, want one containing %q", pub.sets, playing)
	}
	if st, _ := store.Load(); !strings.Contains(st.Signature, playing) {
		t.Fatalf("stored signature = %q, want it to contain %q", st.Signature, playing)
	}
}

// TestUpdateShowsHeartWhenLiked checks that a liked now-playing track carries the
// ♡, and that a liked-lookup error degrades to no heart without failing the run.
func TestUpdateShowsHeartWhenLiked(t *testing.T) {
	t.Run("liked track shows the heart", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		pub := &fakePublisher{}
		res, err := update(context.Background(), bio.Default(), fakePlayer{track: playingTrack()}, atMac, pub, fakeLiker{liked: true}, dayAt(time.Monday, 14), io.Discard)
		if err != nil {
			t.Fatalf("update: %v", err)
		}
		if !strings.Contains(res.Signature, "♡") {
			t.Fatalf("signature = %q, want a ♡ for a liked track", res.Signature)
		}
	})
	t.Run("lookup error degrades to no heart", func(t *testing.T) {
		t.Setenv("HOME", t.TempDir())
		var warn strings.Builder
		pub := &fakePublisher{}
		res, err := update(context.Background(), bio.Default(), fakePlayer{track: playingTrack()}, atMac, pub, fakeLiker{err: errors.New("token boom")}, dayAt(time.Monday, 14), &warn)
		if err != nil {
			t.Fatalf("a liked-lookup error must not fail the update: %v", err)
		}
		if strings.Contains(res.Signature, "♡") {
			t.Fatalf("signature = %q, want no ♡ when the lookup errors", res.Signature)
		}
		if !strings.Contains(warn.String(), "token boom") {
			t.Fatalf("warn = %q, want it to surface the liked error", warn.String())
		}
	})
}

func TestUpdateSkipsUnchanged(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	pub := &fakePublisher{}
	now := dayAt(time.Monday, 14) // at the Mac, nothing playing -> 在线

	for range 2 {
		if _, err := update(context.Background(), bio.Default(), fakePlayer{}, atMac, pub, noLiked, now, io.Discard); err != nil {
			t.Fatalf("update: %v", err)
		}
	}
	if len(pub.sets) != 1 || !strings.Contains(pub.sets[0], "在线") {
		t.Fatalf("publishes = %v, want a single 在线 (same minute -> no second write)", pub.sets)
	}
}

func TestUpdateRetriesAfterError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	pub := &fakePublisher{err: errors.New("boom")}
	now := dayAt(time.Monday, 14)

	if _, err := update(context.Background(), bio.Default(), fakePlayer{}, atMac, pub, noLiked, now, io.Discard); err == nil {
		t.Fatal("update: want error from failed publish, got nil")
	}
	if len(pub.sets) != 0 {
		t.Fatalf("publishes = %v, want none after a failed write", pub.sets)
	}
	if st, _ := store.Load(); st.Signature != "" {
		t.Fatalf("stored signature = %q, want empty after a failed write", st.Signature)
	}

	pub.err = nil
	if _, err := update(context.Background(), bio.Default(), fakePlayer{}, atMac, pub, noLiked, now, io.Discard); err != nil {
		t.Fatalf("retry: %v", err)
	}
	if len(pub.sets) != 1 || !strings.Contains(pub.sets[0], "在线") {
		t.Fatalf("publishes = %v, want one 在线 after retry", pub.sets)
	}
}

func TestUpdateAwaySuppressesMusic(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	pub := &fakePublisher{}

	res, err := update(context.Background(), bio.Default(), fakePlayer{track: playingTrack()}, away, pub, noLiked, dayAt(time.Monday, 14), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Signature, "离线") || strings.Contains(res.Signature, "♫") {
		t.Fatalf("signature = %q, want 离线 with music suppressed while away", res.Signature)
	}
	if len(pub.sets) != 1 {
		t.Fatalf("publishes = %v, want exactly one", pub.sets)
	}
}

func TestUpdatePausedShowsStatus(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, err := store.SetPaused(true); err != nil {
		t.Fatal(err)
	}
	pub := &fakePublisher{}

	res, err := update(context.Background(), bio.Default(), fakePlayer{track: playingTrack()}, atMac, pub, noLiked, dayAt(time.Monday, 14), io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Paused || !strings.Contains(res.Signature, "在线") || strings.Contains(res.Signature, "♫") {
		t.Fatalf("result = %+v, want paused showing 在线 (music suppressed)", res)
	}
}

func TestUpdateWarnsOnPlayerError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var warn strings.Builder
	pub := &fakePublisher{}

	_, err := update(context.Background(), bio.Default(), fakePlayer{err: errors.New("osascript boom")}, atMac, pub, noLiked, dayAt(time.Monday, 14), &warn)
	if err != nil {
		t.Fatalf("a player error must not fail the update: %v", err)
	}
	if !strings.Contains(warn.String(), "osascript boom") {
		t.Fatalf("warn = %q, want it to surface the player error", warn.String())
	}
	if len(pub.sets) != 1 || !strings.Contains(pub.sets[0], "在线") {
		t.Fatalf("publishes = %v, want 在线 fallback when the player errors", pub.sets)
	}
}

// TestUpdateWarnsOnIdleError checks idle-read tolerance: a failed idle read is
// surfaced to warnw and treated as "at the Mac", so a playing track still shows
// rather than the run marking you away.
func TestUpdateWarnsOnIdleError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var warn strings.Builder
	pub := &fakePublisher{}

	res, err := update(context.Background(), bio.Default(), fakePlayer{track: playingTrack()}, fakeIdle{err: errors.New("ioreg boom")}, pub, noLiked, dayAt(time.Monday, 14), &warn)
	if err != nil {
		t.Fatalf("an idle-read error must not fail the update: %v", err)
	}
	if !strings.Contains(warn.String(), "ioreg boom") {
		t.Fatalf("warn = %q, want it to surface the idle error", warn.String())
	}
	if !res.Changed || !strings.Contains(res.Signature, playing) {
		t.Fatalf("result = %+v, want the track shown when the idle read fails (treated as active)", res)
	}
}

// TestPreviewLine covers the preview command's core: a playing track renders the
// now-playing line, nothing playing falls back to the idle status, and a player
// error is surfaced to warnw but still yields the idle status.
func TestPreviewLine(t *testing.T) {
	now := dayAt(time.Monday, 14)
	playing := bio.Track{Playing: true, Name: "夜曲", Artist: "周杰伦", Position: 81 * time.Second, Duration: 316 * time.Second}

	t.Run("playing renders the now-playing line", func(t *testing.T) {
		got := previewLine(context.Background(), bio.Default(), fakePlayer{track: playing}, atMac, noLiked, now, io.Discard)
		if want := `♫ 夜曲 · 周杰伦  1:21 ━━●─────── 5:16`; got != want {
			t.Fatalf("previewLine() = %q, want %q", got, want)
		}
	})
	t.Run("nothing playing falls back to idle", func(t *testing.T) {
		if got := previewLine(context.Background(), bio.Default(), fakePlayer{}, atMac, noLiked, now, io.Discard); got != "在线" {
			t.Fatalf("previewLine() = %q, want 在线", got)
		}
	})
	t.Run("player error surfaces but yields idle", func(t *testing.T) {
		var warn strings.Builder
		got := previewLine(context.Background(), bio.Default(), fakePlayer{err: errors.New("boom")}, atMac, noLiked, now, &warn)
		if got != "在线" {
			t.Fatalf("previewLine() = %q, want 在线 on error", got)
		}
		if !strings.Contains(warn.String(), "boom") {
			t.Fatalf("warn = %q, want it to surface the error", warn.String())
		}
	})
}

// TestUpdateBlocked verifies a blacklisted signature is withheld: nothing is
// published, the result is marked blocked, and stored state is untouched.
func TestUpdateBlocked(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	pub := &fakePublisher{}
	policy := bio.Default()
	policy.Blacklist = []string{"Artist"} // matches playingTrack's artist

	res, err := update(context.Background(), policy, fakePlayer{track: playingTrack()}, atMac, pub, noLiked, dayAt(time.Monday, 14), io.Discard)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if !res.Blocked || res.Changed {
		t.Fatalf("result = %+v, want blocked with no change", res)
	}
	if len(pub.sets) != 0 {
		t.Fatalf("publishes = %v, want none when blacklisted", pub.sets)
	}
	if st, _ := store.Load(); st.Signature != "" {
		t.Fatalf("stored signature = %q, want empty (never published)", st.Signature)
	}
}
