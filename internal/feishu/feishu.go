// Package feishu sets the current user's personal signature (个性签名) on
// Feishu/Lark via the web client's cookie-authenticated endpoint:
//
//	PUT https://internal-api-lark-api.feishu.cn/passport/users/details/
//	{"description":"<text>","description_flag":0}
//
// Only the `session` cookie is required; it is valid ~350 days, so no token
// refresh is needed. When it expires, re-export `session` from a logged-in
// feishu.cn browser.
package feishu

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	detailsURL  = "https://internal-api-lark-api.feishu.cn/passport/users/details/"
	maxDescRune = 140 // signature length limit enforced by the web editor
)

// ErrSessionExpired is returned when the stored session cookie is rejected.
// Recover by re-exporting the `session` cookie from a logged-in browser.
var ErrSessionExpired = errors.New("feishu: session expired or invalid")

// Client sets the signature for one logged-in account.
type Client struct {
	session string
	http    *http.Client
}

// New returns a client that authenticates with the given session cookie.
func New(session string) *Client {
	return &Client{
		session: session,
		http:    &http.Client{Timeout: 15 * time.Second},
	}
}

// LoadSession reads the web session token from FEISHU_SESSION, falling back to
// ~/.larktune/session. It does not hit the network.
func LoadSession() (string, error) {
	if s := strings.TrimSpace(os.Getenv("FEISHU_SESSION")); s != "" {
		return s, nil
	}
	if b, err := os.ReadFile(sessionFile()); err == nil {
		if s := strings.TrimSpace(string(b)); s != "" {
			return s, nil
		}
	}
	return "", fmt.Errorf("feishu: no session token: run `larktune login` with the `session` cookie "+
		"from a logged-in feishu.cn browser, set FEISHU_SESSION, or write it to %s", sessionFile())
}

// SaveSession writes the session token to the session file (0600), creating the
// data directory if needed. It is the counterpart to LoadSession, used by the
// `login` command.
func SaveSession(token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("feishu: empty session token")
	}
	path := sessionFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("feishu: create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(token+"\n"), 0o600); err != nil {
		return fmt.Errorf("feishu: write session: %w", err)
	}
	return nil
}

type detailsRequest struct {
	Description     string `json:"description"`
	DescriptionFlag int    `json:"description_flag"`
}

type detailsResponse struct {
	BaseResp struct {
		StatusCode    int    `json:"StatusCode"`
		StatusMessage string `json:"StatusMessage"`
	} `json:"BaseResp"`
	Code int    `json:"code"` // set on passport errors, e.g. 99991641 "session is invalid"
	Msg  string `json:"msg"`
}

// Set replaces the personal signature. Text longer than 140 runes is truncated
// to the platform limit. Returns ErrSessionExpired (wrapped) when the session
// is rejected.
func (c *Client) Set(ctx context.Context, desc string) error {
	if c.session == "" {
		return errors.New("feishu: empty session token")
	}
	body, err := json.Marshal(detailsRequest{Description: clamp(desc)})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, detailsURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	// text/plain mirrors the web client and avoids a CORS preflight; the server parses JSON regardless.
	req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	req.Header.Set("Cookie", "session="+c.session)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	var r detailsResponse
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return fmt.Errorf("feishu: decode response (HTTP %d): %w", resp.StatusCode, err)
	}
	if resp.StatusCode == http.StatusUnauthorized || r.Code != 0 {
		return fmt.Errorf("%w (HTTP %d, code %d %q)", ErrSessionExpired, resp.StatusCode, r.Code, r.Msg)
	}
	if r.BaseResp.StatusCode != 0 {
		return fmt.Errorf("feishu: set description failed: %d %q", r.BaseResp.StatusCode, r.BaseResp.StatusMessage)
	}
	return nil
}

// clamp truncates to maxDescRune runes without splitting a multibyte character.
func clamp(s string) string {
	if r := []rune(s); len(r) > maxDescRune {
		return string(r[:maxDescRune])
	}
	return s
}

func sessionFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".larktune-session"
	}
	return filepath.Join(home, ".larktune", "session")
}
