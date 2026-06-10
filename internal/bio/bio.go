// Package bio decides what the Feishu personal signature should say.
//
// The signature answers "what am I doing right now?". While you are at the Mac a
// playing track is shown; otherwise — idle, away, or manually paused — a status
// is shown instead:
//
//   - weekend -> Weekend ("weekend")
//   - at the Mac (recent input) -> Online ("online")
//   - away (no input past the idle threshold) -> Offline ("away")
//
// Compose is a pure function of (now, track, paused, active); all I/O — reading
// the player and the Mac's idle time — lives in the adapter packages.
package bio

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/lascape/sat"
)

// Track is a snapshot of the music player. The zero value (Playing == false)
// means nothing is playing. Position and Duration drive the progress readout; a
// zero Duration means the length is unknown and the progress is omitted.
type Track struct {
	Playing  bool
	Name     string
	Artist   string
	Album    string
	Position time.Duration
	Duration time.Duration

	// ID is the Spotify track URI ("spotify:track:<base62>"), filled by the
	// adapter. It identifies the track for the liked-status lookup and is empty
	// for ads, local files, or anything that is not a Spotify track.
	ID string
	// Liked reports whether this track is in the user's Liked Songs. It is set
	// by the orchestration (after an out-of-band lookup), not the player adapter;
	// when true, the now-playing line carries a ♡.
	Liked bool
}

// Policy holds the configurable status text and the idle threshold.
type Policy struct {
	Online  string // at the Mac, nothing playing
	Offline string // away from the Mac
	Weekend string // idle on Saturday or Sunday

	// IdleAfter is how long the Mac may go without keyboard or mouse input
	// before it counts as away: the Mac is active while its idle time is below
	// IdleAfter and inactive once it reaches it.
	IdleAfter time.Duration

	// Blacklist holds substrings that must never reach Feishu; if the composed
	// signature contains any of them, the update is suppressed.
	Blacklist []string
}

// Default returns the standard policy: online / away / weekend, away after 10m idle.
func Default() Policy {
	return Policy{
		Online:    "online",
		Offline:   "away",
		Weekend:   "weekend",
		IdleAfter: 10 * time.Minute,
	}
}

// Active reports whether the Mac counts as in use, given how long it has gone
// without input: active while idle is below IdleAfter, inactive once it reaches it.
func (p Policy) Active(idle time.Duration) bool {
	return idle < p.IdleAfter
}

// Compose renders the signature for the given moment and player state. The track
// is shown only while not paused, with the Mac active, and actually playing;
// otherwise the idle status wins.
func (p Policy) Compose(now time.Time, track Track, paused, active bool) string {
	if !paused && active && track.Playing {
		return nowPlaying(track)
	}
	return p.idle(now, active)
}

// Preview renders the signature for right now without Compose's active and pause
// gating: a playing track always shows its now-playing line so the format can be
// inspected at any time, and anything else falls back to the idle status. It
// backs the `preview` command, which never writes.
func (p Policy) Preview(track Track, now time.Time, active bool) string {
	if track.Playing {
		return nowPlaying(track)
	}
	return p.idle(now, active)
}

// Blocked reports whether text contains any blacklisted substring (case-folded),
// in which case the caller must not publish it. Empty entries never match.
func (p Policy) Blocked(text string) bool {
	low := strings.ToLower(text)
	for _, b := range p.Blacklist {
		if b != "" && strings.Contains(low, strings.ToLower(b)) {
			return true
		}
	}
	return false
}

// idle returns the status shown when no track is playing: weekend wins, then
// Online while the Mac is active, else Offline.
func (p Policy) idle(now time.Time, active bool) string {
	switch {
	case isWeekend(now):
		return p.Weekend
	case active:
		return p.Online
	default:
		return p.Offline
	}
}

func isWeekend(t time.Time) bool {
	switch t.Weekday() {
	case time.Saturday, time.Sunday:
		return true
	default:
		return false
	}
}

// Glyphs and width for the now-playing scrubber. The box-drawing cells tile
// cleanly in Feishu's proportional UI font and the knob marks the playhead. The
// elapsed and total times flank the bar so the whole thing reads as a player
// scrubber; ten cells fit comfortably in Feishu's status line.
const (
	barWidth  = 10
	barPlayed = "━"
	barKnob   = "●"
	barRemain = "─"
)

// nowPlaying renders the track as "♫ <name> · <artist>", converting any
// Traditional Chinese in the metadata to Simplified so it matches the rest of
// the signature and stripping Spotify's remaster/format noise from the title.
// A liked track gets an outline ♡ right after the name, kept clear of the bar.
// When the track length is known it appends a scrubber flanked by the elapsed
// and total times. The album is intentionally not shown.
func nowPlaying(t Track) string {
	d := sat.DefaultDict()
	name := cleanTitle(d.Read(t.Name))
	artist := d.Read(t.Artist)
	if t.Liked {
		name += " ♡"
	}
	s := fmt.Sprintf("♫ %s · %s", name, artist)
	if t.Duration > 0 {
		s += fmt.Sprintf("  %s %s %s", mmss(t.Position), progressBar(t.Position, t.Duration), mmss(t.Duration))
	}
	return s
}

// progressBar renders a fixed-width scrubber for the elapsed fraction pos/dur:
// played cells, the playhead knob, then remaining cells. The ratio is clamped so
// an over-run position (or a zero duration) can neither overflow nor underflow.
func progressBar(pos, dur time.Duration) string {
	ratio := 0.0
	if dur > 0 {
		ratio = float64(pos) / float64(dur)
	}
	switch {
	case ratio < 0:
		ratio = 0
	case ratio > 1:
		ratio = 1
	}
	knob := int(ratio*float64(barWidth-1) + 0.5)
	return strings.Repeat(barPlayed, knob) + barKnob + strings.Repeat(barRemain, barWidth-1-knob)
}

// titleNoise matches the mastering/format suffix Spotify appends to track names
// — "- Remastered 2011", "(2009 Remaster)", "- Mono" — which adds length without
// changing what is playing. Anchored to the end and case-insensitive.
var titleNoise = regexp.MustCompile(`(?i)\s*(?:[-(]\s*(?:\d{4}\s+)?re-?master(?:ed)?(?:\s+\d{4})?\s*\)?|-\s*(?:mono|stereo))\s*$`)

// cleanTitle strips that trailing noise so the song name stays short and intact.
func cleanTitle(s string) string {
	return strings.TrimSpace(titleNoise.ReplaceAllString(s, ""))
}

// mmss formats a duration as m:ss, truncating to whole seconds the way a media
// player shows elapsed time and length (81.9s -> "1:21").
func mmss(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	secs := int(d / time.Second)
	return fmt.Sprintf("%d:%02d", secs/60, secs%60)
}
