package bio

import (
	"strings"
	"testing"
	"time"
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

// TestCompose checks branch selection only — which status wins for a given
// moment and player state — by asserting the distinguishing marker is present.
// It deliberately does not pin the exact rendering (separators, the progress
// readout, any future prefix/suffix), so format tweaks don't ripple through
// every case; the exact now-playing format is pinned by TestNowPlaying instead.
func TestCompose(t *testing.T) {
	p := Default()
	track := Track{Playing: true, Name: "Song", Artist: "Artist", Album: "Album"}
	const trackMarker = "♫" // appears only when the track branch wins

	tests := []struct {
		name   string
		now    time.Time
		track  Track
		paused bool
		active bool
		marker string
	}{
		{"paused while active -> online", dayAt(time.Monday, 14), track, true, true, p.Online},
		{"paused while away -> offline", dayAt(time.Monday, 14), track, true, false, p.Offline},
		{"paused on weekend -> weekend", dayAt(time.Saturday, 14), track, true, true, p.Weekend},
		{"weekday active playing -> track", dayAt(time.Monday, 14), track, false, true, trackMarker},
		{"weekday active idle -> online", dayAt(time.Monday, 14), Track{}, false, true, p.Online},
		{"weekday away idle -> offline", dayAt(time.Monday, 14), Track{}, false, false, p.Offline},
		{"weekday away suppresses music", dayAt(time.Monday, 14), track, false, false, p.Offline},
		{"weekend active idle -> weekend", dayAt(time.Saturday, 14), Track{}, false, true, p.Weekend},
		{"weekend active playing -> track", dayAt(time.Saturday, 14), track, false, true, trackMarker},
		{"weekend away -> weekend", dayAt(time.Sunday, 14), Track{}, false, false, p.Weekend},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := p.Compose(tt.now, tt.track, tt.paused, tt.active); !strings.Contains(got, tt.marker) {
				t.Fatalf("Compose() = %q, want it to contain %q", got, tt.marker)
			}
		})
	}
}

// TestComposeEmptyStatus confirms an explicitly empty status text yields an
// empty bio (clearing the signature) rather than falling back to anything.
func TestComposeEmptyStatus(t *testing.T) {
	p := Default()
	p.Online = ""
	if got := p.Compose(dayAt(time.Monday, 14), Track{}, false, true); got != "" {
		t.Fatalf("Compose with empty Online = %q, want an empty bio", got)
	}
}

// TestActive pins the idle-threshold boundary: the Mac is active while its idle
// time is below IdleAfter and inactive once it reaches it.
func TestActive(t *testing.T) {
	p := Default() // IdleAfter = 10m
	cases := []struct {
		idle time.Duration
		want bool
	}{
		{0, true},
		{5 * time.Minute, true},
		{10*time.Minute - time.Nanosecond, true},
		{10 * time.Minute, false},
		{30 * time.Minute, false},
	}
	for _, c := range cases {
		if got := p.Active(c.idle); got != c.want {
			t.Errorf("Active(%v) = %v, want %v", c.idle, got, c.want)
		}
	}
}

// TestNowPlaying is the single golden test for the exact now-playing format: the
// metadata layout, the Traditional->Simplified conversion, and the progress
// readout. Update it (and only it) when the now-playing format changes.
func TestNowPlaying(t *testing.T) {
	t.Run("simplifies and omits progress when length unknown", func(t *testing.T) {
		got := nowPlaying(Track{Playing: true, Name: "手寫的從前", Artist: "周杰倫", Album: "哎呦, 不錯哦"})
		if want := `♫ 手写的从前 · 周杰伦`; got != want {
			t.Fatalf("nowPlaying() = %q, want %q", got, want)
		}
	})
	t.Run("appends progress when length known", func(t *testing.T) {
		got := nowPlaying(Track{
			Playing: true, Name: "Song", Artist: "Art", Album: "Alb",
			Position: 81 * time.Second, Duration: 316501 * time.Millisecond,
		})
		if want := `♫ Song · Art  1:21 ━━●─────── 5:16`; got != want {
			t.Fatalf("nowPlaying() = %q, want %q", got, want)
		}
	})
	t.Run("liked track carries the heart after the name", func(t *testing.T) {
		got := nowPlaying(Track{
			Playing: true, Name: "夜长梦多", Artist: "蛙池", Liked: true,
			Position: 168 * time.Second, Duration: 464 * time.Second,
		})
		if want := `♫ 夜长梦多 ♡ · 蛙池  2:48 ━━━●────── 7:44`; got != want {
			t.Fatalf("nowPlaying() = %q, want %q", got, want)
		}
	})
	t.Run("liked with unknown length keeps the heart by the name", func(t *testing.T) {
		got := nowPlaying(Track{Playing: true, Name: "Song", Artist: "Art", Liked: true})
		if want := `♫ Song ♡ · Art`; got != want {
			t.Fatalf("nowPlaying() = %q, want %q", got, want)
		}
	})
}

