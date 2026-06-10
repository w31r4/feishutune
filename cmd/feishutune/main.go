// Command feishutune keeps your Feishu personal signature in
// sync with the song currently playing in the local Spotify desktop app. Each
// run is one shot: `update` computes the signature and writes it to Feishu only
// when it changed. Scheduling is delegated to launchd (`install`), and a small
// state file remembers the last signature and whether now-playing is paused.
//
// Run `feishutune help` for the command list and configuration.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/Durden-T/feishutune/internal/feishu"
)

const progName = "feishutune"

// version is overridable at build time: -ldflags "-X main.version=v1.2.3".
var version = "dev"

func main() {
	c := cli{stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr}
	if err := c.run(os.Args[1:]); err != nil {
		code, show := classify(err)
		if show && err.Error() != "" {
			fmt.Fprintf(os.Stderr, "%s: %s\n", progName, err)
		}
		os.Exit(code)
	}
}

// cli carries the streams the commands read and write, so tests can drive them
// with buffers.
type cli struct {
	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer
}

func (c cli) run(args []string) error {
	if len(args) == 0 {
		c.usage()
		return silentExit(2)
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "-h", "--help":
		c.usage()
		return nil
	case "help":
		// `help <cmd>` defers to the subcommand's own -h; bare `help` is usage.
		if len(rest) > 0 {
			return c.run([]string{rest[0], "-h"})
		}
		c.usage()
		return nil
	case "-version", "--version", "version":
		fmt.Fprintln(c.stdout, version)
		return nil
	case "update":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return c.update(ctx, rest)
	case "preview":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return c.preview(ctx, rest)
	case "pause":
		return c.pause(rest)
	case "resume":
		return c.resume(rest)
	case "status":
		return c.status(rest)
	case "login":
		return c.login(rest)
	case "spotify-login":
		return c.spotifyLogin(rest)
	case "install":
		return c.install(rest)
	case "uninstall":
		return c.uninstall(rest)
	default:
		fmt.Fprintf(c.stderr, "%s: unknown command %q\n\n", progName, cmd)
		c.usage()
		return silentExit(2)
	}
}

func (c cli) usage() {
	fmt.Fprint(c.stdout, `feishutune — sync your Feishu signature to Spotify now-playing (macOS)

USAGE
  feishutune <command> [flags]

COMMANDS
  update        Compute the signature once and push it to Feishu if it changed
  preview       Print the signature for right now without writing it to Feishu
  pause         Stop showing now-playing; show your at-the-Mac/away status instead
  resume        Resume now-playing updates
  status        Print whether paused and the last signature written
  login         Store the Feishu session cookie, read from stdin
  spotify-login Store the Spotify sp_dc cookie (optional; adds ♡ on liked tracks)
  install       Install a launchd agent that runs update on an interval
  uninstall     Remove the launchd agent
  version       Print the version

EXAMPLES
  pbpaste | feishutune login         # save the Feishu session cookie from the clipboard
  pbpaste | feishutune spotify-login # save the Spotify sp_dc cookie (enables ♡ on liked tracks)
  feishutune install --interval 30s  # run update every 30s via launchd
  feishutune update --online afk      # one sync now, overriding the at-the-Mac status text
  feishutune preview                 # print what the signature looks like right now
  feishutune status --json
  feishutune pause                   # hide now-playing until you resume

Run "feishutune <command> -h" for a command's flags.
Config precedence: flags > env > ~/.feishutune/config.json > defaults.
Env: FEISHU_SESSION, SPOTIFY_SP_DC, ONLINE, OFFLINE, WEEKEND, IDLE_AFTER, BLACKLIST.
`)
}

// parseFlags parses a subcommand's flags, translating the flag package's own
// outcomes into exit codes: -h prints usage and exits 0; a bad flag has already
// been reported, so it exits 2 silently.
func parseFlags(fs *flag.FlagSet, args []string) error {
	switch err := fs.Parse(args); {
	case err == nil:
		return nil
	case errors.Is(err, flag.ErrHelp):
		return silentExit(0)
	default:
		return silentExit(2)
	}
}

// silentExit sets the process exit code without printing anything (the message,
// if any, was already written — e.g. by the flag package).
type silentExit int

func (silentExit) Error() string { return "" }

// codedError pairs an error with an explicit exit code; main prints it.
type codedError struct {
	code int
	err  error
}

func (e codedError) Error() string { return e.err.Error() }
func (e codedError) Unwrap() error { return e.err }

// classify maps an error to an exit code and whether main should print it.
func classify(err error) (code int, show bool) {
	if se, ok := errors.AsType[silentExit](err); ok {
		return int(se), false
	}
	if ce, ok := errors.AsType[codedError](err); ok {
		return ce.code, true
	}
	if errors.Is(err, feishu.ErrSessionExpired) {
		return 3, true
	}
	return 1, true
}
