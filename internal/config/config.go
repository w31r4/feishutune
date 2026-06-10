// Package config resolves the signature policy from, in increasing precedence:
// built-in defaults, the config file (<data dir>/config.json), and environment
// variables. Command-line flags override all of these; the caller binds them
// with a loaded Policy's fields as their defaults, so an unset flag keeps the
// resolved value and a set flag wins.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Durden-T/larktune/internal/bio"
	"github.com/Durden-T/larktune/internal/store"
)

// FileName is the config file within the data directory.
const FileName = "config.json"

// Policy is the string form of bio.Policy: the idle threshold is held as Go
// duration text (e.g. "10m") so it can be bound directly to a command-line flag
// before parsing.
type Policy struct {
	Online    string `json:"online"`
	Offline   string `json:"offline"`
	Weekend   string `json:"weekend"`
	IdleAfter string `json:"idle_after"`
	Blacklist string `json:"blacklist"` // comma-separated substrings that suppress publishing
}

// Defaults returns the built-in policy: 在线 / 离线 / 周末了！, away after 10m idle.
func Defaults() Policy {
	return Policy{
		Online:    "在线",
		Offline:   "离线",
		Weekend:   "周末了！",
		IdleAfter: "10m",
	}
}

// Load returns the defaults overlaid by the config file (if present) and then by
// environment variables (ONLINE, OFFLINE, WEEKEND, IDLE_AFTER, BLACKLIST). A
// field provided at either layer wins even when it is empty — config
// {"online":""} or ONLINE= clears that text — which is distinct from leaving it
// out, which keeps the previous layer's value. A missing config file is not an
// error. The caller layers command-line flags on top of the result.
func Load() (Policy, error) {
	p := Defaults()

	dir, err := store.Dir()
	if err != nil {
		return p, err
	}
	path := filepath.Join(dir, FileName)
	switch b, err := os.ReadFile(path); {
	case err == nil:
		var f filePolicy
		if err := json.Unmarshal(b, &f); err != nil {
			return p, fmt.Errorf("config: parse %s: %w", path, err)
		}
		set(&p.Online, f.Online)
		set(&p.Offline, f.Offline)
		set(&p.Weekend, f.Weekend)
		set(&p.IdleAfter, f.IdleAfter)
		set(&p.Blacklist, f.Blacklist)
	case !os.IsNotExist(err):
		return p, fmt.Errorf("config: read %s: %w", path, err)
	}

	set(&p.Online, envPtr("ONLINE"))
	set(&p.Offline, envPtr("OFFLINE"))
	set(&p.Weekend, envPtr("WEEKEND"))
	set(&p.IdleAfter, envPtr("IDLE_AFTER"))
	set(&p.Blacklist, envPtr("BLACKLIST"))
	return p, nil
}

// filePolicy mirrors Policy with pointers so a key that is present but empty is
// distinguished from an absent key.
type filePolicy struct {
	Online    *string `json:"online"`
	Offline   *string `json:"offline"`
	Weekend   *string `json:"weekend"`
	IdleAfter *string `json:"idle_after"`
	Blacklist *string `json:"blacklist"`
}

// set overwrites dst when src is provided (non-nil), trimming whitespace. A nil
// src means "not provided" and leaves dst unchanged, so an explicit empty value
// clears the field while an omitted one keeps the previous layer's.
func set(dst, src *string) {
	if src != nil {
		*dst = strings.TrimSpace(*src)
	}
}

// envPtr returns the value of key when it is set (even if empty), else nil.
func envPtr(key string) *string {
	if v, ok := os.LookupEnv(key); ok {
		return &v
	}
	return nil
}

// ToBio parses the idle threshold and returns the policy used by package bio.
func (p Policy) ToBio() (bio.Policy, error) {
	idleAfter, err := time.ParseDuration(p.IdleAfter)
	if err != nil {
		return bio.Policy{}, fmt.Errorf("idle-after: %w", err)
	}
	if idleAfter <= 0 {
		return bio.Policy{}, fmt.Errorf("idle-after must be positive, got %q", p.IdleAfter)
	}
	return bio.Policy{
		Online:    p.Online,
		Offline:   p.Offline,
		Weekend:   p.Weekend,
		IdleAfter: idleAfter,
		Blacklist: splitList(p.Blacklist),
	}, nil
}

// splitList splits a comma-separated list, trimming each item and dropping the
// empties, so "a, b ,, c" -> [a b c] and "" -> nil.
func splitList(s string) []string {
	var out []string
	for item := range strings.SplitSeq(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}
