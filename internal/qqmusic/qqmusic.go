// Package qqmusic reads the currently playing track from the local QQ Music
// (QQ音乐) desktop app on macOS. The app exposes no AppleScript dictionary, so
// unlike Spotify it can't be queried with osascript; instead it publishes to the
// system "Now Playing" service (MediaRemote), which the `media-control` CLI
// reads. This observes whichever app last published now-playing info on this
// Mac, so NowPlaying gates on QQ Music's bundle id and ignores anything else.
package qqmusic

import (
	"context"

	"github.com/Durden-T/feishutune/internal/bio"
	"github.com/Durden-T/feishutune/internal/mediaremote"
)

// bundleID is QQ Music's macOS bundle identifier. media-control reports the one
// system now-playing source regardless of which app it is, so a read is only
// QQ Music's when bundleIdentifier matches this.
const bundleID = "com.tencent.QQMusicMac"

// Client reads now-playing state from the local QQ Music app via media-control.
type Client struct {
	reader *mediaremote.Client
}

// New returns a QQ Music reader.
func New() *Client { return &Client{reader: mediaremote.New("qqmusic", bundleID, "")} }

// NowPlaying returns the current QQ Music track. When QQ Music isn't the active
// now-playing source — it's closed, nothing is loaded, or another app (e.g.
// Spotify) is the source — it returns the zero Track (Playing == false) and a
// nil error, so the caller falls through to another source or the idle status.
func (c *Client) NowPlaying(ctx context.Context) (bio.Track, error) {
	return c.reader.NowPlaying(ctx)
}

// parse turns one `media-control get` JSON object into a Track, keeping it only
// when QQ Music is the source. A `null` (nothing playing) or any other app's
// track yields the zero Track. QQ Music exposes no stable catalog id through
// MediaRemote, so ID is left empty and the liked lookup matches on name+artist.
func parse(out []byte) (bio.Track, error) {
	return mediaremote.New("qqmusic", bundleID, "").Parse(out)
}
