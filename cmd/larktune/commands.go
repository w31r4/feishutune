package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/Durden-T/larktune/internal/config"
	"github.com/Durden-T/larktune/internal/feishu"
	"github.com/Durden-T/larktune/internal/idle"
	"github.com/Durden-T/larktune/internal/spotify"
	"github.com/Durden-T/larktune/internal/spotifyliked"
	"github.com/Durden-T/larktune/internal/store"
)

// maxSessionBytes caps `login` stdin; a session cookie is well under this.
const maxSessionBytes = 1 << 16

func (c cli) update(ctx context.Context, args []string) error {
	pol, err := config.Load()
	if err != nil {
		return err
	}
	fs := c.flagSet("update")
	fs.StringVar(&pol.Online, "online", pol.Online, "status when at the Mac with nothing playing")
	fs.StringVar(&pol.Offline, "offline", pol.Offline, "status when away from the Mac")
	fs.StringVar(&pol.Weekend, "weekend", pol.Weekend, "status when idle on weekends")
	fs.StringVar(&pol.IdleAfter, "idle-after", pol.IdleAfter, "idle time before counting as away, e.g. 10m")
	fs.StringVar(&pol.Blacklist, "blacklist", pol.Blacklist, "comma-separated substrings that suppress publishing")
	quiet := fs.Bool("quiet", false, "only print on error")
	fs.BoolVar(quiet, "q", false, "shorthand for --quiet")
	asJSON := fs.Bool("json", false, "print the result as JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	policy, err := pol.ToBio()
	if err != nil {
		return codedError{2, err}
	}
	session, err := feishu.LoadSession()
	if err != nil {
		return err
	}

	res, err := update(ctx, policy, spotify.New(), idle.New(), feishu.New(session), spotifyliked.New(spotifyliked.LoadSPDC()), time.Now(), c.stderr)
	if err != nil {
		return err
	}
	c.printUpdate(res, *asJSON, *quiet)
	return nil
}

func (c cli) printUpdate(res updateResult, asJSON, quiet bool) {
	switch {
	case asJSON:
		c.writeJSON(res)
	case quiet:
		return
	case res.Blocked:
		fmt.Fprintln(c.stdout, "blocked: signature withheld (matched blacklist)")
	case !res.Changed:
		return
	default:
		fmt.Fprintf(c.stdout, "set: %s\n", res.Signature)
	}
}

// preview prints the signature for right now — the live track's now-playing line,
// or the idle status when nothing is playing — without writing anything to Feishu
// or touching stored state, so it is safe to run at any time to check the format.
func (c cli) preview(ctx context.Context, args []string) error {
	if err := c.noArgs("preview", args); err != nil {
		return err
	}
	pol, err := config.Load()
	if err != nil {
		return err
	}
	policy, err := pol.ToBio()
	if err != nil {
		return codedError{2, err}
	}
	fmt.Fprintln(c.stdout, previewLine(ctx, policy, spotify.New(), idle.New(), spotifyliked.New(spotifyliked.LoadSPDC()), time.Now(), c.stderr))
	return nil
}

func (c cli) status(args []string) error {
	fs := c.flagSet("status")
	asJSON := fs.Bool("json", false, "print the state as JSON")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	st, err := store.Load()
	if err != nil {
		return err
	}
	if *asJSON {
		c.writeJSON(st)
		return nil
	}
	state := "active"
	if st.Paused {
		state = "paused"
	}
	sig := st.Signature
	if sig == "" {
		sig = "(none yet)"
	}
	fmt.Fprintf(c.stdout, "%s\nsignature: %s\n", state, sig)
	return nil
}

func (c cli) pause(args []string) error  { return c.setPaused("pause", args, true) }
func (c cli) resume(args []string) error { return c.setPaused("resume", args, false) }

func (c cli) setPaused(name string, args []string, paused bool) error {
	if err := c.noArgs(name, args); err != nil {
		return err
	}
	if _, err := store.SetPaused(paused); err != nil {
		return err
	}
	verb := "paused"
	if !paused {
		verb = "resumed"
	}
	fmt.Fprintf(c.stdout, "%s — applies on the next scheduled run; `larktune update` applies it now\n", verb)
	return nil
}

func (c cli) login(args []string) error {
	if err := c.noArgs("login", args); err != nil {
		return err
	}
	b, err := io.ReadAll(io.LimitReader(c.stdin, maxSessionBytes))
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	if len(b) == 0 {
		return codedError{2, errors.New("no session token on stdin (e.g. `pbpaste | larktune login`)")}
	}
	if err := feishu.SaveSession(string(b)); err != nil {
		return err
	}
	fmt.Fprintln(c.stdout, "saved session token")
	return nil
}

// spotifyLogin stores the Spotify `sp_dc` login cookie, read from stdin, enabling
// the ♡ on liked now-playing tracks. It is optional: without it the tool runs
// unchanged, just without the heart. The cookie lasts ~1 year.
func (c cli) spotifyLogin(args []string) error {
	if err := c.noArgs("spotify-login", args); err != nil {
		return err
	}
	b, err := io.ReadAll(io.LimitReader(c.stdin, maxSessionBytes))
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}
	if len(b) == 0 {
		return codedError{2, errors.New("no sp_dc cookie on stdin (e.g. `pbpaste | larktune spotify-login`)")}
	}
	if err := spotifyliked.SaveSPDC(string(b)); err != nil {
		return err
	}
	fmt.Fprintln(c.stdout, "saved Spotify sp_dc cookie")
	return nil
}

func (c cli) install(args []string) error {
	fs := c.flagSet("install")
	interval := fs.Duration("interval", time.Minute, "how often to run update")
	label := fs.String("label", defaultLabel, "launchd agent label")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if *interval < time.Second {
		return codedError{2, errors.New("--interval must be at least 1s")}
	}
	path, logPath, err := install(*label, *interval)
	if err != nil {
		return err
	}
	fmt.Fprintf(c.stdout, "installed %s\nrunning `update` every %s; logs: %s\n", path, *interval, logPath)
	return nil
}

func (c cli) uninstall(args []string) error {
	fs := c.flagSet("uninstall")
	label := fs.String("label", defaultLabel, "launchd agent label")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	path, err := uninstall(*label)
	if err != nil {
		return err
	}
	fmt.Fprintf(c.stdout, "removed %s\n", path)
	return nil
}

// noArgs parses args for a command that accepts flags but no positional
// arguments, reporting any extras as a usage error.
func (c cli) noArgs(name string, args []string) error {
	fs := c.flagSet(name)
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return codedError{2, fmt.Errorf("%s takes no arguments, got %q", name, fs.Arg(0))}
	}
	return nil
}

func (c cli) flagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(c.stderr)
	return fs
}

func (c cli) writeJSON(v any) {
	enc := json.NewEncoder(c.stdout)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}
