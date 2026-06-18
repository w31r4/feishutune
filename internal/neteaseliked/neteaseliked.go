// Package neteaseliked reports whether a NetEase Cloud Music track is in the
// user's liked playlist ("我喜欢的音乐"). The app keeps playlist and track caches
// in a local SQLite store; this package reads that cache through the system
// sqlite3 CLI and treats every failure as "not liked".
package neteaseliked

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Durden-T/feishutune/internal/bio"
)

const (
	trackPrefix = "netease:track:"
	fieldSep    = "\x1f"
)

const likedPlaylistCTE = `
WITH liked_playlist AS (
	SELECT e.key AS pid,
		CASE WHEN json_extract(e.value, '$.specialType') = 5 THEN 0 ELSE 1 END AS priority
	FROM persistentModel p, json_each(p.jsonStr, '$.data.entities') e
	WHERE p.uniKey = 'page:playlist'
		AND (
			json_extract(e.value, '$.specialType') = 5
			OR json_extract(e.value, '$.name') = '我喜欢的音乐'
		)
	ORDER BY priority
	LIMIT 1
),
liked_ids AS (
	SELECT json_extract(j.value, '$.id') AS track_id
	FROM playlistTrackIds p
	JOIN liked_playlist lp ON p.id = lp.pid,
	json_each(p.jsonStr, '$.trackIds') j
)
`

const likedTracksQuery = likedPlaylistCTE + `
SELECT
	d.id,
	coalesce(json_extract(d.jsonStr, '$.name'), ''),
	coalesce((SELECT group_concat(json_extract(a.value, '$.name'), ',') FROM json_each(d.jsonStr, '$.artists') a), ''),
	coalesce(json_extract(d.jsonStr, '$.album.name'), ''),
	coalesce(json_extract(d.jsonStr, '$.duration'), 0)
FROM liked_ids l
JOIN dbTrack d ON d.id = l.track_id;
`

// Client answers liked lookups against NetEase's local cache.
type Client struct {
	dbPath string
	api    APILiker
}

// APILiker is implemented by the optional NetEase Open Platform client. ok=false
// means the API was unavailable or not applicable and the local cache should be
// used instead.
type APILiker interface {
	LikedStatus(ctx context.Context, track bio.Track) (liked bool, ok bool, err error)
}

// Option customizes a Client.
type Option func(*Client)

// WithAPI enables an official API lookup before the local cache fallback.
func WithAPI(api APILiker) Option {
	return func(c *Client) { c.api = api }
}

// New returns a Client reading NetEase's standard sandboxed cache path. If the
// home directory can't be resolved the client is disabled.
func New(opts ...Option) *Client {
	c := &Client{dbPath: defaultDBPath()}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Liked reports whether track is in NetEase's liked playlist. An exact NetEase
// track id is authoritative; otherwise a strict metadata match is attempted.
// Missing files, malformed caches, sqlite errors, and ambiguous metadata matches
// all degrade to false without an error.
func (c *Client) Liked(ctx context.Context, track bio.Track) (bool, error) {
	if c.api != nil {
		if liked, ok, err := c.api.LikedStatus(ctx, track); err == nil && ok {
			return liked, nil
		}
	}
	if c.dbPath == "" {
		return false, nil
	}
	if _, err := os.Stat(c.dbPath); err != nil {
		return false, nil
	}
	if id, ok := strings.CutPrefix(track.ID, trackPrefix); ok {
		if id == "" {
			return false, nil
		}
		return c.likedID(ctx, id), nil
	}
	if track.ID != "" {
		return false, nil
	}
	if track.Name == "" {
		return false, nil
	}
	return c.likedByMetadata(ctx, track), nil
}

func (c *Client) likedID(ctx context.Context, id string) bool {
	query := likedPlaylistCTE + `SELECT EXISTS(SELECT 1 FROM liked_ids WHERE track_id = ` + sqlQuote(id) + `);`
	out, err := c.query(ctx, query)
	return err == nil && strings.TrimSpace(out) == "1"
}

func (c *Client) likedByMetadata(ctx context.Context, track bio.Track) bool {
	out, err := c.query(ctx, likedTracksQuery)
	if err != nil {
		return false
	}
	matches := 0
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		cand, ok := parseCandidate(line)
		if !ok {
			return false
		}
		if metadataMatch(track, cand) {
			matches++
			if matches > 1 {
				return false
			}
		}
	}
	return matches == 1
}

func (c *Client) query(ctx context.Context, query string) (string, error) {
	out, err := exec.CommandContext(ctx, "sqlite3", "-readonly", "-separator", fieldSep, c.dbPath, query).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("netease liked: sqlite3: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("netease liked: sqlite3: %w", err)
	}
	return string(out), nil
}

type candidate struct {
	id       string
	name     string
	artist   string
	album    string
	duration time.Duration
}

func parseCandidate(line string) (candidate, bool) {
	f := strings.Split(line, fieldSep)
	if len(f) != 5 {
		return candidate{}, false
	}
	ms, _ := time.ParseDuration(strings.TrimSpace(f[4]) + "ms")
	return candidate{
		id:       f[0],
		name:     f[1],
		artist:   f[2],
		album:    f[3],
		duration: ms,
	}, true
}

func metadataMatch(track bio.Track, cand candidate) bool {
	if norm(track.Name) == "" || norm(track.Name) != norm(cand.name) {
		return false
	}
	if !artistsMatch(track.Artist, cand.artist) {
		return false
	}

	checked := false
	if track.Album != "" && cand.album != "" {
		checked = true
		if norm(track.Album) != norm(cand.album) {
			return false
		}
	}
	if track.Duration > 0 && cand.duration > 0 {
		checked = true
		if math.Abs(float64(track.Duration-cand.duration)) > float64(2*time.Second) {
			return false
		}
	}
	return checked
}

func artistsMatch(a, b string) bool {
	aa, bb := artistSet(a), artistSet(b)
	if len(aa) == 0 || len(aa) != len(bb) {
		return false
	}
	for i := range aa {
		if aa[i] != bb[i] {
			return false
		}
	}
	return true
}

func artistSet(s string) []string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		switch r {
		case ',', '，', '/', '、', ';', '；', '&':
			return true
		default:
			return false
		}
	})
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = norm(p); p != "" {
			out = append(out, p)
		}
	}
	sort.Strings(out)
	return out
}

func norm(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(s)), " "))
}

func sqlQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func defaultDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "Containers", "com.netease.163music",
		"Data", "Documents", "storage", "sqlite_storage.sqlite3")
}
