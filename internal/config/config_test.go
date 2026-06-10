package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	clearPolicyEnv(t)

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != Defaults() {
		t.Fatalf("Load() = %+v, want defaults %+v", got, Defaults())
	}
}

// clearPolicyEnv unsets the policy environment variables so a value set on the
// host machine cannot influence a test. It truly unsets them (not set-to-empty),
// since an empty value is now meaningful ("explicitly cleared").
func clearPolicyEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{"ONLINE", "OFFLINE", "WEEKEND", "IDLE_AFTER"} {
		t.Setenv(k, "") // register the original for restoration on cleanup
		os.Unsetenv(k)  // then unset for the duration of the test
	}
}

// TestLoadPrecedence checks the merge order defaults < config file < env across
// every field and each precedence relationship. Flags (highest) are layered by
// the caller and covered separately.
func TestLoadPrecedence(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	clearPolicyEnv(t)

	dir := filepath.Join(home, ".feishutune")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	// File sets online, weekend, idle_after; leaves offline unset.
	cfg := `{"online":"file-online","weekend":"file-weekend","idle_after":"3m"}`
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ONLINE", "env-online")   // env beats file
	t.Setenv("OFFLINE", "env-offline") // env beats default (file unset)
	t.Setenv("IDLE_AFTER", "5m")       // env beats file
	// WEEKEND left unset -> file stands.

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := Policy{
		Online:    "env-online",   // env > file
		Offline:   "env-offline",  // env > default
		Weekend:   "file-weekend", // file > default
		IdleAfter: "5m",           // env > file
	}
	if got != want {
		t.Fatalf("Load() = %+v, want %+v", got, want)
	}
}

// TestLoadExplicitEmpty verifies that an explicitly empty value clears a field
// rather than falling back to the default — from both the config file and env.
func TestLoadExplicitEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	clearPolicyEnv(t)

	dir := filepath.Join(home, ".feishutune")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(`{"online":""}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OFFLINE", "") // explicitly empty via env

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Online != "" {
		t.Errorf(`Online = %q, want "" (explicit empty in config must not fall back to the default)`, got.Online)
	}
	if got.Offline != "" {
		t.Errorf(`Offline = %q, want "" (explicit empty env must not fall back to the default)`, got.Offline)
	}
	if got.Weekend != Defaults().Weekend {
		t.Errorf("Weekend = %q, want default %q (untouched)", got.Weekend, Defaults().Weekend)
	}
}

func TestToBio(t *testing.T) {
	b, err := Defaults().ToBio()
	if err != nil {
		t.Fatalf("ToBio: %v", err)
	}
	if b.IdleAfter != 10*time.Minute {
		t.Fatalf("IdleAfter = %v, want 10m", b.IdleAfter)
	}
	if b.Online != "online" || b.Offline != "away" || b.Weekend != "weekend" {
		t.Fatalf("texts = %q / %q / %q", b.Online, b.Offline, b.Weekend)
	}

	bl, err := Policy{IdleAfter: "10m", Blacklist: "foo, bar ,, baz"}.ToBio()
	if err != nil {
		t.Fatalf("ToBio blacklist: %v", err)
	}
	if want := []string{"foo", "bar", "baz"}; !reflect.DeepEqual(bl.Blacklist, want) {
		t.Fatalf("Blacklist = %v, want %v", bl.Blacklist, want)
	}

	bad := []Policy{
		{IdleAfter: "bad"}, // not a duration
		{IdleAfter: ""},    // empty
		{IdleAfter: "0"},   // not positive
		{IdleAfter: "-5m"}, // negative
	}
	for _, p := range bad {
		if _, err := p.ToBio(); err == nil {
			t.Errorf("ToBio(%+v): want error, got nil", p)
		}
	}
}

// TestSplitList covers the comma-list parsing behind the blacklist: items are
// trimmed, empties dropped, and a blank string yields no entries.
func TestSplitList(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"foo", []string{"foo"}},
		{"foo, bar ,, baz ", []string{"foo", "bar", "baz"}},
	}
	for _, c := range cases {
		if got := splitList(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("splitList(%q) = %#v, want %#v", c.in, got, c.want)
		}
	}
}
