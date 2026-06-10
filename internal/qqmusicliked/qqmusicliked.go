// Package qqmusicliked reports whether a QQ Music track is in the user's
// favorites ("我喜欢"). QQ Music exposes no stable catalog id through MediaRemote,
// so unlike Spotify's exact-URI lookup this matches the now-playing name+artist
// against the app's local library database — a best-effort text match, not an
// exact-id one. The database is QQ Music's own SQLite store; it is read through
// the system `sqlite3` CLI (already on macOS and on the launchd PATH) so the
// tool keeps its single Go dependency.
//
// Like the Spotify liked lookup, every step is best-effort from the caller's
// side: any error is treated as "not liked", so a locked or missing database
// drops the ♡ rather than failing the bio.
package qqmusicliked

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Durden-T/feishutune/internal/bio"
)

// favoritesFolderID is the folderid of QQ Music's built-in "我喜欢" (Liked)
// playlist in the NEWFOLDERS table; it is the same fixed id across installs.
const favoritesFolderID = 201

// likedQuery reports whether a song with the given name and singer sits in the
// favorites folder. NEWFOLDERSONGS links to NEWFOLDERS by its autoincrement PK
// `seq` (not folderid), and to SONGS by (id, type); EXISTS yields "1" or "0".
// The %s holes take pre-quoted SQL string literals from sqlQuote.
const likedQuery = `SELECT EXISTS(` +
	`SELECT 1 FROM NEWFOLDERSONGS fs ` +
	`JOIN NEWFOLDERS f ON fs.seq = f.seq ` +
	`JOIN SONGS s ON fs.id = s.id AND fs.type = s.type ` +
	`WHERE f.folderid = %d AND s.name = %s AND s.singer = %s);`

// Client answers favorites lookups against QQ Music's local library database.
type Client struct {
	// dbPath is QQ Music's SQLite library file; empty disables lookups (Liked
	// then returns false). Overridable in tests.
	dbPath string
}

// New returns a Client reading QQ Music's library at its standard sandbox path.
// If the home directory can't be resolved the client is disabled (Liked is a
// no-op), mirroring how an absent Spotify cookie disables that lookup.
func New() *Client { return &Client{dbPath: defaultDBPath()} }

// Liked reports whether the now-playing track is in the user's QQ Music
// favorites, matched by name+artist. It returns false without error when lookups
// are disabled (no database path), the track has no name, or the database file
// is absent — none of which is a failure, just no ♡.
func (c *Client) Liked(ctx context.Context, track bio.Track) (bool, error) {
	if c.dbPath == "" || track.Name == "" {
		return false, nil
	}
	if _, err := os.Stat(c.dbPath); err != nil {
		return false, nil // no QQ Music library here; treat as not liked
	}
	query := fmt.Sprintf(likedQuery, favoritesFolderID, sqlQuote(track.Name), sqlQuote(track.Artist))
	out, err := exec.CommandContext(ctx, "sqlite3", "-readonly", c.dbPath, query).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return false, fmt.Errorf("qqmusic liked: sqlite3: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return false, fmt.Errorf("qqmusic liked: sqlite3: %w", err)
	}
	return strings.TrimSpace(string(out)) == "1", nil
}

// sqlQuote renders s as a SQLite string literal, doubling any embedded single
// quote. SQLite has no backslash escaping, so '' doubling is the complete and
// only escaping needed — this keeps a track title with an apostrophe both
// correct and injection-safe in the read-only query.
func sqlQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// defaultDBPath returns QQ Music's sandboxed SQLite library path, or "" when the
// home directory can't be resolved (which disables lookups).
func defaultDBPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "Library", "Containers", "com.tencent.QQMusicMac",
		"Data", "Library", "Application Support", "QQMusicMac", "qqmusic.sqlite")
}
