package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/Durden-T/feishutune/internal/bio"
	"github.com/Durden-T/feishutune/internal/store"
)

// Player reads the current track. Implemented by each music source adapter.
type Player interface {
	NowPlaying(context.Context) (bio.Track, error)
}

// TrackEnhancer optionally enriches a playing track with provider metadata such
// as a stable id. Failures are warned and ignored by orchestration.
type TrackEnhancer interface {
	Enhance(context.Context, bio.Track) (bio.Track, error)
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

// Liker reports whether a track is in the user's liked/favorite songs.
// Implemented per player: neteaseliked and spotifyliked prefer provider track
// ids; qqmusicliked matches on name+artist.
type Liker interface {
	Liked(ctx context.Context, track bio.Track) (bool, error)
}

// source pairs a now-playing reader with the liked lookup for that same player,
// so the ♡ uses each player's own favorites. update and previewLine try sources
// in order and take the first that is actually playing.
type source struct {
	player   Player
	enhancer TrackEnhancer
	liker    Liker
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
// idle-read, or liked-lookup error is reported to warnw and tolerated — a player
// hiccup is treated as "nothing playing", an idle-read hiccup as "at the Mac",
// and a liked-lookup hiccup as "not liked" — so the run degrades to a sensible
// signature rather than blanking it or failing; a rejected Feishu write is a hard
// error and propagates.
func update(ctx context.Context, policy bio.Policy, sources []source, idleR IdleReader, pub Publisher, now time.Time, warnw io.Writer) (updateResult, error) {
	st, err := store.Load()
	if err != nil {
		return updateResult{}, err
	}

	active := activeNow(ctx, policy, idleR, warnw)
	var track bio.Track
	if !st.Paused && active {
		track = nowPlayingTrack(ctx, sources, warnw)
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
func previewLine(ctx context.Context, policy bio.Policy, sources []source, idleR IdleReader, now time.Time, warnw io.Writer) string {
	track := nowPlayingTrack(ctx, sources, warnw)
	return policy.Preview(track, now, activeNow(ctx, policy, idleR, warnw))
}

// nowPlayingTrack returns the first playing track across the sources, in order,
// with its liked status filled in from that source's own lookup — so Spotify and
// QQ Music each contribute their own ♡. A player read error is surfaced to warnw
// and that source is skipped (treated as "nothing playing"); a liked-lookup error
// is surfaced and treated as "not liked". Nothing playing anywhere yields the
// zero Track, which Compose renders as the idle status.
func nowPlayingTrack(ctx context.Context, sources []source, warnw io.Writer) bio.Track {
	for _, s := range sources {
		track, err := s.player.NowPlaying(ctx)
		if err != nil {
			fmt.Fprintf(warnw, "now playing: %v\n", err)
			continue
		}
		if track.Playing {
			if s.enhancer != nil {
				if enhanced, err := s.enhancer.Enhance(ctx, track); err != nil {
					fmt.Fprintf(warnw, "enhance: %v\n", err)
				} else {
					track = enhanced
				}
			}
			track.Liked = likedNow(ctx, s.liker, track, warnw)
			return track
		}
	}
	return bio.Track{}
}

// likedNow reports whether the track is in the source's liked songs, surfacing
// any lookup error to warnw and treating it as not liked.
func likedNow(ctx context.Context, liked Liker, track bio.Track, warnw io.Writer) bool {
	ok, err := liked.Liked(ctx, track)
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
