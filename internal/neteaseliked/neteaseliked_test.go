package neteaseliked

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Durden-T/feishutune/internal/bio"
)

type fakeAPI struct {
	liked bool
	ok    bool
	err   error
}

func (f fakeAPI) LikedStatus(context.Context, bio.Track) (bool, bool, error) {
	return f.liked, f.ok, f.err
}

func TestLiked(t *testing.T) {
	db := buildDB(t, false)
	c := &Client{dbPath: db}
	ctx := context.Background()

	tests := []struct {
		name  string
		track bio.Track
		want  bool
	}{
		{
			name:  "exact id match",
			track: bio.Track{ID: "netease:track:2725880283", Name: "Anything"},
			want:  true,
		},
		{
			name:  "exact id match does not need metadata",
			track: bio.Track{ID: "netease:track:2725880283"},
			want:  true,
		},
		{
			name:  "exact id miss is authoritative",
			track: bio.Track{ID: "netease:track:999", Name: "下学路", Artist: "驼儿", Album: "眠气", Duration: 203 * time.Second},
			want:  false,
		},
		{
			name:  "strict metadata match",
			track: bio.Track{Name: "下学路", Artist: "驼儿", Album: "眠气", Duration: 203 * time.Second},
			want:  true,
		},
		{
			name:  "artist separator normalization",
			track: bio.Track{Name: "因为爱情", Artist: "王菲/陈奕迅", Album: "因为爱情", Duration: 216 * time.Second},
			want:  true,
		},
		{
			name:  "same title wrong artist does not match",
			track: bio.Track{Name: "下学路", Artist: "别人", Album: "眠气", Duration: 203 * time.Second},
			want:  false,
		},
		{
			name:  "same title wrong album does not match",
			track: bio.Track{Name: "下学路", Artist: "驼儿", Album: "另一张", Duration: 203 * time.Second},
			want:  false,
		},
		{
			name:  "same title wrong duration does not match",
			track: bio.Track{Name: "下学路", Artist: "驼儿", Album: "眠气", Duration: 300 * time.Second},
			want:  false,
		},
		{
			name:  "unliked metadata does not match",
			track: bio.Track{Name: "不在红心", Artist: "Nobody", Album: "No", Duration: 100 * time.Second},
			want:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := c.Liked(ctx, tt.track)
			if err != nil {
				t.Fatalf("Liked: %v", err)
			}
			if got != tt.want {
				t.Fatalf("Liked(%+v) = %v, want %v", tt.track, got, tt.want)
			}
		})
	}
}

func TestLikedDisabledAndMalformed(t *testing.T) {
	ctx := context.Background()
	if got, err := (&Client{dbPath: ""}).Liked(ctx, bio.Track{Name: "X"}); err != nil || got {
		t.Fatalf("Liked with no path = (%v, %v), want (false, nil)", got, err)
	}
	if got, err := (&Client{dbPath: "/no/such/file.sqlite"}).Liked(ctx, bio.Track{Name: "X"}); err != nil || got {
		t.Fatalf("Liked with missing DB = (%v, %v), want (false, nil)", got, err)
	}

	c := &Client{dbPath: buildDB(t, true)}
	if got, err := c.Liked(ctx, bio.Track{Name: "下学路", Artist: "驼儿", Album: "眠气", Duration: 203 * time.Second}); err != nil || got {
		t.Fatalf("Liked with malformed cache = (%v, %v), want (false, nil)", got, err)
	}
}

func TestAPILikedStatusPreferredWithLocalFallback(t *testing.T) {
	ctx := context.Background()
	db := buildDB(t, false)

	c := &Client{dbPath: db, api: fakeAPI{liked: true, ok: true}}
	if got, err := c.Liked(ctx, bio.Track{ID: "netease:track:999"}); err != nil || !got {
		t.Fatalf("API liked = (%v, %v), want (true, nil)", got, err)
	}

	c = &Client{dbPath: db, api: fakeAPI{err: errors.New("api down")}}
	if got, err := c.Liked(ctx, bio.Track{ID: "netease:track:2725880283"}); err != nil || !got {
		t.Fatalf("API error fallback = (%v, %v), want local true", got, err)
	}

	c = &Client{dbPath: db, api: fakeAPI{ok: false}}
	if got, err := c.Liked(ctx, bio.Track{ID: "netease:track:2725880283"}); err != nil || !got {
		t.Fatalf("API not applicable fallback = (%v, %v), want local true", got, err)
	}
}

func TestSQLQuote(t *testing.T) {
	if got, want := sqlQuote("don't"), "'don''t'"; got != want {
		t.Fatalf("sqlQuote = %q, want %q", got, want)
	}
}

func buildDB(t *testing.T, malformed bool) string {
	t.Helper()
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not available")
	}
	path := filepath.Join(t.TempDir(), "sqlite_storage.sqlite3")
	page := `{"data":{"entities":{"7489420710":{"id":"7489420710","name":"我喜欢的音乐","specialType":5},"8855352266":{"id":"8855352266","name":"别的歌单","specialType":0}}}}`
	if malformed {
		page = `{not json`
	}
	const schemaPrefix = `
CREATE TABLE persistentModel (uniKey VARCHAR(40) NOT NULL, jsonStr TEXT NULL, PRIMARY KEY (uniKey));
CREATE TABLE playlistTrackIds (id VARCHAR(40) NOT NULL, jsonStr TEXT NULL, PRIMARY KEY (id));
CREATE TABLE dbTrack (id VARCHAR(40) NOT NULL, jsonStr TEXT NULL, PRIMARY KEY (id));
`
	sql := schemaPrefix + `
INSERT INTO persistentModel (uniKey,jsonStr) VALUES ('page:playlist', '` + page + `');
INSERT INTO playlistTrackIds (id,jsonStr) VALUES
	('7489420710', '{"id":"7489420710","trackIds":[{"id":"2725880283","v":0},{"id":"349892","v":361},{"id":"555","v":1}]}'),
	('8855352266', '{"id":"8855352266","trackIds":[{"id":"999","v":0}]}');
INSERT INTO dbTrack (id,jsonStr) VALUES
	('2725880283', '{"id":"2725880283","name":"下学路","artists":[{"name":"驼儿"}],"album":{"name":"眠气"},"duration":203000}'),
	('349892', '{"id":"349892","name":"因为爱情","artists":[{"name":"陈奕迅"},{"name":"王菲"}],"album":{"name":"因为爱情"},"duration":216000}'),
	('555', '{"id":"555","name":"不相干","artists":[{"name":"Somebody"}],"album":{"name":"Other"},"duration":100000}'),
	('999', '{"id":"999","name":"别的歌单曲目","artists":[{"name":"Someone"}],"album":{"name":"No"},"duration":100000}');
`
	cmd := exec.Command("sqlite3", path)
	cmd.Stdin = strings.NewReader(sql)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build test db: %v: %s", err, out)
	}
	return path
}
