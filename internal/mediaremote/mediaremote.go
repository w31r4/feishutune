// Package mediaremote reads macOS system Now Playing metadata via the
// media-control CLI. Player-specific packages use it with a bundle identifier
// filter so one system-wide now-playing source does not masquerade as another
// app.
package mediaremote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/Durden-T/feishutune/internal/bio"
)

// mediaControl is the CLI that bridges MediaRemote (Homebrew: `brew install
// media-control`). It lives in /opt/homebrew/bin, which the launchd agent's
// pinned PATH includes.
const mediaControl = "media-control"

// Client reads the system Now Playing source and keeps only one app's metadata.
type Client struct {
	SourceName string
	BundleID   string
	IDPrefix   string
	now        func() time.Time
}

// New returns a MediaRemote-backed now-playing reader. idPrefix is optional; if
// non-empty, a stable numeric MediaRemote identifier is exposed as
// "<idPrefix><digits>" in bio.Track.ID.
func New(sourceName, bundleID, idPrefix string) *Client {
	return &Client{SourceName: sourceName, BundleID: bundleID, IDPrefix: idPrefix, now: time.Now}
}

// NowPlaying returns the current track for the configured bundle id. When the
// system now-playing source is empty or belongs to another app, the zero Track is
// returned with no error so callers can fall through to another source.
func (c *Client) NowPlaying(ctx context.Context) (bio.Track, error) {
	out, err := exec.CommandContext(ctx, mediaControl, "get").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return bio.Track{}, fmt.Errorf("%s: media-control: %s", c.SourceName, strings.TrimSpace(string(ee.Stderr)))
		}
		return bio.Track{}, fmt.Errorf("%s: media-control: %w", c.SourceName, err)
	}
	return c.Parse(out)
}

// Parse turns one `media-control get` JSON object into a Track, keeping it only
// when its bundle identifier matches the configured source. A `null`, empty
// output, or any other app's track yields the zero Track.
func (c *Client) Parse(out []byte) (bio.Track, error) {
	if t := strings.TrimSpace(string(out)); t == "" || t == "null" {
		return bio.Track{}, nil
	}
	var in info
	dec := json.NewDecoder(bytes.NewReader(out))
	dec.UseNumber()
	if err := dec.Decode(&in); err != nil {
		return bio.Track{}, fmt.Errorf("%s: parse media-control output: %w", c.SourceName, err)
	}
	if in.Bundle != c.BundleID {
		return bio.Track{}, nil
	}
	now := c.now
	if now == nil {
		now = time.Now
	}
	return bio.Track{
		Playing:  in.Playing,
		Name:     in.Title,
		Artist:   in.Artist,
		Album:    in.Album,
		Duration: secs(in.Duration),
		Position: position(in, now),
		ID:       trackID(c.IDPrefix, in.UniqueIdentifier, in.ContentItemIdentifier),
	}, nil
}

// info is the subset of `media-control get` JSON we use. Times are seconds. Some
// media-control versions expose elapsedTimeNow, while older ones expose
// elapsedTime plus a timestamp for when that elapsed value was captured. NetEase
// often leaves elapsedTime at 0 and relies on timestamp/playbackRate instead.
type info struct {
	Bundle                string   `json:"bundleIdentifier"`
	Title                 string   `json:"title"`
	Artist                string   `json:"artist"`
	Album                 string   `json:"album"`
	Duration              float64  `json:"duration"`
	Elapsed               float64  `json:"elapsedTime"`
	ElapsedNow            *float64 `json:"elapsedTimeNow"`
	Timestamp             string   `json:"timestamp"`
	PlaybackRate          float64  `json:"playbackRate"`
	Playing               bool     `json:"playing"`
	UniqueIdentifier      any      `json:"uniqueIdentifier"`
	ContentItemIdentifier any      `json:"contentItemIdentifier"`
}

func position(in info, now func() time.Time) time.Duration {
	pos := elapsed(in, now)
	if in.Duration > 0 && pos > in.Duration {
		pos = in.Duration
	}
	return secs(pos)
}

func elapsed(in info, now func() time.Time) float64 {
	if in.ElapsedNow != nil {
		return *in.ElapsedNow
	}
	pos := in.Elapsed
	if in.Playing && in.PlaybackRate > 0 && in.Timestamp != "" {
		if ts, err := time.Parse(time.RFC3339Nano, in.Timestamp); err == nil {
			if since := now().Sub(ts).Seconds(); since > 0 {
				pos += since * in.PlaybackRate
			}
		}
	}
	return pos
}

// secs converts a fractional-seconds value to a Duration, truncating sub-second
// precision the same way the now-playing readout shows whole seconds.
func secs(f float64) time.Duration {
	return time.Duration(f) * time.Second
}

func trackID(prefix string, vals ...any) string {
	if prefix == "" {
		return ""
	}
	for _, v := range vals {
		if id := numericID(v); id != "" {
			return prefix + id
		}
	}
	return ""
}

var (
	digitsOnly  = regexp.MustCompile(`^\d+$`)
	songIDInURL = regexp.MustCompile(`(?:[?&]id=|/song/)(\d+)`)
)

func numericID(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case json.Number:
		return numericString(x.String())
	case float64:
		if x > 0 && x == math.Trunc(x) {
			return strconv.FormatInt(int64(x), 10)
		}
	case string:
		s := strings.TrimSpace(x)
		if id := numericString(s); id != "" {
			return id
		}
		if m := songIDInURL.FindStringSubmatch(s); len(m) == 2 {
			return m[1]
		}
	}
	return ""
}

func numericString(s string) string {
	if s != "" && digitsOnly.MatchString(s) {
		return s
	}
	return ""
}
