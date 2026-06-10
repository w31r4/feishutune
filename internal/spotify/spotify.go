// Package spotify reads the currently playing track from the local Spotify
// desktop app via AppleScript, so no Web API credentials are needed. It observes
// playback on this Mac only, not phones or other Spotify Connect devices.
package spotify

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Durden-T/larktune/internal/bio"
)

// fieldSep is `character id 31` (ASCII Unit Separator), emitted between fields
// by the AppleScript; it cannot appear in track metadata, so the split below is
// unambiguous.
const fieldSep = "\x1f"

// script reads the player state and current track, returning unit-separated
// fields: state, name, artist, album, duration (milliseconds), player position
// (whole seconds), and the track id (a "spotify:track:<base62>" URI, used for
// the liked-status lookup). The `is running` guard keeps it from launching
// Spotify, and it short-circuits when nothing is loaded.
const script = `if application "Spotify" is running then
	tell application "Spotify"
		set s to player state as string
		if s is "stopped" then return "stopped"
		set d to character id 31
		return s & d & (name of current track) & d & (artist of current track) & d & (album of current track) & d & (duration of current track) & d & ((player position) div 1) & d & (id of current track)
	end tell
else
	return "notrunning"
end if`

// Client reads now-playing state from the local Spotify app.
type Client struct{}

// New returns a Spotify reader.
func New() *Client { return &Client{} }

// NowPlaying returns the current track. When Spotify is closed, stopped, or has
// nothing loaded it returns the zero Track (Playing == false) and a nil error.
func (c *Client) NowPlaying(ctx context.Context) (bio.Track, error) {
	out, err := exec.CommandContext(ctx, "osascript", "-e", script).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return bio.Track{}, fmt.Errorf("spotify: osascript: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return bio.Track{}, fmt.Errorf("spotify: osascript: %w", err)
	}
	return parse(string(out))
}

// parse turns one line of AppleScript output into a Track.
func parse(out string) (bio.Track, error) {
	out = strings.TrimRight(out, "\n")
	switch out {
	case "", "notrunning", "stopped":
		return bio.Track{}, nil
	}
	f := strings.Split(out, fieldSep)
	if len(f) != 7 {
		return bio.Track{}, fmt.Errorf("spotify: unexpected output: %q", out)
	}
	// duration (ms) and position (whole seconds) are best-effort: a malformed
	// number just omits the progress, it never hides the track.
	durMS, _ := strconv.Atoi(strings.TrimSpace(f[4]))
	posSec, _ := strconv.Atoi(strings.TrimSpace(f[5]))
	return bio.Track{
		Playing:  f[0] == "playing",
		Name:     f[1],
		Artist:   f[2],
		Album:    f[3],
		Duration: time.Duration(durMS) * time.Millisecond,
		Position: time.Duration(posSec) * time.Second,
		ID:       strings.TrimSpace(f[6]),
	}, nil
}
