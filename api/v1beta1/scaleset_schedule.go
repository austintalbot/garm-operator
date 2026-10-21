// SPDX-License-Identifier: MIT

package v1beta1

import (
	"fmt"
	"time"
	_ "time/tzdata" // Keep IANA zones available in minimal operator images.
)

const defaultRunnerScheduleTimeZone = "UTC"

// RunnerSchedule defines weekly overrides in one wall-clock timezone.
type RunnerSchedule struct {
	// TimeZone is an IANA timezone. When omitted, UTC is used.
	// +kubebuilder:default=UTC
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:XValidation:rule="self != 'Local'",message="timeZone must not be Local"
	// +optional
	TimeZone string `json:"timeZone,omitempty"`
	// Ranges override the baseline; the highest matching count wins.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=100
	Ranges []RunnerScheduleRange `json:"ranges"`
}

// RunnerScheduleRange is a start-inclusive, end-exclusive local time range.
// For overnight ranges, Days identifies the starting weekday.
// +kubebuilder:validation:XValidation:rule="self.start != self.end",message="start and end must differ"
type RunnerScheduleRange struct {
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:MaxItems=7
	// +listType=set
	// +kubebuilder:validation:items:Enum=Monday;Tuesday;Wednesday;Thursday;Friday;Saturday;Sunday
	Days []string `json:"days"`
	// +kubebuilder:validation:Pattern=`^([01][0-9]|2[0-3]):[0-5][0-9]$`
	// +kubebuilder:validation:MaxLength=5
	Start string `json:"start"`
	// +kubebuilder:validation:Pattern=`^([01][0-9]|2[0-3]):[0-5][0-9]$`
	// +kubebuilder:validation:MaxLength=5
	End string `json:"end"`
	// +kubebuilder:validation:Minimum=0
	MinIdleRunners uint `json:"minIdleRunners"`
}

// Validate rejects malformed ranges before reconciliation sends any API calls.
func (s RunnerSchedule) Validate(maxRunners uint) error {
	zone := s.TimeZone
	if zone == "" {
		zone = defaultRunnerScheduleTimeZone
	}
	if zone == "Local" {
		return fmt.Errorf("timeZone must not be Local")
	}
	if _, err := time.LoadLocation(zone); err != nil {
		return fmt.Errorf("invalid timeZone %q: %w", zone, err)
	}
	if len(s.Ranges) == 0 || len(s.Ranges) > 100 {
		return fmt.Errorf("ranges must contain between 1 and 100 entries")
	}
	for i, r := range s.Ranges {
		if len(r.Days) == 0 || len(r.Days) > 7 {
			return fmt.Errorf("ranges[%d].days must contain between 1 and 7 weekdays", i)
		}
		seen := make(map[string]bool, len(r.Days))
		for _, day := range r.Days {
			switch day {
			case "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday", "Sunday":
			default:
				return fmt.Errorf("ranges[%d]: invalid weekday %q", i, day)
			}
			if seen[day] {
				return fmt.Errorf("ranges[%d]: duplicate weekday %q", i, day)
			}
			seen[day] = true
		}
		for _, value := range []string{r.Start, r.End} {
			parsed, err := time.Parse("15:04", value)
			if err != nil || parsed.Format("15:04") != value {
				return fmt.Errorf("ranges[%d]: time %q must use 24-hour HH:MM", i, value)
			}
		}
		if r.Start == r.End {
			return fmt.Errorf("ranges[%d]: start and end must differ", i)
		}
		if r.MinIdleRunners > maxRunners {
			return fmt.Errorf("ranges[%d]: minIdleRunners must be less than or equal to maxRunners", i)
		}
	}
	return nil
}
