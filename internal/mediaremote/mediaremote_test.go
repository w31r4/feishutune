package mediaremote

import (
	"testing"
	"time"

	"github.com/Durden-T/feishutune/internal/bio"
)

func TestParse(t *testing.T) {
	qq := New("qqmusic", "com.tencent.QQMusicMac", "")
	netease := New("netease", "com.netease.163music", "netease:track:")
	netease.now = func() time.Time { return time.Date(2026, 6, 12, 8, 42, 46, 0, time.UTC) }

	tests := []struct {
		name   string
		client *Client
		in     string
		want   bio.Track
		err    bool
	}{
		{name: "nothing playing null", client: qq, in: "null\n"},
		{name: "empty output", client: qq, in: ""},
		{
			name:   "playing QQ Music track",
			client: qq,
			in:     `{"bundleIdentifier":"com.tencent.QQMusicMac","title":"Awakening","artist":"栾慧","album":"《Awakening》","duration":81.5,"elapsedTime":1.2,"playing":true}`,
			want:   bio.Track{Playing: true, Name: "Awakening", Artist: "栾慧", Album: "《Awakening》", Duration: 81 * time.Second, Position: 1 * time.Second},
		},
		{
			name:   "playing NetEase track with numeric identifier",
			client: netease,
			in:     `{"bundleIdentifier":"com.netease.163music","title":"下学路","artist":"驼儿","album":"眠气","duration":203.9,"elapsedTime":11.8,"playing":true,"uniqueIdentifier":"2725880283"}`,
			want:   bio.Track{Playing: true, Name: "下学路", Artist: "驼儿", Album: "眠气", Duration: 203 * time.Second, Position: 11 * time.Second, ID: "netease:track:2725880283"},
		},
		{
			name:   "elapsedTimeNow wins",
			client: netease,
			in:     `{"bundleIdentifier":"com.netease.163music","title":"Song","artist":"Art","duration":200,"elapsedTime":5,"elapsedTimeNow":7.9,"playing":true}`,
			want:   bio.Track{Playing: true, Name: "Song", Artist: "Art", Duration: 200 * time.Second, Position: 7 * time.Second},
		},
		{
			name:   "playing NetEase track synthesizes elapsed from timestamp",
			client: netease,
			in:     `{"bundleIdentifier":"com.netease.163music","title":"Song","artist":"Art","duration":224.7,"elapsedTime":0,"timestamp":"2026-06-12T08:40:46Z","playbackRate":1,"playing":true}`,
			want:   bio.Track{Playing: true, Name: "Song", Artist: "Art", Duration: 224 * time.Second, Position: 120 * time.Second},
		},
		{
			name:   "synthesized elapsed clamps to duration",
			client: netease,
			in:     `{"bundleIdentifier":"com.netease.163music","title":"Song","artist":"Art","duration":60,"elapsedTime":0,"timestamp":"2026-06-12T08:40:46Z","playbackRate":1,"playing":true}`,
			want:   bio.Track{Playing: true, Name: "Song", Artist: "Art", Duration: 60 * time.Second, Position: 60 * time.Second},
		},
		{
			name:   "paused keeps metadata but is not playing",
			client: qq,
			in:     `{"bundleIdentifier":"com.tencent.QQMusicMac","title":"Song","artist":"Art","album":"Alb","duration":200,"elapsedTime":5,"playing":false}`,
			want:   bio.Track{Name: "Song", Artist: "Art", Album: "Alb", Duration: 200 * time.Second, Position: 5 * time.Second},
		},
		{
			name:   "another app is ignored",
			client: netease,
			in:     `{"bundleIdentifier":"com.spotify.client","title":"Other","artist":"Band","playing":true}`,
			want:   bio.Track{},
		},
		{
			name:   "url content identifier yields id",
			client: netease,
			in:     `{"bundleIdentifier":"com.netease.163music","title":"Song","artist":"Art","playing":true,"contentItemIdentifier":"https://music.163.com/#/song?id=349892"}`,
			want:   bio.Track{Playing: true, Name: "Song", Artist: "Art", ID: "netease:track:349892"},
		},
		{name: "malformed JSON is an error", client: qq, in: `{not json`, err: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.client.Parse([]byte(tt.in))
			if tt.err {
				if err == nil {
					t.Fatalf("Parse(%q): want error, got nil", tt.in)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q): unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("Parse(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}
