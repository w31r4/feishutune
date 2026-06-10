package spotifyliked

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"time"
)

// Spotify guards its web token endpoint with a TOTP whose shared secret and
// version ship in the web player and rotate from time to time. These mirror
// version 61; if token minting starts returning 4xx, re-capture them from a live
// open.spotify.com /api/token request (see the open.spotify.com browser note).
const (
	totpSecret  = "GM3TMMJTGYZTQNZVGM4DINJZHA4TGOBYGMZTCMRTGEYDSMJRHE4TEOBUG4YTCMRUGQ4DQOJUGQYTAMRRGA2TCMJSHE3TCMBY"
	totpVersion = "61"
)

// totp returns the 6-digit code for time t: RFC 6238 with a 30-second step and
// SHA-1 over the base32 secret above. The web player sends this same code as both
// the totp and totpServer query parameters.
func totp(t time.Time) (string, error) {
	key, err := base32.StdEncoding.DecodeString(totpSecret)
	if err != nil {
		return "", fmt.Errorf("spotify liked: decode totp secret: %w", err)
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(t.Unix())/30)
	mac := hmac.New(sha1.New, key)
	mac.Write(counter[:])
	sum := mac.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	code := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", code%1_000_000), nil
}
