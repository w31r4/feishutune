package neteaseauth

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseSaveLoad(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	for _, key := range []string{envAppID, envPrivateKey, envAccessToken, envRefreshToken, envExpiresAt} {
		t.Setenv(key, "")
	}

	expires := "2026-06-12T05:30:00Z"
	c, err := ParseJSON([]byte(`{
		"app_id":" app ",
		"private_key":" key ",
		"access_token":" tok ",
		"refresh_token":" ref ",
		"expires_at":"` + expires + `"
	}`))
	if err != nil {
		t.Fatalf("ParseJSON: %v", err)
	}
	if err := Save(c); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got := Load()
	if got.AppID != "app" || got.PrivateKey != "key" || got.AccessToken != "tok" || got.RefreshToken != "ref" {
		t.Fatalf("Load = %+v", got)
	}
	wantExp, _ := time.Parse(time.RFC3339, expires)
	if !got.ExpiresAt.Equal(wantExp) {
		t.Fatalf("ExpiresAt = %s, want %s", got.ExpiresAt, wantExp)
	}
	info, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".feishutune", fileName))
	if err != nil {
		t.Fatalf("stat auth file: %v", err)
	}
	if gotMode := info.Mode().Perm(); gotMode != 0o600 {
		t.Fatalf("auth file mode = %o, want 600", gotMode)
	}
}

func TestLoadEnvOverridesFile(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := Save(Credentials{AppID: "file-app", PrivateKey: "file-key", AccessToken: "file-token"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envAppID, "env-app")
	t.Setenv(envAccessToken, "env-token")

	got := Load()
	if got.AppID != "env-app" || got.PrivateKey != "file-key" || got.AccessToken != "env-token" {
		t.Fatalf("Load with env = %+v", got)
	}
}

func TestParseRejectsEmptyAndMalformed(t *testing.T) {
	for _, in := range [][]byte{
		[]byte(""),
		[]byte("{"),
		[]byte(`{"app_id":"a","private_key":"k"}`),
	} {
		if _, err := ParseJSON(in); err == nil {
			t.Fatalf("ParseJSON(%q) succeeded, want error", in)
		}
	}
}

func TestLoadMalformedFileDisables(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	for _, key := range []string{envAppID, envPrivateKey, envAccessToken, envRefreshToken, envExpiresAt} {
		t.Setenv(key, "")
	}
	dir := filepath.Join(home, ".feishutune")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, fileName), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Load(); got.Enabled() {
		t.Fatalf("Load malformed = %+v, want disabled", got)
	}
}
