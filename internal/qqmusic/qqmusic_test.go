package qqmusic

import (
	"testing"
	"time"

	"github.com/Durden-T/feishutune/internal/bio"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want bio.Track
		err  bool
	}{
		{name: "nothing playing (null)", in: "null\n"},
		{name: "empty output", in: ""},
		{
			name: "playing QQ Music track",
			in:   `{"bundleIdentifier":"com.tencent.QQMusicMac","title":"Awakening","artist":"栾慧","album":"《Awakening》","duration":81.5,"elapsedTime":1.2,"playing":true}`,
			want: bio.Track{Playing: true, Name: "Awakening", Artist: "栾慧", Album: "《Awakening》", Duration: 81 * time.Second, Position: 1 * time.Second},
		},
		{
			name: "paused keeps metadata but is not playing",
			in:   `{"bundleIdentifier":"com.tencent.QQMusicMac","title":"Song","artist":"Art","album":"Alb","duration":200,"elapsedTime":5,"playing":false}`,
			want: bio.Track{Name: "Song", Artist: "Art", Album: "Alb", Duration: 200 * time.Second, Position: 5 * time.Second},
		},
		{
			name: "another app's track is ignored",
			in:   `{"bundleIdentifier":"com.spotify.client","title":"Other","artist":"Band","playing":true}`,
			want: bio.Track{},
		},
		{name: "malformed JSON is an error", in: `{not json`, err: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parse([]byte(tt.in))
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
