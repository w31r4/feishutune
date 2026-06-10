package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Durden-T/feishutune/internal/store"
)

// defaultLabel names the launchd agent. launchctl identifies the job by it, so
// install and uninstall must agree.
const defaultLabel = "feishutune"

// plistPath returns the LaunchAgent path for label
// (~/Library/LaunchAgents/<label>.plist).
func plistPath(label string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("launchd: locate home: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist"), nil
}

// renderPlist builds a launchd agent that runs `<exe> update` every interval,
// at load, and on each tick, with output appended to logPath. PATH is pinned so
// the agent's minimal environment can still find osascript.
func renderPlist(label, exe, logPath string, interval time.Duration) string {
	secs := max(int(interval.Seconds()), 1)
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>update</string>
	</array>
	<key>EnvironmentVariables</key>
	<dict>
		<key>PATH</key>
		<string>/usr/bin:/bin:/usr/sbin:/sbin</string>
	</dict>
	<key>StartInterval</key>
	<integer>%d</integer>
	<key>RunAtLoad</key>
	<true/>
	<key>StandardOutPath</key>
	<string>%s</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
</dict>
</plist>
`, xmlEscape(label), xmlEscape(exe), secs, xmlEscape(logPath), xmlEscape(logPath))
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// install writes the agent plist for this executable and (re)loads it via
// launchctl, returning the plist and log paths.
func install(label string, interval time.Duration) (path, logPath string, err error) {
	path, logPath, err = writeAgent(label, interval)
	if err != nil {
		return "", "", err
	}
	if err := loadAgent(path); err != nil {
		return "", "", err
	}
	return path, logPath, nil
}

// writeAgent renders and writes the agent plist for the current executable,
// returning the plist path and the log path it points at. It performs no
// launchctl side effects, so it is safe to call outside a real install.
func writeAgent(label string, interval time.Duration) (path, logPath string, err error) {
	exe, err := os.Executable()
	if err != nil {
		return "", "", fmt.Errorf("launchd: find executable: %w", err)
	}

	dir, err := store.Dir()
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", fmt.Errorf("launchd: create %s: %w", dir, err)
	}
	logPath = filepath.Join(dir, "agent.log")

	path, err = plistPath(label)
	if err != nil {
		return "", "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", "", fmt.Errorf("launchd: create %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(renderPlist(label, exe, logPath, interval)), 0o644); err != nil {
		return "", "", fmt.Errorf("launchd: write %s: %w", path, err)
	}
	return path, logPath, nil
}

// loadAgent (re)loads the plist, unloading any previous version first so an
// upgraded binary path takes effect.
func loadAgent(path string) error {
	_ = exec.Command("launchctl", "unload", path).Run() // ignore: not loaded yet on first install
	if out, err := exec.Command("launchctl", "load", path).CombinedOutput(); err != nil {
		return fmt.Errorf("launchd: launchctl load: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// uninstall unloads the agent and removes its plist, returning the plist path.
// A missing plist is not an error.
func uninstall(label string) (string, error) {
	path, err := plistPath(label)
	if err != nil {
		return "", err
	}
	_ = exec.Command("launchctl", "unload", path).Run() // ignore: may already be unloaded
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("launchd: remove %s: %w", path, err)
	}
	return path, nil
}
