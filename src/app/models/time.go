package models

import (
	"database/sql/driver"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Time wraps time.Time to pin the JSON wire format to RFC3339 in UTC with
// second precision.
//
// Go's default time.Time marshaling emits RFC3339Nano. Swift's built-in
// .iso8601 decoding strategy rejects fractional seconds outright, so a
// default-marshaled timestamp fails to decode on iOS while parsing fine in
// JavaScript — the exact class of asymmetry this type exists to remove.
//
// Sub-second precision is discarded. SQLite's CURRENT_TIMESTAMP, the only
// timestamp source in generated schemas, has one-second resolution anyway.
type Time time.Time

func (t Time) MarshalJSON() ([]byte, error) {
	return []byte(strconv.Quote(time.Time(t).UTC().Format(time.RFC3339))), nil
}

func (t *Time) UnmarshalJSON(b []byte) error {
	// Handle literal JSON null.
	if string(b) == "null" {
		*t = Time{}
		return nil
	}
	s, err := strconv.Unquote(string(b))
	if err != nil {
		return fmt.Errorf("models.Time: %w", err)
	}
	if s == "" {
		*t = Time{}
		return nil
	}
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return fmt.Errorf("models.Time: %w", err)
	}
	*t = Time(parsed)
	return nil
}

// scanLayouts covers every shape the SQLite driver can hand back for a
// DATETIME column: a driver-parsed time.Time, or a raw string when the
// column's declared type does not trigger driver-side parsing.
var scanLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// Scan implements sql.Scanner. Every generated model scans created_at
// directly into this type, so it must accept all of the above.
func (t *Time) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*t = Time{}
		return nil
	case time.Time:
		*t = Time(v)
		return nil
	case string:
		return t.parseString(v)
	case []byte:
		return t.parseString(string(v))
	}
	return fmt.Errorf("models.Time: cannot scan %T", src)
}

func (t *Time) parseString(s string) error {
	s = strings.TrimSpace(s)
	for _, layout := range scanLayouts {
		if parsed, err := time.Parse(layout, s); err == nil {
			*t = Time(parsed)
			return nil
		}
	}
	return fmt.Errorf("models.Time: cannot parse %q", s)
}

// Value implements driver.Valuer.
func (t Time) Value() (driver.Value, error) {
	return time.Time(t).UTC(), nil
}

func (t Time) String() string {
	return time.Time(t).UTC().Format(time.RFC3339)
}

// NullTime is Time for a nullable DATETIME column.
//
// It exists because the generated models scan nullable columns through a
// database/sql temporary and then take its address, and sql.NullTime's payload
// is a bare time.Time — which would put a *time.Time on the struct and marshal
// as RFC3339Nano, the exact thing Time exists to prevent. Keeping the payload a
// Time means the null path and the non-null path serialize identically.
type NullTime struct {
	Time  Time
	Valid bool
}

func (n *NullTime) Scan(src any) error {
	if src == nil {
		n.Time, n.Valid = Time{}, false
		return nil
	}
	if err := n.Time.Scan(src); err != nil {
		return err
	}
	n.Valid = true
	return nil
}

func (n NullTime) Value() (driver.Value, error) {
	if !n.Valid {
		return nil, nil
	}
	return n.Time.Value()
}
