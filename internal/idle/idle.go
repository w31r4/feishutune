// Package idle reads how long the Mac has gone without keyboard or mouse input,
// so the signature can fall back to "away" when you step away from the machine.
// It reads IOHIDSystem's HIDIdleTime via ioreg and observes this Mac only.
package idle

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// idleKey is the ioreg property holding nanoseconds since the last HID event.
const idleKey = `"HIDIdleTime"`

// Reader reads the system HID idle time.
type Reader struct{}

// New returns an idle-time reader.
func New() *Reader { return &Reader{} }

// Idle returns how long the Mac has gone without keyboard or mouse input, read
// from IOHIDSystem's HIDIdleTime (nanoseconds since the last HID event) via ioreg.
func (r *Reader) Idle(ctx context.Context) (time.Duration, error) {
	out, err := exec.CommandContext(ctx, "ioreg", "-c", "IOHIDSystem").Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return 0, fmt.Errorf("idle: ioreg: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return 0, fmt.Errorf("idle: ioreg: %w", err)
	}
	return parse(string(out))
}

// parse extracts the HIDIdleTime value from ioreg output, which contains a line
// like `"HIDIdleTime" = 86333916`, and returns it as a duration.
func parse(out string) (time.Duration, error) {
	_, after, found := strings.Cut(out, idleKey)
	if !found {
		return 0, fmt.Errorf("idle: %s not found in ioreg output", idleKey)
	}
	line, _, _ := strings.Cut(after, "\n")
	_, value, found := strings.Cut(line, "=")
	if !found {
		return 0, fmt.Errorf("idle: malformed HIDIdleTime line: %q", strings.TrimSpace(line))
	}
	field := strings.TrimSpace(value)
	ns, err := strconv.ParseInt(field, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("idle: parse HIDIdleTime %q: %w", field, err)
	}
	return time.Duration(ns) * time.Nanosecond, nil
}
