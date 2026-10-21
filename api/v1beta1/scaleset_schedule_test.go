// SPDX-License-Identifier: MIT

package v1beta1

import (
	"encoding/json"
	"testing"
)

func testRunnerSchedule() *RunnerSchedule {
	return &RunnerSchedule{TimeZone: "America/Chicago", Ranges: []RunnerScheduleRange{{
		Days: []string{"Monday"}, Start: "08:00", End: "18:00", MinIdleRunners: 5,
	}}}
}

// Rejecting malformed schedules prevents invalid configuration reaching GARM.
func TestRunnerScheduleValidation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(*RunnerSchedule)
		invalid bool
	}{
		{"valid", func(s *RunnerSchedule) {}, false},
		{"default zone", func(s *RunnerSchedule) { s.TimeZone = "" }, false},
		{"UTC", func(s *RunnerSchedule) { s.TimeZone = "UTC" }, false},
		{"overnight", func(s *RunnerSchedule) { s.Ranges[0].Start = "22:00"; s.Ranges[0].End = "06:00" }, false},
		{"zero", func(s *RunnerSchedule) { s.Ranges[0].MinIdleRunners = 0 }, false},
		{"at max", func(s *RunnerSchedule) { s.Ranges[0].MinIdleRunners = 10 }, false},
		{"above max", func(s *RunnerSchedule) { s.Ranges[0].MinIdleRunners = 11 }, true},
		{"bad zone", func(s *RunnerSchedule) { s.TimeZone = "Mars/Olympus" }, true},
		{"local zone", func(s *RunnerSchedule) { s.TimeZone = "Local" }, true},
		{"no ranges", func(s *RunnerSchedule) { s.Ranges = nil }, true},
		{"no days", func(s *RunnerSchedule) { s.Ranges[0].Days = nil }, true},
		{"bad day", func(s *RunnerSchedule) { s.Ranges[0].Days = []string{"Mon"} }, true},
		{"duplicate day", func(s *RunnerSchedule) { s.Ranges[0].Days = []string{"Monday", "Monday"} }, true},
		{"short time", func(s *RunnerSchedule) { s.Ranges[0].Start = "8:00" }, true},
		{"bad hour", func(s *RunnerSchedule) { s.Ranges[0].Start = "24:00" }, true},
		{"bad minute", func(s *RunnerSchedule) { s.Ranges[0].End = "12:60" }, true},
		{"equal times", func(s *RunnerSchedule) { s.Ranges[0].End = "08:00" }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			spec := validScaleSetSpec()
			spec.RunnerSchedule = testRunnerSchedule()
			tc.change(spec.RunnerSchedule)
			if err := spec.Validate(); (err != nil) != tc.invalid {
				t.Fatalf("Validate() = %v, invalid = %v", err, tc.invalid)
			}
		})
	}
}

func TestRunnerScheduleDeepCopy(t *testing.T) {
	set := &ScaleSet{Spec: validScaleSetSpec()}
	set.Spec.RunnerSchedule = testRunnerSchedule()
	copied := set.DeepCopy()
	copied.Spec.RunnerSchedule.TimeZone = "UTC"
	copied.Spec.RunnerSchedule.Ranges[0].Start = "09:00"
	copied.Spec.RunnerSchedule.Ranges[0].Days[0] = "Tuesday"
	if s := set.Spec.RunnerSchedule; s.TimeZone != "America/Chicago" || s.Ranges[0].Start != "08:00" || s.Ranges[0].Days[0] != "Monday" {
		t.Fatalf("copy mutated original: %+v", s)
	}
}

func TestRunnerScheduleJSON(t *testing.T) {
	for _, scheduled := range []bool{false, true} {
		spec := validScaleSetSpec()
		if scheduled {
			spec.RunnerSchedule = testRunnerSchedule()
			spec.RunnerSchedule.Ranges[0].MinIdleRunners = 0
		}
		data, err := json.Marshal(spec)
		if err != nil {
			t.Fatal(err)
		}
		var got ScaleSetSpec
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		if (got.RunnerSchedule != nil) != scheduled {
			t.Fatalf("schedule presence lost: %s", data)
		}
		if scheduled && got.RunnerSchedule.Ranges[0].MinIdleRunners != 0 {
			t.Fatal("zero count lost")
		}
	}
}
