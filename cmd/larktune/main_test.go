package main

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Durden-T/larktune/internal/feishu"
	"github.com/Durden-T/larktune/internal/spotifyliked"
	"github.com/Durden-T/larktune/internal/store"
)

// newCLI wires a cli to in-memory streams and returns the captured stdout/stderr.
func newCLI(stdin string) (cli, *bytes.Buffer, *bytes.Buffer) {
	var out, errb bytes.Buffer
	return cli{stdin: strings.NewReader(stdin), stdout: &out, stderr: &errb}, &out, &errb
}

func TestRunNoArgsIsUsageError(t *testing.T) {
	c, _, _ := newCLI("")
	if code, _ := classify(c.run(nil)); code != 2 {
		t.Fatalf("run(nil) exit = %d, want 2", code)
	}
}

func TestRunUnknownCommand(t *testing.T) {
	c, _, errb := newCLI("")
	code, show := classify(c.run([]string{"frobnicate"}))
	if code != 2 || show {
		t.Fatalf("classify = (%d, %v), want (2, false)", code, show)
	}
	if !strings.Contains(errb.String(), "unknown command") {
		t.Fatalf("stderr = %q, want 'unknown command'", errb.String())
	}
}

func TestRunVersion(t *testing.T) {
	c, out, _ := newCLI("")
	if err := c.run([]string{"version"}); err != nil {
		t.Fatalf("version: %v", err)
	}
	if strings.TrimSpace(out.String()) != version {
		t.Fatalf("version printed %q, want %q", out.String(), version)
	}
}

func TestRunHelp(t *testing.T) {
	c, out, _ := newCLI("")
	if err := c.run([]string{"-h"}); err != nil {
		t.Fatalf("-h: %v", err)
	}
	if !strings.Contains(out.String(), "USAGE") {
		t.Fatalf("help missing USAGE:\n%s", out.String())
	}
}

// `help <cmd>` defers to the subcommand's own -h (its flag list), not top-level usage.
func TestRunHelpSubcommand(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c, out, errb := newCLI("")
	// update -h exits 0 via flag.ErrHelp (a silentExit, non-nil but code 0).
	if code, _ := classify(c.run([]string{"help", "update"})); code != 0 {
		t.Fatalf("help update exit = %d, want 0", code)
	}
	// flag.PrintDefaults writes to stderr; the update flag list must appear there.
	if !strings.Contains(errb.String(), "-online") {
		t.Fatalf("help update stderr = %q, want update's -online flag", errb.String())
	}
	if strings.Contains(out.String(), "COMMANDS") {
		t.Fatalf("help update printed top-level usage, want the subcommand's flags")
	}
}

// resume reports usage errors under its own name, not "pause" (regression: setPaused
// once hardcoded "pause" for both commands).
func TestResumeRejectsArgsUnderOwnName(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c, _, _ := newCLI("")
	err := c.run([]string{"resume", "stray"})
	if code, _ := classify(err); code != 2 {
		t.Fatalf("resume with stray arg exit = %d, want 2", code)
	}
	if err == nil || !strings.Contains(err.Error(), "resume takes no arguments") {
		t.Fatalf("err = %v, want 'resume takes no arguments'", err)
	}
}

func TestLoginSavesFromStdin(t *testing.T) {
	t.Setenv("FEISHU_SESSION", "")
	t.Setenv("HOME", t.TempDir())

	c, out, _ := newCLI("tok-from-stdin\n")
	if err := c.run([]string{"login"}); err != nil {
		t.Fatalf("login: %v", err)
	}
	if !strings.Contains(out.String(), "saved") {
		t.Fatalf("stdout = %q, want a confirmation", out.String())
	}
	got, err := feishu.LoadSession()
	if err != nil {
		t.Fatalf("LoadSession after login: %v", err)
	}
	if got != "tok-from-stdin" {
		t.Fatalf("saved session = %q, want %q", got, "tok-from-stdin")
	}
}

func TestLoginEmptyStdinIsUsageError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c, _, _ := newCLI("")
	if code, _ := classify(c.run([]string{"login"})); code != 2 {
		t.Fatalf("login with empty stdin exit = %d, want 2", code)
	}
}

func TestSpotifyLoginSavesFromStdin(t *testing.T) {
	t.Setenv("SPOTIFY_SP_DC", "")
	t.Setenv("HOME", t.TempDir())

	c, out, _ := newCLI("sp-dc-from-stdin\n")
	if err := c.run([]string{"spotify-login"}); err != nil {
		t.Fatalf("spotify-login: %v", err)
	}
	if !strings.Contains(out.String(), "saved") {
		t.Fatalf("stdout = %q, want a confirmation", out.String())
	}
	if got := spotifyliked.LoadSPDC(); got != "sp-dc-from-stdin" {
		t.Fatalf("saved sp_dc = %q, want %q", got, "sp-dc-from-stdin")
	}
}

