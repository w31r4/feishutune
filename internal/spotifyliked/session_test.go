package spotifyliked

import "testing"

// TestLoadSaveSPDC covers the optional cookie: absent yields "" (lookups
// disabled), a saved cookie round-trips trimmed, the env var overrides the file,
// and a blank cookie is rejected on save.
func TestLoadSaveSPDC(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv(spDCEnv, "")

	if got := LoadSPDC(); got != "" {
		t.Fatalf("LoadSPDC() with nothing configured = %q, want empty", got)
	}
	if err := SaveSPDC("  mycookie\n"); err != nil {
		t.Fatalf("SaveSPDC: %v", err)
	}
	if got := LoadSPDC(); got != "mycookie" {
		t.Fatalf("LoadSPDC() = %q, want trimmed mycookie", got)
	}
	t.Setenv(spDCEnv, "envcookie")
	if got := LoadSPDC(); got != "envcookie" {
		t.Fatalf("LoadSPDC() with env set = %q, want envcookie (env wins)", got)
	}
	if err := SaveSPDC("   "); err == nil {
		t.Fatal("SaveSPDC(blank) must error")
	}
}
