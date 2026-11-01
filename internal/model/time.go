package model

import (
	"fmt"
	"time"
)

// timeLayout is the stored form: UTC, always three fractional digits (§7.2).
const timeLayout = "2006-01-02T15:04:05.000Z"

// Time is a timestamp that marshals as RFC 3339 UTC with milliseconds (§7.2).
type Time struct{ time.Time }

// NewTime returns t in UTC, truncated to the stored millisecond (§7.2).
func NewTime(t time.Time) Time {
	return Time{t.UTC().Truncate(time.Millisecond)}
}

// ParseTime parses any RFC 3339 timestamp and normalizes it per NewTime (§7.2).
func ParseTime(s string) (Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return Time{}, fmt.Errorf("parse time: %w", err)
	}
	return NewTime(t), nil
}

// String returns the stored form of t (§7.2).
func (t Time) String() string {
	return t.UTC().Format(timeLayout)
}

// MarshalText implements encoding.TextMarshaler with the stored form (§7.2).
func (t Time) MarshalText() ([]byte, error) {
	if y := t.UTC().Year(); y < 0 || y > 9999 {
		return nil, fmt.Errorf("marshal time: year %d out of range", y)
	}
	return []byte(t.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler per ParseTime (§7.2).
func (t *Time) UnmarshalText(b []byte) error {
	parsed, err := ParseTime(string(b))
	if err != nil {
		return err
	}
	*t = parsed
	return nil
}

// MarshalJSON writes the stored form as a JSON string (§7.2).
func (t Time) MarshalJSON() ([]byte, error) {
	b, err := t.MarshalText()
	if err != nil {
		return nil, err
	}
	return []byte(`"` + string(b) + `"`), nil
}

// UnmarshalJSON reads a JSON string per ParseTime; null leaves t unchanged (§7.2).
func (t *Time) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}
	if len(b) < 2 || b[0] != '"' || b[len(b)-1] != '"' {
		return fmt.Errorf("parse time: not a JSON string: %s", b)
	}
	return t.UnmarshalText(b[1 : len(b)-1])
}
