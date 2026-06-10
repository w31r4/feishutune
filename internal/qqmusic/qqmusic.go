// Package qqmusic reads the currently playing track from the local QQ Music
// (QQ音乐) desktop app on macOS. The app exposes no AppleScript dictionary, so
// unlike Spotify it can't be queried with osascript; instead it publishes to the
// system "Now Playing" service (MediaRemote), which the `media-control` CLI
// reads. This observes whichever app last published now-playing info on this
// Mac, so NowPlaying gates on QQ Music's bundle id and ignores anything else.
package qqmusic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/Durden-T/feishutune/internal/bio"
)

// bundleID is QQ Music's macOS bundle identifier. media-control reports the one
// system now-playing source regardless of which app it is, so a read is only
// QQ Music's when bundleIdentifier matches this.
const bundleID = "com.tencent.QQMusicMac"

// mediaControl is the CLI that bridges MediaRemote (Homebrew: `brew install
// media-control`). It lives in /opt/homebrew/bin, which the launchd agent's
// pinned PATH includes.
const mediaControl = "media-control"

// Client reads now-playing state from the local QQ Music app via media-control.
type Client struct{}

// New returns a QQ Music reader.
func New() *Client { return &Client{} }

// info is the subset of `media-control get` JSON we use. Times are seconds (the
// raw JSON has them as floats). A bare `null` (nothing playing) decodes to the
// zero value, which parse treats as not playing.
type info struct {
	Bundle   string  `json:"bundleIdentifier"`
	Title    string  `json:"title"`
	Artist   string  `json:"artist"`
	Album    string  `json:"album"`
	Duration float64 `json:"duration"`
	Elapsed  float64 `json:"elapsedTime"`
	Playing  bool    `json:"playing"`
}

// NowPlaying returns the current QQ Music track. When QQ Music isn't the active
// now-playing source — it's closed, nothing is loaded, or another app (e.g.
// Spotify) is the source — it returns the zero Track (Playing == false) and a
// nil error, so the caller falls through to another source or the idle status.
func (c *Client) NowPlaying(ctx context.Context) (bio.Track, error) {
	out, err := exec.CommandContext(ctx, mediaControl, "get").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return bio.Track{}, fmt.Errorf("qqmusic: media-control: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return bio.Track{}, fmt.Errorf("qqmusic: media-control: %w", err)
	}
	return parse(out)
}

// parse turns one `media-control get` JSON object into a Track, keeping it only
// when QQ Music is the source. A `null` (nothing playing) or any other app's
// track yields the zero Track. QQ Music exposes no stable catalog id through
// MediaRemote, so ID is left empty and the liked lookup matches on name+artist.
func parse(out []byte) (bio.Track, error) {
	if t := strings.TrimSpace(string(out)); t == "" || t == "null" {
		return bio.Track{}, nil
	}
	var in info
	if err := json.Unmarshal(out, &in); err != nil {
		return bio.Track{}, fmt.Errorf("qqmusic: parse media-control output: %w", err)
	}
	if in.Bundle != bundleID {
		return bio.Track{}, nil // some other app is the now-playing source
	}
	return bio.Track{
		Playing:  in.Playing,
		Name:     in.Title,
		Artist:   in.Artist,
		Album:    in.Album,
		Duration: secs(in.Duration),
		Position: secs(in.Elapsed),
	}, nil
}

// secs converts a fractional-seconds value to a Duration, truncating sub-second
// precision the same way the now-playing readout shows whole seconds.
func secs(f float64) time.Duration {
	return time.Duration(f) * time.Second
}
