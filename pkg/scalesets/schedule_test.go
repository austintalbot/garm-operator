// SPDX-License-Identifier: MIT

package scalesets

import (
	"reflect"
	"testing"
	"time"

	garmoperatorv1beta1 "github.com/mercedes-benz/garm-operator/api/v1beta1"
)

func TestEffectiveMinIdleRunners(t *testing.T) {
	rangeFor := func(days []string, start, end string, count uint) garmoperatorv1beta1.RunnerScheduleRange {
		return garmoperatorv1beta1.RunnerScheduleRange{Days: days, Start: start, End: end, MinIdleRunners: count}
	}
	daytime := rangeFor([]string{"Monday"}, "08:00", "18:00", 5)
	overnight := rangeFor([]string{"Monday"}, "22:00", "06:00", 5)
	sunday := rangeFor([]string{"Sunday"}, "22:00", "06:00", 5)
	for _, tc := range []struct {
		name, instant, zone string
		baseline, want      uint
		ranges              []garmoperatorv1beta1.RunnerScheduleRange
	}{
		{"no schedule", "2026-10-19T12:00:00Z", "", 3, 3, nil},
		{"before", "2026-10-19T07:59:59Z", "", 0, 0, []garmoperatorv1beta1.RunnerScheduleRange{daytime}},
		{"start", "2026-10-19T08:00:00Z", "", 0, 5, []garmoperatorv1beta1.RunnerScheduleRange{daytime}},
		{"during", "2026-10-19T17:59:59Z", "UTC", 0, 5, []garmoperatorv1beta1.RunnerScheduleRange{daytime}},
		{"end", "2026-10-19T18:00:00Z", "UTC", 0, 0, []garmoperatorv1beta1.RunnerScheduleRange{daytime}},
		{"wrong weekday", "2026-10-20T12:00:00Z", "UTC", 1, 1, []garmoperatorv1beta1.RunnerScheduleRange{daytime}},
		{"overlap", "2026-10-19T12:00:00Z", "UTC", 8, 5, []garmoperatorv1beta1.RunnerScheduleRange{daytime, rangeFor([]string{"Monday"}, "10:00", "14:00", 3)}},
		{"reverse overlap", "2026-10-19T12:00:00Z", "UTC", 8, 5, []garmoperatorv1beta1.RunnerScheduleRange{rangeFor([]string{"Monday"}, "10:00", "14:00", 3), daytime}},
		{"zero override", "2026-10-19T12:00:00Z", "UTC", 8, 0, []garmoperatorv1beta1.RunnerScheduleRange{rangeFor([]string{"Monday"}, "08:00", "18:00", 0)}},
		{"overnight start", "2026-10-19T22:00:00Z", "UTC", 0, 5, []garmoperatorv1beta1.RunnerScheduleRange{overnight}},
		{"overnight next day", "2026-10-20T05:59:59Z", "UTC", 0, 5, []garmoperatorv1beta1.RunnerScheduleRange{overnight}},
		{"overnight wrong morning", "2026-10-19T05:59:00Z", "UTC", 0, 0, []garmoperatorv1beta1.RunnerScheduleRange{overnight}},
		{"overnight end", "2026-10-20T06:00:00Z", "UTC", 0, 0, []garmoperatorv1beta1.RunnerScheduleRange{overnight}},
		{"week wrap", "2026-10-19T05:59:00Z", "UTC", 0, 5, []garmoperatorv1beta1.RunnerScheduleRange{sunday}},
		{"summer zone", "2026-07-06T13:00:00Z", "America/Chicago", 0, 5, []garmoperatorv1beta1.RunnerScheduleRange{daytime}},
		{"winter zone", "2026-01-05T14:00:00Z", "America/Chicago", 0, 5, []garmoperatorv1beta1.RunnerScheduleRange{daytime}},
		{"spring before gap", "2026-03-08T07:59:00Z", "America/Chicago", 0, 5, []garmoperatorv1beta1.RunnerScheduleRange{rangeFor([]string{"Sunday"}, "01:30", "03:30", 5)}},
		{"spring after gap", "2026-03-08T08:00:00Z", "America/Chicago", 0, 5, []garmoperatorv1beta1.RunnerScheduleRange{rangeFor([]string{"Sunday"}, "01:30", "03:30", 5)}},
		{"spring end", "2026-03-08T08:30:00Z", "America/Chicago", 0, 0, []garmoperatorv1beta1.RunnerScheduleRange{rangeFor([]string{"Sunday"}, "01:30", "03:30", 5)}},
		{"fall first hour", "2026-11-01T06:30:00Z", "America/Chicago", 0, 5, []garmoperatorv1beta1.RunnerScheduleRange{rangeFor([]string{"Sunday"}, "01:00", "02:00", 5)}},
		{"fall repeated hour", "2026-11-01T07:30:00Z", "America/Chicago", 0, 5, []garmoperatorv1beta1.RunnerScheduleRange{rangeFor([]string{"Sunday"}, "01:00", "02:00", 5)}},
		{"spring overnight", "2026-03-08T08:00:00Z", "America/Chicago", 0, 5, []garmoperatorv1beta1.RunnerScheduleRange{rangeFor([]string{"Saturday"}, "22:00", "06:00", 5)}},
		{"spring overnight end", "2026-03-08T11:00:00Z", "America/Chicago", 0, 0, []garmoperatorv1beta1.RunnerScheduleRange{rangeFor([]string{"Saturday"}, "22:00", "06:00", 5)}},
		{"fall overnight", "2026-11-01T07:30:00Z", "America/Chicago", 0, 5, []garmoperatorv1beta1.RunnerScheduleRange{rangeFor([]string{"Saturday"}, "22:00", "06:00", 5)}},
		{"fall overnight end", "2026-11-01T12:00:00Z", "America/Chicago", 0, 0, []garmoperatorv1beta1.RunnerScheduleRange{rangeFor([]string{"Saturday"}, "22:00", "06:00", 5)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := validSpec()
			spec.MinIdleRunners = tc.baseline
			if tc.ranges != nil {
				spec.RunnerSchedule = &garmoperatorv1beta1.RunnerSchedule{TimeZone: tc.zone, Ranges: tc.ranges}
			}
			original := spec.DeepCopy()
			now, err := time.Parse(time.RFC3339, tc.instant)
			if err != nil {
				t.Fatal(err)
			}
			got, err := EffectiveMinIdleRunners(spec, now)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
			if !reflect.DeepEqual(&spec, original) {
				t.Fatal("evaluator mutated spec")
			}
		})
	}
}

