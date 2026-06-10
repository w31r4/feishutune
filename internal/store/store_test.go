package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s, err := Load()
	if err != nil {
		t.Fatalf("Load on missing file: %v", err)
	}
	if s != (State{}) {
		t.Fatalf("Load() = %+v, want zero State", s)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	want := State{Paused: true, Signature: `♫ X · Y`}
	if err := want.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	path := filepath.Join(home, ".feishutune", "state.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat state.json: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("state.json perm = %o, want 600", perm)
	}

	got, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got != want {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
}

func TestSetPaused(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := (State{Signature: "keep"}).Save(); err != nil {
		t.Fatal(err)
	}

	s, err := SetPaused(true)
	if err != nil {
		t.Fatalf("SetPaused(true): %v", err)
	}
	if !s.Paused || s.Signature != "keep" {
		t.Fatalf("SetPaused(true) = %+v, want paused with signature kept", s)
	}

	if s, err := SetPaused(false); err != nil || s.Paused {
		t.Fatalf("SetPaused(false) = %+v, %v, want not paused", s, err)
	}
}