func TestMMSS(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0:00"},
		{5 * time.Second, "0:05"},
		{81 * time.Second, "1:21"},
		{316501 * time.Millisecond, "5:16"},
		{10 * time.Minute, "10:00"},
		{-3 * time.Second, "0:00"},
	}
	for _, c := range cases {
		if got := mmss(c.d); got != c.want {
			t.Errorf("mmss(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

// TestProgressBar pins the scrubber geometry: the knob walks left-to-right across
// a fixed-width bar as the elapsed fraction grows, and an over-run or unknown
// length clamps to the ends instead of overflowing.
func TestProgressBar(t *testing.T) {
	cases := []struct {
		name     string
		pos, dur time.Duration
		want     string
	}{
		{"start", 0, 100 * time.Second, "●─────────"},
		{"quarter", 25 * time.Second, 100 * time.Second, "━━●───────"},
		{"half", 50 * time.Second, 100 * time.Second, "━━━━━●────"},
		{"end", 100 * time.Second, 100 * time.Second, "━━━━━━━━━●"},
		{"overrun clamps to end", 130 * time.Second, 100 * time.Second, "━━━━━━━━━●"},
		{"unknown length clamps to start", 30 * time.Second, 0, "●─────────"},
		{"negative position clamps to start", -5 * time.Second, 100 * time.Second, "●─────────"},
	}
	for _, c := range cases {
		if got := progressBar(c.pos, c.dur); got != c.want {
			t.Errorf("progressBar(%v, %v) = %q, want %q", c.pos, c.dur, got, c.want)
		}
	}
}

// TestPreview checks the ungated preview: a playing track shows even on a weekend
// outside active hours (where Compose would hide it), and idle falls back to the
// time-based status.
func TestPreview(t *testing.T) {
	p := Default()
	t.Run("playing shows the track regardless of gating", func(t *testing.T) {
		awayWeekend := dayAt(time.Sunday, 3)
		got := p.Preview(Track{Playing: true, Name: "Song", Artist: "Art"}, awayWeekend, false)
		if want := "♫ Song · Art"; !strings.HasPrefix(got, want) {
			t.Fatalf("Preview() = %q, want it to start with %q", got, want)
		}
	})
	t.Run("nothing playing falls back to the idle status", func(t *testing.T) {
		if got := p.Preview(Track{}, dayAt(time.Monday, 14), true); got != p.Online {
			t.Fatalf("Preview() = %q, want %q", got, p.Online)
		}
	})
}

// TestCleanTitle pins the remaster/format-noise stripping: Spotify's trailing
// "- Remastered 2011", "(2009 Remaster)", and "- Mono" suffixes are removed,
// while a real title — even one that merely contains such a word — is left intact.
func TestCleanTitle(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Love Of My Life - Remastered 2011", "Love Of My Life"},
		{"Hey Jude - Remastered 2015", "Hey Jude"},
		{"Imagine (Remastered)", "Imagine"},
		{"Yesterday (2009 Remaster)", "Yesterday"},
		{"Sweet Jane - Mono", "Sweet Jane"},
		{"Bohemian Rhapsody", "Bohemian Rhapsody"},
		{"Mono", "Mono"},
		{"Get Lucky - Radio Edit", "Get Lucky - Radio Edit"},
	}
	for _, c := range cases {
		if got := cleanTitle(c.in); got != c.want {
			t.Errorf("cleanTitle(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestBlocked checks blacklist matching: case-insensitive substring; an empty
// blacklist or an empty entry matches nothing.
func TestBlocked(t *testing.T) {
	p := Default()
	p.Blacklist = []string{"secret", "周杰伦"}
	cases := []struct {
		text string
		want bool
	}{
		{"♫ 夜曲 · 周杰伦  ━●──── 1:21/5:16", true},
		{"♫ Secret Garden · Someone", true},
		{"♫ 山阴路的夏天 · 李志", false},
		{"在线", false},
	}
	for _, c := range cases {
		if got := p.Blocked(c.text); got != c.want {
			t.Errorf("Blocked(%q) = %v, want %v", c.text, got, c.want)
		}
	}
	if Default().Blocked("anything") {
		t.Error("default policy (no blacklist) must block nothing")
	}
	q := Default()
	q.Blacklist = []string{""}
	if q.Blocked("anything") {
		t.Error("an empty blacklist entry must not match everything")
	}
}
