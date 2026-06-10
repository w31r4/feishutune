package spotify

import (
	"context"
	"testing"
	"time"

	"github.com/Durden-T/feishutune/internal/bio"
)

func TestParse(t *testing.T) {
	us := fieldSep
	tests := []struct {
		name string
		in   string
		want bio.Track
		err  bool
	}{
		{name: "not running", in: "notrunning\n"},
		{name: "stopped", in: "stopped\n"},
		{name: "empty", in: ""},
		{
			name: "playing with progress",
			in:   "playing" + us + "Jukebox Uncle" + us + "Jay Chou" + us + "Capricorn" + us + "316501" + us + "81" + us + "spotify:track:5hvyS23Ya468Sp4VeL48U5\n",
			want: bio.Track{Playing: true, Name: "Jukebox Uncle", Artist: "Jay Chou", Album: "Capricorn", Duration: 316501 * time.Millisecond, Position: 81 * time.Second, ID: "spotify:track:5hvyS23Ya468Sp4VeL48U5"},
		},
		{
			name: "paused keeps metadata but is not playing",
			in:   "paused" + us + "Song" + us + "Artist" + us + "Album" + us + "200000" + us + "5" + us + "spotify:track:abc",
			want: bio.Track{Name: "Song", Artist: "Artist", Album: "Album", Duration: 200000 * time.Millisecond, Position: 5 * time.Second, ID: "spotify:track:abc"},
		},
		{
			name: "separators inside metadata are preserved",
			in:   "playing" + us + "A——B | C" + us + "Art|ist" + us + "Al——bum" + us + "123456" + us + "12" + us + "spotify:track:xyz",
			want: bio.Track{Playing: true, Name: "A——B | C", Artist: "Art|ist", Album: "Al——bum", Duration: 123456 * time.Millisecond, Position: 12 * time.Second, ID: "spotify:track:xyz"},
		},
		{
			name: "malformed numbers are tolerated; the track still shows without progress",
			in:   "playing" + us + "N" + us + "A" + us + "Al" + us + "x" + us + "y" + us + "spotify:track:q",
			want: bio.Track{Playing: true, Name: "N", Artist: "A", Album: "Al", ID: "spotify:track:q"},
		},
		{
			name: "a local file's non-track id is passed through verbatim",
			in:   "playing" + us + "Demo" + us + "Me" + us + "Tape" + us + "180000" + us + "3" + us + "spotify:local:::Demo:180",
			want: bio.Track{Playing: true, Name: "Demo", Artist: "Me", Album: "Tape", Duration: 180000 * time.Millisecond, Position: 3 * time.Second, ID: "spotify:local:::Demo:180"},
		},
		{name: "malformed: too few fields", in: "playing" + us + "only" + us + "two", err: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parse(tt.in)
			if tt.err {
				if err == nil {
					t.Fatalf("parse(%q): want error, got nil", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("parse(%q): unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("parse(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

// TestNowPlayingLive exercises the real osascript path. It is read-only and skips
// when Spotify isn't actively playing on this Mac, so it never fails in CI.
func TestNowPlayingLive(t *testing.T) {
	got, err := New().NowPlaying(context.Background())
	if err != nil {
		t.Skipf("NowPlaying unavailable here: %v", err)
	}
	if !got.Playing {
		t.Skip("Spotify is not playing; nothing to verify")
	}
	if got.Name == "" {
		t.Fatalf("playing track missing name: %+v", got)
	}
	t.Logf("live now-playing: %s — %s (%s) [%v/%v]", got.Name, got.Artist, got.Album, got.Position, got.Duration)
}
