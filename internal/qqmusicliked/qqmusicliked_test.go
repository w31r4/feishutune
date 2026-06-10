package qqmusicliked

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Durden-T/feishutune/internal/bio"
)

func TestSQLQuote(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Awakening", "'Awakening'"},
		{"don't", "'don''t'"},
		{"a'b'c", "'a''b''c'"},
		{"", "''"},
	}
	for _, tt := range tests {
		if got := sqlQuote(tt.in); got != tt.want {
			t.Errorf("sqlQuote(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestLiked builds a tiny copy of QQ Music's schema with the system sqlite3 and
// checks the favorites join end-to-end: a song in folder 201 is liked, one only
// in another folder is not, an unknown song is not, and a title with an
// apostrophe is matched (and injection-safe) through sqlQuote.
func TestLiked(t *testing.T) {
	db := buildDB(t)
	c := &Client{dbPath: db}
	ctx := context.Background()

	cases := []struct {
		name, artist string
		want         bool
	}{
		{"Awakening", "栾慧", true},        // in favorites (folder 201)
		{"Recent Only", "Someone", false}, // only in 最近播放 (folder 5)
		{"Unknown", "Nobody", false},      // not in the library
		{"It's Mine", "O'Brien", true},    // apostrophes, in favorites
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := c.Liked(ctx, bio.Track{Name: tt.name, Artist: tt.artist})
			if err != nil {
				t.Fatalf("Liked(%q): %v", tt.name, err)
			}
			if got != tt.want {
				t.Fatalf("Liked(%q,%q) = %v, want %v", tt.name, tt.artist, got, tt.want)
			}
		})
	}
}

// TestLikedDisabled covers the no-network short-circuits that yield (false, nil)
// without touching sqlite: no configured path, an empty track name, and a
// missing database file.
func TestLikedDisabled(t *testing.T) {
	ctx := context.Background()
	if got, err := (&Client{dbPath: ""}).Liked(ctx, bio.Track{Name: "X"}); err != nil || got {
		t.Fatalf("Liked with no path = (%v, %v), want (false, nil)", got, err)
	}
	if got, err := (&Client{dbPath: "/x.sqlite"}).Liked(ctx, bio.Track{}); err != nil || got {
		t.Fatalf("Liked with no track name = (%v, %v), want (false, nil)", got, err)
	}
	if got, err := (&Client{dbPath: "/no/such/file.sqlite"}).Liked(ctx, bio.Track{Name: "X"}); err != nil || got {
		t.Fatalf("Liked with a missing db = (%v, %v), want (false, nil)", got, err)
	}
}

// buildDB writes a minimal QQ Music library replicating the three tables and the
// seq-based folder join the real query relies on, returning its path. It skips
// the test if the system sqlite3 is unavailable.
func buildDB(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not available")
	}
	path := filepath.Join(t.TempDir(), "qqmusic.sqlite")
	const schema = `
CREATE TABLE NEWFOLDERS (seq INTEGER PRIMARY KEY, folderid INTEGER, folderName TEXT);
CREATE TABLE SONGS (id BIGINT, type INTEGER, name TEXT, singer TEXT, PRIMARY KEY(id,type));
CREATE TABLE NEWFOLDERSONGS (seq INTEGER, id BIGINT, type INTEGER);
INSERT INTO NEWFOLDERS (seq,folderid,folderName) VALUES (6,5,'最近播放'),(8,201,'我喜欢');
INSERT INTO SONGS (id,type,name,singer) VALUES (1,13,'Awakening','栾慧'),(2,13,'Recent Only','Someone'),(3,13,'It''s Mine','O''Brien');
INSERT INTO NEWFOLDERSONGS (seq,id,type) VALUES (8,1,13),(6,2,13),(8,3,13);
`
	cmd := exec.Command("sqlite3", path)
	cmd.Stdin = strings.NewReader(schema)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build test db: %v: %s", err, out)
	}
	return path
}
