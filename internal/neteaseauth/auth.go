// Package neteaseauth stores the optional NetEase Cloud Music Open Platform
// credentials used by the API enhancement layer. Runtime loading is deliberately
// forgiving: absent or malformed credentials disable the enhancement instead of
// breaking signature updates.
package neteaseauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Durden-T/feishutune/internal/store"
)

const (
	fileName = "netease-auth.json"

	envAppID        = "NETEASE_APP_ID"
	envPrivateKey   = "NETEASE_PRIVATE_KEY"
	envAccessToken  = "NETEASE_OAUTH_TOKEN"
	envRefreshToken = "NETEASE_REFRESH_TOKEN"
	envExpiresAt    = "NETEASE_TOKEN_EXPIRES_AT"
)

// Credentials are the user-provided Open Platform credentials. RefreshToken and
// ExpiresAt are stored for a future refresh implementation; v1 only uses the
// current access token.
type Credentials struct {
	AppID        string    `json:"app_id"`
	PrivateKey   string    `json:"private_key"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitzero"`
}

// Enabled reports whether the required credentials are present.
func (c Credentials) Enabled() bool {
	return strings.TrimSpace(c.AppID) != "" &&
		strings.TrimSpace(c.PrivateKey) != "" &&
		strings.TrimSpace(c.AccessToken) != ""
}

// Load reads credentials from the stored file and overlays non-empty environment
// variables. Any malformed file or invalid timestamp disables that part of the
// configuration rather than returning an error to the update path.
func Load() Credentials {
	c := loadFile()
	overlayEnv(&c)
	c.trim()
	if !c.Enabled() {
		return Credentials{}
	}
	return c
}

// Save writes validated credentials to ~/.feishutune/netease-auth.json.
func Save(c Credentials) error {
	c.trim()
	if !c.Enabled() {
		return errors.New("netease: app_id, private_key, and access_token are required")
	}
	dir, err := store.Dir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("netease: create %s: %w", dir, err)
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, fileName), append(b, '\n'), 0o600); err != nil {
		return fmt.Errorf("netease: write auth: %w", err)
	}
	return nil
}

// ParseJSON decodes the stdin payload accepted by `feishutune netease-auth`.
func ParseJSON(b []byte) (Credentials, error) {
	if len(strings.TrimSpace(string(b))) == 0 {
		return Credentials{}, errors.New("netease: no auth JSON on stdin")
	}
	var c Credentials
	if err := json.Unmarshal(b, &c); err != nil {
		return Credentials{}, fmt.Errorf("netease: parse auth JSON: %w", err)
	}
	c.trim()
	if !c.Enabled() {
		return Credentials{}, errors.New("netease: app_id, private_key, and access_token are required")
	}
	return c, nil
}

func loadFile() Credentials {
	path, err := authPath()
	if err != nil {
		return Credentials{}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Credentials{}
	}
	var c Credentials
	if err := json.Unmarshal(b, &c); err != nil {
		return Credentials{}
	}
	return c
}

func overlayEnv(c *Credentials) {
	setString(&c.AppID, envAppID)
	setString(&c.PrivateKey, envPrivateKey)
	setString(&c.AccessToken, envAccessToken)
	setString(&c.RefreshToken, envRefreshToken)
	if s := strings.TrimSpace(os.Getenv(envExpiresAt)); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			c.ExpiresAt = t
		}
	}
}

func setString(dst *string, key string) {
	if s := strings.TrimSpace(os.Getenv(key)); s != "" {
		*dst = s
	}
}

func (c *Credentials) trim() {
	c.AppID = strings.TrimSpace(c.AppID)
	c.PrivateKey = strings.TrimSpace(c.PrivateKey)
	c.AccessToken = strings.TrimSpace(c.AccessToken)
	c.RefreshToken = strings.TrimSpace(c.RefreshToken)
}

func authPath() (string, error) {
	dir, err := store.Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fileName), nil
}
