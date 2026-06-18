// Package netease reads the currently playing track from the local NetEase
// Cloud Music (网易云音乐) desktop app on macOS. The app publishes to the
// system "Now Playing" service (MediaRemote), which the `media-control` CLI
// reads. This observes whichever app last published now-playing info on this
// Mac, so NowPlaying gates on NetEase's bundle id and ignores anything else.
package netease

import (
	"context"

	"github.com/Durden-T/feishutune/internal/bio"
	"github.com/Durden-T/feishutune/internal/mediaremote"
)

// bundleID is NetEase Cloud Music's macOS bundle identifier.
const bundleID = "com.netease.163music"

const trackIDPrefix = "netease:track:"

// Client reads now-playing state from the local NetEase app via media-control.
type Client struct {
	reader *mediaremote.Client
}

// New returns a NetEase Cloud Music reader.
func New() *Client {
	return &Client{reader: mediaremote.New("netease", bundleID, trackIDPrefix)}
}

// NowPlaying returns the current NetEase track. When NetEase isn't the active
// now-playing source — it's closed, nothing is loaded, or another app is the
// source — it returns the zero Track and a nil error, so the caller falls through
// to another source or the idle status.
func (c *Client) NowPlaying(ctx context.Context) (bio.Track, error) {
	return c.reader.NowPlaying(ctx)
}

func parse(out []byte) (bio.Track, error) {
	return mediaremote.New("netease", bundleID, trackIDPrefix).Parse(out)
}
