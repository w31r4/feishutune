# feishutune

[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](go.mod)
[![Platform: macOS](https://img.shields.io/badge/platform-macOS-lightgrey?logo=apple&logoColor=white)](#requirements)

English | [简体中文](README.zh-CN.md)

Keep your Feishu personal signature in sync with whatever is playing in your
**local music app** — **Spotify** or **QQ Music (QQ音乐)**, auto-detected — on macOS.

```text
♫ Clair de Lune ♡ · Debussy  2:11 ━━━━●───── 5:08
```

When a track is playing and you're at your Mac, your signature becomes that
now-playing line: the song, a ♡ if it's one of your liked tracks, and a live
progress scrubber flanked by the elapsed and total time. Otherwise it falls
back to a one-word status — `online` (at the Mac), `away` (stepped away past
the idle threshold), or `weekend` (idle on a weekend).

## How it works

Each `update` run is **one shot**: read the player, compose the signature, and
write it to Feishu *only if it changed*. There's no background daemon — a
`launchd` agent just runs `update` on an interval (every minute by default), and
the change-detection means almost all of those ticks are cheap no-ops.

- **Spotify** is read locally via AppleScript (`osascript`) — this Mac only, not
  phones or Spotify Connect devices.
- **QQ Music** has no AppleScript support, so it's read from the system "Now
  Playing" info it publishes to macOS, via the [`media-control`](https://github.com/ungive/media-control)
  CLI. Spotify is tried first, then QQ Music — whichever is actually playing wins.
- **Idle detection** uses `ioreg` (`HIDIdleTime`) to tell whether you're at the
  keyboard, so it can switch to the away status when you step away.
- **Feishu** is updated with a cookie-authenticated `PUT` to the same web
  endpoint the browser client uses — an unofficial API, so a Feishu-side change
  could break it.
- The **♡** on liked tracks is optional. For Spotify it's read from Spotify's
  internal web GraphQL using your `sp_dc` cookie; for QQ Music it's read from the
  app's local favorites library (no login needed), matched by song name + artist.
  Without either, the tool runs unchanged, just without the heart.

The tool is error-tolerant by design: a player read error shows the idle status
instead of failing, an idle read error assumes you're present, a liked-status
error just drops the ♡, and a failed Feishu write is retried naturally on the
next tick.

## Requirements

- macOS (relies on `osascript`, `ioreg`, `sqlite3`, and `launchd`)
- A music app: the Spotify desktop app, and/or the QQ Music desktop app
- For QQ Music only: [`media-control`](https://github.com/ungive/media-control)
  — `brew install media-control` (Spotify-only users don't need it)
- Go 1.26+ to build
- A Feishu account (tested against Feishu; not verified on Lark, the global
  edition)

## Install

```bash
go install github.com/Durden-T/feishutune/cmd/feishutune@latest
```

This installs to `$GOBIN` (usually `~/go/bin`); make sure that's on your `PATH`.
To update later, re-run the same command — a scheduled agent runs the new binary
on its next tick, no reload needed.

## Setup

### 1. Store your Feishu session cookie

Log in to Feishu in your browser, copy the `session` cookie value, and pipe it
in:

```bash
pbpaste | feishutune login
```

The cookie is valid for ~350 days and is stored at `~/.feishutune/session`. You
can also pass it via the `FEISHU_SESSION` environment variable.

### 2. (Optional) Enable the ♡ on liked tracks

**Spotify:** grab the `sp_dc` cookie from a logged-in `open.spotify.com` browser
session (it's HttpOnly — read it from devtools), then:

```bash
pbpaste | feishutune spotify-login
```

Valid for ~1 year, stored at `~/.feishutune/sp_dc`. Or set `SPOTIFY_SP_DC`.

**QQ Music:** nothing to set up — the ♡ is read straight from the app's local
favorites (我喜欢), matched by song name + artist. Just be logged into the QQ
Music app so your favorites are synced locally. (Because it matches on text
rather than a stable ID, it's best-effort and could miss tracks that share a
name and artist with a different version.)

### 3. Try it

```bash
feishutune preview   # render the live signature WITHOUT writing to Feishu
feishutune update    # compute once and push to Feishu if it changed
```

### 4. Schedule it

```bash
feishutune install                  # run `update` every minute via launchd
feishutune install --interval 30s   # or pick your own interval
```

`install` writes a LaunchAgent to `~/Library/LaunchAgents/feishutune.plist` and
loads it. To stop:

```bash
feishutune uninstall
```

## Commands

| Command         | What it does                                                   |
| --------------- | ------------------------------------------------------------- |
| `update`        | Compute the signature once and push to Feishu if it changed   |
| `preview`       | Print the signature for right now without writing it          |
| `pause`         | Hide now-playing; show your at-the-Mac/away status instead    |
| `resume`        | Resume now-playing updates                                     |
| `status`        | Print whether paused and the last signature written           |
| `login`         | Store the Feishu session cookie (from stdin)                  |
| `spotify-login` | Store the Spotify `sp_dc` cookie for the Spotify ♡ (from stdin) |
| `install`       | Install a launchd agent that runs `update` on an interval     |
| `uninstall`     | Remove the launchd agent                                       |
| `version`       | Print the version                                              |

Run `feishutune <command> -h` for a command's flags. `update` and `status` accept
`--json`; `update` also accepts `--quiet`.

## Configuration

Settings are layered, later sources win:

```
defaults  <  ~/.feishutune/config.json  <  environment  <  command-line flags
```

| Setting    | Flag           | Env           | Default     | Meaning                                          |
| ---------- | -------------- | ------------- | ----------- | ------------------------------------------------ |
| Online     | `--online`     | `ONLINE`      | `online`    | Status when at the Mac with nothing playing      |
| Offline    | `--offline`    | `OFFLINE`     | `away`      | Status when away from the Mac                    |
| Weekend    | `--weekend`    | `WEEKEND`     | `weekend`   | Status when idle on weekends                     |
| Idle after | `--idle-after` | `IDLE_AFTER`  | `10m`       | Idle time before counting as away (Go duration)  |
| Blacklist  | `--blacklist`  | `BLACKLIST`   | (none)      | Comma-separated substrings that suppress publishing |

Example `~/.feishutune/config.json`:

```json
{
  "online": "afk",
  "idle_after": "5m",
  "blacklist": "podcast,white noise"
}
```

A blacklist match suppresses publishing entirely — nothing is written, and the
run reports it as blocked.

## Files

Everything lives under `~/.feishutune/`:

- `session` — the Feishu session cookie
- `sp_dc` — the Spotify cookie for the Spotify ♡ (if set)
- `config.json` — optional config overrides
- `state.json` — last signature written and the paused flag
- `spotify-cache.json` — cached Spotify tokens and per-track liked results
- `agent.log` — stdout and stderr from the scheduled launchd runs (for debugging)

(QQ Music needs no files here — its now-playing comes from `media-control` and
its ♡ is read directly from the QQ Music app's own library.)

Your cookies are stored as plaintext (not in the macOS Keychain), but the files
are owner-only — the directory is `0700` and each file is `0600`.

## Exit codes

| Code | Meaning                                       |
| ---- | --------------------------------------------- |
| `0`  | OK                                            |
| `1`  | Other error                                   |
| `2`  | Usage error                                   |
| `3`  | Feishu session expired or invalid — re-`login`|

## Troubleshooting

- **Nothing's updating.** Check `feishutune status` (last signature, paused?) and
  `feishutune preview` (what it would write right now). The scheduled agent appends
  every run to `~/.feishutune/agent.log` — read it for errors — and `launchctl list
  | grep feishutune` confirms it's loaded.
- **Exit code 3 / "session expired".** The Feishu cookie lapsed (~350 days). Re-run
  `pbpaste | feishutune login`.
- **No ♡ on Spotify tracks.** The `sp_dc` cookie is missing or expired (~1 year); the
  log notes when to re-auth. Grab a fresh one and `pbpaste | feishutune spotify-login`.
- **No ♡ on QQ Music tracks.** Stay logged into the QQ Music app so 我喜欢 syncs to the
  local library. Matching is by song name + artist, so an alternate version can be missed.
- **QQ Music isn't detected.** Install `media-control` (`brew install media-control`); the
  agent looks on `/opt/homebrew/bin` (Apple silicon) and `/usr/local/bin` (Intel).

## Development

```bash
go build -o feishutune ./cmd/feishutune   # local binary
go test ./...                          # full suite (live tests self-skip)
go vet ./...
```

The architecture is ports-and-adapters with a pure domain core
(`internal/bio`); see [CLAUDE.md](CLAUDE.md) for a full tour.

## License

[MIT](LICENSE)
