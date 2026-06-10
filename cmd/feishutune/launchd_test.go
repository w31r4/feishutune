package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRenderPlist(t *testing.T) {
	got := renderPlist("my&label", "/usr/local/bin/feishutune", "/tmp/agent.log", 30*time.Second)
	for _, want := range []string{
		"<string>my&amp;label</string>", // label, XML-escaped
		"<string>/usr/local/bin/feishutune</string>",
		"<string>update</string>",
		"<integer>30</integer>",
		"<key>RunAtLoad</key>",
		"<true/>",
		"<string>/tmp/agent.log</string>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("plist missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderPlistClampsInterval(t *testing.T) {
	if got := renderPlist("l", "exe", "log", 0); !strings.Contains(got, "<integer>1</integer>") {
		t.Fatalf("interval 0 not clamped to 1s:\n%s", got)
	}
}

func TestPlistPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	got, err := plistPath("feishutune")
	if err != nil {
		t.Fatalf("plistPath: %v", err)
	}
	want := filepath.Join(home, "Library", "LaunchAgents", "feishutune.plist")
	if got != want {
		t.Fatalf("plistPath = %q, want %q", got, want)
	}
}

// TestWriteAgent covers the file-writing half of install without the launchctl
// side effect, so the test never registers a real launchd job.
func TestWriteAgent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, logPath, err := writeAgent("feishutune", time.Minute)
	if err != nil {
		t.Fatalf("writeAgent: %v", err)
	}
	if want := filepath.Join(home, "Library", "LaunchAgents", "feishutune.plist"); path != want {
		t.Errorf("plist path = %q, want %q", path, want)
	}
	if want := filepath.Join(home, ".feishutune", "agent.log"); logPath != want {
		t.Errorf("log path = %q, want %q", logPath, want)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read plist: %v", err)
	}
	for _, want := range []string{"<string>update</string>", "<integer>60</integer>", logPath} {
		if !strings.Contains(string(b), want) {
			t.Errorf("plist missing %q:\n%s", want, b)
		}
	}
}