func TestEffectiveMinIdleRunnersInvalidSchedule(t *testing.T) {
	spec := validSpec()
	spec.RunnerSchedule = &garmoperatorv1beta1.RunnerSchedule{}
	if _, err := EffectiveMinIdleRunners(spec, time.Now()); err == nil {
		t.Fatal("expected invalid schedule error")
	}
}

func TestNextTransitionTime(t *testing.T) {
	t.Run("nil schedule returns zero", func(t *testing.T) {
		got, err := NextTransitionTime(nil, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if !got.IsZero() {
			t.Fatalf("expected zero time, got %v", got)
		}
	})

	t.Run("empty ranges returns zero", func(t *testing.T) {
		got, err := NextTransitionTime(&garmoperatorv1beta1.RunnerSchedule{}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if !got.IsZero() {
			t.Fatalf("expected zero time, got %v", got)
		}
	})

	t.Run("upcoming start transition on same day", func(t *testing.T) {
		// Monday at 06:30 UTC
		now, _ := time.Parse(time.RFC3339, "2026-10-19T06:30:00Z")
		schedule := &garmoperatorv1beta1.RunnerSchedule{
			TimeZone: "UTC",
			Ranges: []garmoperatorv1beta1.RunnerScheduleRange{
				{Days: []string{"Monday"}, Start: "08:00", End: "18:00", MinIdleRunners: 5},
			},
		}
		got, err := NextTransitionTime(schedule, now)
		if err != nil {
			t.Fatal(err)
		}
		want, _ := time.Parse(time.RFC3339, "2026-10-19T08:00:00Z")
		if !got.Equal(want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})

	t.Run("upcoming end transition on same day", func(t *testing.T) {
		// Monday at 12:00 UTC
		now, _ := time.Parse(time.RFC3339, "2026-10-19T12:00:00Z")
		schedule := &garmoperatorv1beta1.RunnerSchedule{
			TimeZone: "UTC",
			Ranges: []garmoperatorv1beta1.RunnerScheduleRange{
				{Days: []string{"Monday"}, Start: "08:00", End: "18:00", MinIdleRunners: 5},
			},
		}
		got, err := NextTransitionTime(schedule, now)
		if err != nil {
			t.Fatal(err)
		}
		want, _ := time.Parse(time.RFC3339, "2026-10-19T18:00:00Z")
		if !got.Equal(want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})

	t.Run("upcoming overnight end transition on next day", func(t *testing.T) {
		// Monday at 23:00 UTC
		now, _ := time.Parse(time.RFC3339, "2026-10-19T23:00:00Z")
		schedule := &garmoperatorv1beta1.RunnerSchedule{
			TimeZone: "UTC",
			Ranges: []garmoperatorv1beta1.RunnerScheduleRange{
				{Days: []string{"Monday"}, Start: "22:00", End: "06:00", MinIdleRunners: 5},
			},
		}
		got, err := NextTransitionTime(schedule, now)
		if err != nil {
			t.Fatal(err)
		}
		// Tuesday at 06:00 UTC
		want, _ := time.Parse(time.RFC3339, "2026-10-20T06:00:00Z")
		if !got.Equal(want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})

	t.Run("upcoming start transition multiple days ahead", func(t *testing.T) {
		// Tuesday at 12:00 UTC (2026-10-20)
		now, _ := time.Parse(time.RFC3339, "2026-10-20T12:00:00Z")
		schedule := &garmoperatorv1beta1.RunnerSchedule{
			TimeZone: "UTC",
			Ranges: []garmoperatorv1beta1.RunnerScheduleRange{
				{Days: []string{"Friday"}, Start: "09:00", End: "17:00", MinIdleRunners: 5},
			},
		}
		got, err := NextTransitionTime(schedule, now)
		if err != nil {
			t.Fatal(err)
		}
		// Friday at 09:00 UTC (2026-10-23)
		want, _ := time.Parse(time.RFC3339, "2026-10-23T09:00:00Z")
		if !got.Equal(want) {
			t.Fatalf("expected %v, got %v", want, got)
		}
	})
}