func TestSpotifyLoginEmptyStdinIsUsageError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c, _, _ := newCLI("")
	if code, _ := classify(c.run([]string{"spotify-login"})); code != 2 {
		t.Fatalf("spotify-login with empty stdin exit = %d, want 2", code)
	}
}

func TestPauseStatusResume(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	c, _, _ := newCLI("")
	if err := c.run([]string{"pause"}); err != nil {
		t.Fatalf("pause: %v", err)
	}
	if st, _ := store.Load(); !st.Paused {
		t.Fatal("after pause, state is not paused")
	}

	c2, out, _ := newCLI("")
	if err := c2.run([]string{"status", "--json"}); err != nil {
		t.Fatalf("status: %v", err)
	}
	if !strings.Contains(out.String(), `"paused":true`) {
		t.Fatalf("status --json = %q, want paused:true", out.String())
	}

	c3, _, _ := newCLI("")
	if err := c3.run([]string{"resume"}); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if st, _ := store.Load(); st.Paused {
		t.Fatal("after resume, state is still paused")
	}
}

func TestStatusRejectsExtraArgsViaFlagParser(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c, _, _ := newCLI("")
	// An unknown flag is reported by the flag package and exits 2 silently.
	if code, show := classify(c.run([]string{"status", "--bogus"})); code != 2 || show {
		t.Fatalf("classify = (%d, %v), want (2, false)", code, show)
	}
}

func TestUpdateBadIdleAfterIsUsageError(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c, _, _ := newCLI("")

	err := c.run([]string{"update", "--idle-after", "nope"})
	if code, _ := classify(err); code != 2 {
		t.Fatalf("bad --idle-after exit = %d, want 2", code)
	}
	if !strings.Contains(err.Error(), "idle-after") {
		t.Fatalf("error = %q, want it to name idle-after", err)
	}
}

func TestUpdateMissingSessionFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("FEISHU_SESSION", "")
	t.Setenv("IDLE_AFTER", "10m") // a valid threshold so ToBio succeeds and we reach the session check
	c, _, _ := newCLI("")

	err := c.run([]string{"update"})
	if err == nil {
		t.Fatal("update without a session: want error, got nil")
	}
	if code, _ := classify(err); code != 1 {
		t.Fatalf("missing-session exit = %d, want 1", code)
	}
}

func TestPrintUpdate(t *testing.T) {
	cases := []struct {
		name   string
		res    updateResult
		asJSON bool
		quiet  bool
		want   string // "" means: expect empty output
	}{
		{"changed prints set line", updateResult{Changed: true, Signature: "在线"}, false, false, "set: 在线\n"},
		{"unchanged is silent", updateResult{Signature: "在线"}, false, false, ""},
		{"quiet is silent even on change", updateResult{Changed: true, Signature: "在线"}, false, true, ""},
		{"json always prints", updateResult{Paused: true, Signature: "离线"}, true, false, `"signature":"离线"`},
		{"blocked prints a notice", updateResult{Blocked: true, Signature: "在线"}, false, false, "blocked: signature withheld (matched blacklist)\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, out, _ := newCLI("")
			c.printUpdate(tc.res, tc.asJSON, tc.quiet)
			switch {
			case tc.want == "":
				if out.String() != "" {
					t.Fatalf("output = %q, want empty", out.String())
				}
			case tc.asJSON:
				if !strings.Contains(out.String(), tc.want) {
					t.Fatalf("output = %q, want it to contain %q", out.String(), tc.want)
				}
			default:
				if out.String() != tc.want {
					t.Fatalf("output = %q, want %q", out.String(), tc.want)
				}
			}
		})
	}
}

func TestStatusPlain(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c, out, _ := newCLI("")
	if err := c.run([]string{"status"}); err != nil {
		t.Fatalf("status: %v", err)
	}
	if s := out.String(); !strings.Contains(s, "active") || !strings.Contains(s, "(none yet)") {
		t.Fatalf("status = %q, want 'active' and '(none yet)'", s)
	}
}

func TestInstallRejectsTinyInterval(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c, _, _ := newCLI("")
	if code, _ := classify(c.run([]string{"install", "--interval", "0s"})); code != 2 {
		t.Fatalf("install --interval 0s exit = %d, want 2", code)
	}
}

// TestUninstall is safe to run: launchctl unload of a never-loaded plist is a
// no-op (its error is ignored) and the plist lives under a temp HOME.
func TestUninstall(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c, out, _ := newCLI("")
	if err := c.run([]string{"uninstall"}); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if !strings.Contains(out.String(), "removed") {
		t.Fatalf("stdout = %q, want 'removed'", out.String())
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code int
		show bool
	}{
		{"silent", silentExit(2), 2, false},
		{"coded", codedError{2, errors.New("bad flag")}, 2, true},
		{"session expired", fmt.Errorf("set: %w", feishu.ErrSessionExpired), 3, true},
		{"generic", errors.New("boom"), 1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, show := classify(tc.err)
			if code != tc.code || show != tc.show {
				t.Fatalf("classify(%v) = (%d, %v), want (%d, %v)", tc.err, code, show, tc.code, tc.show)
			}
		})
	}
}
