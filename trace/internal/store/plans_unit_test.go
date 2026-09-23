package store

import (
	"testing"
	"time"
)

func TestMonthStartUTC(t *testing.T) {
	cases := []struct {
		name string
		in   time.Time
		want time.Time
	}{
		{
			name: "mid-month normalizes to day-1 00:00 UTC",
			in:   time.Date(2026, 8, 11, 14, 23, 5, 123, time.FixedZone("BRT", -3*3600)),
			want: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "first second of month stays",
			in:   time.Date(2026, 8, 1, 0, 0, 1, 0, time.UTC),
			want: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "last second of month rolls back",
			in:   time.Date(2026, 8, 31, 23, 59, 59, 0, time.UTC),
			want: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name: "december roll-over",
			in:   time.Date(2026, 12, 15, 12, 0, 0, 0, time.UTC),
			want: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := MonthStart(tc.in)
			if !got.Equal(tc.want) {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}

func TestUsageKindConstantsStable(t *testing.T) {
	// Lock the wire-format strings so quota keys in plan_limits mapping
	// stay aligned with what IncrementCounter writes.
	wants := map[string]string{
		"UsageKindEvents":     UsageKindEvents,
		"UsageKindAPICalls":   UsageKindAPICalls,
		"UsageKindReleases":   UsageKindReleases,
		"UsageKindIssues":     UsageKindIssues,
		"UsageKindAlertsFire": UsageKindAlertsFire,
	}
	expected := map[string]string{
		"UsageKindEvents":     "events",
		"UsageKindAPICalls":   "api_calls",
		"UsageKindReleases":   "releases",
		"UsageKindIssues":     "issues",
		"UsageKindAlertsFire": "alerts_fired",
	}
	for k, want := range expected {
		if wants[k] != want {
			t.Errorf("%s drift: got %q want %q", k, wants[k], want)
		}
	}
}
