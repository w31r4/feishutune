package spotifyliked

import (
	"testing"
	"time"
)

// TestTOTP pins the code generator against the embedded secret with vectors
// computed independently (RFC 6238, SHA-1, 30-second step). It verifies the
// algorithm, not Spotify's acceptance, so it stays valid even if the secret
// rotates — only the embedded constant and these vectors move together.
func TestTOTP(t *testing.T) {
	cases := []struct {
		unix int64
		want string
	}{
		{1718000000, "329398"},
		{1718000029, "891931"}, // same 30s window as ...030
		{1718000030, "891931"},
		{1700000000, "371599"},
	}
	for _, c := range cases {
		got, err := totp(time.Unix(c.unix, 0))
		if err != nil {
			t.Fatalf("totp(%d): %v", c.unix, err)
		}
		if got != c.want {
			t.Errorf("totp(%d) = %s, want %s", c.unix, got, c.want)
		}
	}
}
