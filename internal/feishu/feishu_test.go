package feishu

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
)

func TestClamp(t *testing.T) {
	if got := clamp("hello"); got != "hello" {
		t.Fatalf("short string altered: %q", got)
	}
	long := strings.Repeat("界", maxDescRune+60) // multibyte, over the limit
	got := clamp(long)
	if n := len([]rune(got)); n != maxDescRune {
		t.Fatalf("rune length = %d, want %d", n, maxDescRune)
	}
	if strings.ContainsRune(got, '�') {
		t.Fatal("truncation split a multibyte rune")
	}
}

func TestLoadSessionMissing(t *testing.T) {
	t.Setenv("FEISHU_SESSION", "")
	t.Setenv("HOME", t.TempDir()) // isolate from any real ~/.larktune/session
	if _, err := LoadSession(); err == nil {
		t.Fatal("expected an error when no session is configured")
	}
}

func TestLoadSessionTrimsEnv(t *testing.T) {
	t.Setenv("FEISHU_SESSION", "  tok123  ")
	got, err := LoadSession()
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	if got != "tok123" {
		t.Fatalf("LoadSession = %q, want trimmed %q", got, "tok123")
	}
}

func TestSetEmptySession(t *testing.T) {
	if err := New("").Set(context.Background(), "x"); err == nil {
		t.Fatal("expected an error with an empty session token")
	}
}

func TestSaveSessionRoundTrip(t *testing.T) {
	t.Setenv("FEISHU_SESSION", "") // force LoadSession down the file path
	t.Setenv("HOME", t.TempDir())

	if err := SaveSession("  tok-xyz  "); err != nil {
		t.Fatalf("SaveSession: %v", err)
	}
	got, err := LoadSession()
	if err != nil {
		t.Fatalf("LoadSession after save: %v", err)
	}
	if got != "tok-xyz" {
		t.Fatalf("LoadSession = %q, want trimmed %q", got, "tok-xyz")
	}
}

func TestSaveSessionEmpty(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := SaveSession("   "); err == nil {
		t.Fatal("SaveSession with a blank token: want error, got nil")
	}
}

// roundTripFunc stands in for the HTTP transport so Set can be tested without
// reaching the real Feishu API.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func clientReturning(status int, body string, check func(*http.Request)) *Client {
	c := New("sess-token")
	c.http = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if check != nil {
			check(r)
		}
		return &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})}
	return c
}

func TestSetSuccess(t *testing.T) {
	var gotBody string
	c := clientReturning(http.StatusOK, `{"BaseResp":{"StatusCode":0}}`, func(r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("method = %s, want PUT", r.Method)
		}
		if got := r.Header.Get("Cookie"); got != "session=sess-token" {
			t.Errorf("cookie = %q, want session=sess-token", got)
		}
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
	})
	if err := c.Set(context.Background(), "hello"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if !strings.Contains(gotBody, `"description":"hello"`) || !strings.Contains(gotBody, `"description_flag":0`) {
		t.Errorf("request body = %s, want description and flag", gotBody)
	}
}

func TestSetSessionExpired(t *testing.T) {
	c := clientReturning(http.StatusOK, `{"code":99991641,"msg":"session is invalid"}`, nil)
	if err := c.Set(context.Background(), "x"); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("Set error = %v, want ErrSessionExpired", err)
	}
}

func TestSetUnauthorized(t *testing.T) {
	c := clientReturning(http.StatusUnauthorized, `{}`, nil)
	if err := c.Set(context.Background(), "x"); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("Set error = %v, want ErrSessionExpired", err)
	}
}

func TestSetBackendError(t *testing.T) {
	c := clientReturning(http.StatusOK, `{"BaseResp":{"StatusCode":1,"StatusMessage":"nope"}}`, nil)
	err := c.Set(context.Background(), "x")
	if err == nil || errors.Is(err, ErrSessionExpired) {
		t.Fatalf("Set error = %v, want a non-session backend error", err)
	}
}

// Live integration test, opt-in: FEISHU_SESSION must hold a logged-in `session`
// cookie. Sets a marker, then restores the signature to empty; both must succeed.
func TestSetLive(t *testing.T) {
	s := strings.TrimSpace(os.Getenv("FEISHU_SESSION"))
	if s == "" {
		t.Skip("set FEISHU_SESSION to run the live integration test")
	}
	c := New(s)
	if err := c.Set(context.Background(), "larktune ✦ test"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := c.Set(context.Background(), ""); err != nil {
		t.Fatalf("restore: %v", err)
	}
}
