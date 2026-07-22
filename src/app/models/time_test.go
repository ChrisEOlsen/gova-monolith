package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestTime_MarshalJSON_DropsFractionalSeconds(t *testing.T) {
	ts := Time(time.Date(2026, 7, 21, 18, 45, 0, 123456789, time.UTC))
	b, err := json.Marshal(ts)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := `"2026-07-21T18:45:00Z"`
	if string(b) != want {
		t.Errorf("got %s, want %s", b, want)
	}
}

func TestTime_MarshalJSON_NormalizesToUTC(t *testing.T) {
	zone := time.FixedZone("CEST", 2*60*60)
	ts := Time(time.Date(2026, 7, 21, 20, 45, 0, 0, zone))
	b, err := json.Marshal(ts)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := `"2026-07-21T18:45:00Z"`
	if string(b) != want {
		t.Errorf("got %s, want %s", b, want)
	}
}

func TestTime_UnmarshalJSON_RoundTrips(t *testing.T) {
	var got Time
	if err := json.Unmarshal([]byte(`"2026-07-21T18:45:00Z"`), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	want := time.Date(2026, 7, 21, 18, 45, 0, 0, time.UTC)
	if !time.Time(got).Equal(want) {
		t.Errorf("got %v, want %v", time.Time(got), want)
	}
}

func TestTime_UnmarshalJSON_Null(t *testing.T) {
	var got Time
	if err := json.Unmarshal([]byte("null"), &got); err != nil {
		t.Fatalf("Unmarshal(null): %v", err)
	}
	if !time.Time(got).IsZero() {
		t.Errorf("got %v, want zero time", time.Time(got))
	}
}

func TestTime_Scan(t *testing.T) {
	cases := []struct {
		name     string
		src      any
		wantTime time.Time
	}{
		{"time.Time", time.Date(2026, 7, 21, 18, 45, 0, 0, time.UTC), time.Date(2026, 7, 21, 18, 45, 0, 0, time.UTC)},
		{"string SQLite CURRENT_TIMESTAMP", "2026-07-21 18:45:00", time.Date(2026, 7, 21, 18, 45, 0, 0, time.UTC)},
		{"string RFC3339", "2026-07-21T18:45:00Z", time.Date(2026, 7, 21, 18, 45, 0, 0, time.UTC)},
		{"bytes RFC3339", []byte("2026-07-21T18:45:00Z"), time.Date(2026, 7, 21, 18, 45, 0, 0, time.UTC)},
		{"string RFC3339Nano", "2026-07-21T18:45:00.000000000Z", time.Date(2026, 7, 21, 18, 45, 0, 0, time.UTC)},
		{"string with timezone offset and nanoseconds", "2026-07-21 18:45:00.000000000+00:00", time.Date(2026, 7, 21, 18, 45, 0, 0, time.UTC)},
		{"string date only", "2026-07-21", time.Date(2026, 7, 21, 0, 0, 0, 0, time.UTC)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got Time
			if err := got.Scan(tc.src); err != nil {
				t.Fatalf("Scan(%v): %v", tc.src, err)
			}
			if !time.Time(got).Equal(tc.wantTime) {
				t.Errorf("got %v, want %v", time.Time(got), tc.wantTime)
			}
		})
	}
}

func TestTime_Scan_Nil(t *testing.T) {
	var got Time
	if err := got.Scan(nil); err != nil {
		t.Fatalf("Scan(nil): %v", err)
	}
	if !time.Time(got).IsZero() {
		t.Errorf("got %v, want zero time", time.Time(got))
	}
}

func TestTime_Scan_Unsupported(t *testing.T) {
	var got Time
	if err := got.Scan(42); err == nil {
		t.Error("Scan(int): expected error, got nil")
	}
}

func TestTime_Value(t *testing.T) {
	cases := []struct {
		name     string
		ts       Time
		wantTime time.Time
	}{
		{
			"UTC",
			Time(time.Date(2026, 7, 21, 18, 45, 0, 0, time.UTC)),
			time.Date(2026, 7, 21, 18, 45, 0, 0, time.UTC),
		},
		{
			"non-UTC fixed zone",
			Time(time.Date(2026, 7, 21, 20, 45, 0, 0, time.FixedZone("CEST", 2*60*60))),
			time.Date(2026, 7, 21, 18, 45, 0, 0, time.UTC),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, err := tc.ts.Value()
			if err != nil {
				t.Fatalf("Value: %v", err)
			}
			got, ok := v.(time.Time)
			if !ok {
				t.Fatalf("Value: got %T, want time.Time", v)
			}
			if !got.Equal(tc.wantTime) {
				t.Errorf("got %v, want %v", got, tc.wantTime)
			}
		})
	}
}
