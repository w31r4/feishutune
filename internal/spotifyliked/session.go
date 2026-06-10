package spotifyliked

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Durden-T/larktune/internal/store"
)

// spDCEnv overrides the stored cookie, mirroring how FEISHU_SESSION overrides
// the Feishu session.
const spDCEnv = "SPOTIFY_SP_DC"

// LoadSPDC returns the Spotify sp_dc login cookie from SPOTIFY_SP_DC or the
// stored file, or "" when none is configured. An absent cookie is not an error:
// liked-status lookups are optional and simply disabled without it, so the rest
// of the tool runs unchanged.
func LoadSPDC() string {
	if s := strings.TrimSpace(os.Getenv(spDCEnv)); s != "" {
		return s
	}
	if b, err := os.ReadFile(spDCFile()); err == nil {
		return strings.TrimSpace(string(b))
	}
	return ""
}

// SaveSPDC writes the sp_dc cookie to the data dir (0600), creating it if needed.
// It backs the `spotify-login` command.
func SaveSPDC(cookie string) error {
	cookie = strings.TrimSpace(cookie)
	if cookie == "" {
		return errors.New("spotify: empty sp_dc cookie")
	}
	dir, err := store.Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("spotify: create %s: %w", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sp_dc"), []byte(cookie+"\n"), 0o600); err != nil {
		return fmt.Errorf("spotify: write sp_dc: %w", err)
	}
	return nil
}

func spDCFile() string {
	dir, err := store.Dir()
	if err != nil {
		return ".larktune-sp_dc"
	}
	return filepath.Join(dir, "sp_dc")
}
