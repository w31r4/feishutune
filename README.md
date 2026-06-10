# feishutune

Keep your Feishu personal signature in sync with whatever is
playing in your **local Spotify desktop app** — on macOS.

```
♫ Nocturne · Chopin ♡ ▱▱▰▰▰▰▱▱▱▱
```

When a track is playing and you're at your Mac, your signature becomes the
now-playing line. Otherwise it falls back to a status: `online` (at the Mac),
`away` (away from the Mac), or `weekend` (idle on a weekend).

## How it works

Each `update` run is **one shot**: read Spotify, compose the signature, and
write it to Feishu *only if it changed*. There's no background daemon — a
`launchd` agent just runs `update` on an interval (every minute by default), and
the change-detection means almost all of those ticks are cheap no-ops.

- **Spotify** is read locally via AppleScript (`osascript`) — this Mac only, not
  phones or Spotify Connect devices.
- **Idle detection** uses `ioreg` (`HIDIdleTime`) to tell whether you're at the
  keyboard, so it can switch to the away status when you step away.
- **Feishu** is updated with a cookie-authenticated `PUT` to the web endpoint.
- The **♡** on liked tracks is optional and read from Spotify's internal web
  GraphQL using your `sp_dc` cookie; without it the tool runs unchanged, just
  without the heart.

The tool is error-tolerant by design: a Spotify read error shows the idle status
instead of failing, an idle read error assumes you're present, a liked-status
error just drops the ♡, and a failed Feishu write is retried naturally on the
next tick.

## Requirements

- macOS (relies on `osascript`, `ioreg`, and `launchd`)
- The Spotify desktop app
- Go 1.26+ to build
- A Feishu account (tested against Feishu; not verified on Lark, the global
  edition)

## Install

```bash
go install github.com/Durden-T/feishutune/cmd/feishutune@latest
```

This installs to `$GOBIN` (usually `~/go/bin`); make sure that's on your `PATH`.

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

Grab the `sp_dc` cookie from a logged-in `open.spotify.com` browser session
(it's HttpOnly — read it from devtools), then:

```bash
pbpaste | feishutune spotify-login
```

Valid for ~1 year, stored at `~/.feishutune/sp_dc`. Or set `SPOTIFY_SP_DC`.

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
| `spotify-login` | Store the Spotify `sp_dc` cookie (from stdin)                 |
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
- `sp_dc` — the Spotify cookie (if set)
- `config.json` — optional config overrides
- `state.json` — last signature written and the paused flag
- `spotify-cache.json` — cached Spotify tokens and per-track liked results

## Exit codes

| Code | Meaning                                       |
| ---- | --------------------------------------------- |
| `0`  | OK                                            |
| `1`  | Other error                                   |
| `2`  | Usage error                                   |
| `3`  | Feishu session expired or invalid — re-`login`|

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
