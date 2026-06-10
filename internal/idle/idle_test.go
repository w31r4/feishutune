package idle

import (
	"testing"
	"time"
)

// TestParse pins the HIDIdleTime extraction: the nanosecond value is pulled from
// a representative ioreg block and scales correctly into minutes, and missing or
// malformed values error rather than silently reading as zero idle.
func TestParse(t *testing.T) {
	t.Run("extracts idle nanoseconds from an ioreg block", func(t *testing.T) {
		out := `+-o IOHIDSystem  <class IOHIDSystem, registered>
    {
      "HIDIdleTime" = 86333916
      "HIDKindName" = "IOHIDSystem"
    }`
		got, err := parse(out)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if want := 86333916 * time.Nanosecond; got != want {
			t.Fatalf("parse = %v, want %v", got, want)
		}
	})
	t.Run("a large idle reads as minutes", func(t *testing.T) {
		got, err := parse(`"HIDIdleTime" = 660000000000`) // 660s
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if want := 11 * time.Minute; got != want {
			t.Fatalf("parse = %v, want %v", got, want)
		}
	})
	t.Run("missing key errors", func(t *testing.T) {
		if _, err := parse("no HID property here"); err == nil {
			t.Fatal("want an error when HIDIdleTime is absent")
		}
	})
	t.Run("non-numeric value errors", func(t *testing.T) {
		if _, err := parse(`"HIDIdleTime" = notanumber`); err == nil {
			t.Fatal("want an error on a non-numeric idle time")
		}
	})
}
