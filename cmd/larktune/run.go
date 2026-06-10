package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/Durden-T/larktune/internal/bio"
	"github.com/Durden-T/larktune/internal/store"
)

// Player reads the current track. Implemented by package spotify.
type Player interface {
	NowPlaying(context.Context) (bio.Track, error)
}

// IdleReader reports how long the Mac has gone without user input. Implemented
// by package idle.
type IdleReader interface {
	Idle(context.Context) (time.Duration, error)
}

// Publisher writes the signature. Implemented by package feishu.
type Publisher interface {
	Set(context.Context, string) error
}

// Liker reports whether a track (by Spotify URI) is in the user's Liked Songs.
// Implemented by package spotifyliked; disabled (always false) when no Spotify
// cookie is configured.
type Liker interface {
	Liked(ctx context.Context, trackURI string) (bool, error)
}

// updateResult reports the outcome of one update for human and JSON output.
type updateResult struct {
	Changed   bool   `json:"changed"`
	Blocked   bool   `json:"blocked"`
	Paused    bool   `json:"paused"`
	Signature string `json:"signature"`
}

// update computes the signature for now and writes it to Feishu only when it
// differs from the last one stored in state. A signature matching the blacklist
// is withheld entirely (reported as blocked, nothing written). A failed write
// leaves the stored signature unchanged so the next run retries. A NowPlaying,
// idle-read, or liked-lookup error is reported to warnw and tolerated — a Spotify
// hiccup is treated as "nothing playing", an idle-read hiccup as "at the Mac",
// and a liked-lookup hiccup as "not liked" — so the run degrades to a sensible
// signature rather than blanking it or failing; a rejected Feishu write is a hard
// error and propagates.
func update(ctx context.Context, policy bio.Policy, player Player, idleR IdleReader, pub Publisher, liked Liker, now time.Time, warnw io.Writer) (updateResult, error) {
	st, err := store.Load()
	if err != nil {
		return updateResult{}, err
	}

	active := activeNow(ctx, policy, idleR, warnw)
	var track bio.Track
	if !st.Paused && active {
		track = nowPlayingTrack(ctx, player, liked, warnw)
	}

	text := policy.Compose(now, track, st.Paused, active)
	res := updateResult{Paused: st.Paused, Signature: st.Signature}
	if policy.Blocked(text) {
		res.Blocked = true
		return res, nil // a blacklisted signature is never published
	}
	if text == st.Signature {
		return res, nil
	}
	if err := pub.Set(ctx, text); err != nil {
		return res, err // leave the stored signature unchanged so the next run retries
	}
	st.Signature = text
	if err := st.Save(); err != nil {
		return res, err
	}
	res.Changed = true
	res.Signature = text
	return res, nil
}

// previewLine renders the signature for right now without writing it anywhere: a
// playing track shows its now-playing line (with a ♡ when liked), anything else
// the idle status. A player, liked, or idle read error is reported to warnw and
// tolerated the same way update tolerates it.
func previewLine(ctx context.Context, policy bio.Policy, player Player, idleR IdleReader, liked Liker, now time.Time, warnw io.Writer) string {
	track := nowPlayingTrack(ctx, player, liked, warnw)
	return policy.Preview(track, now, activeNow(ctx, policy, idleR, warnw))
}

// nowPlayingTrack reads the current track and, when something is playing, fills
// in its liked status. A player read error is surfaced to warnw and tolerated as
// "nothing playing"; a liked-lookup error is surfaced and tolerated as "not
// liked" — so neither can fail the run or blank the signature.
func nowPlayingTrack(ctx context.Context, player Player, liked Liker, warnw io.Writer) bio.Track {
	track, err := player.NowPlaying(ctx)
	if err != nil {
		fmt.Fprintf(warnw, "now playing: %v\n", err)
		return bio.Track{}
	}
	if track.Playing {
		track.Liked = likedNow(ctx, liked, track.ID, warnw)
	}
	return track
}

// likedNow reports whether the track is in Liked Songs, surfacing any lookup
// error to warnw and treating it as not liked.
func likedNow(ctx context.Context, liked Liker, trackURI string, warnw io.Writer) bool {
	ok, err := liked.Liked(ctx, trackURI)
	if err != nil {
		fmt.Fprintf(warnw, "liked: %v\n", err)
		return false
	}
	return ok
}

// activeNow reports whether the Mac counts as active right now. A failure to read
// the idle time is reported to warnw and treated as active, so a transient ioreg
// hiccup keeps showing your status instead of marking you away.
func activeNow(ctx context.Context, policy bio.Policy, idleR IdleReader, warnw io.Writer) bool {
	idle, err := idleR.Idle(ctx)
	if err != nil {
		fmt.Fprintf(warnw, "idle: %v\n", err)
		return true
	}
	return policy.Active(idle)
}
