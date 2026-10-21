// SPDX-License-Identifier: MIT

package scalesets

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	garmoperatorv1beta1 "github.com/mercedes-benz/garm-operator/api/v1beta1"
)

// EffectiveMinIdleRunners evaluates weekly ranges without mutating the baseline.
// Matching uses local wall time, including both occurrences of repeated DST minutes.
func EffectiveMinIdleRunners(spec garmoperatorv1beta1.ScaleSetSpec, now time.Time) (uint, error) {
	schedule := spec.RunnerSchedule
	if schedule == nil {
		return spec.MinIdleRunners, nil
	}
	if err := schedule.Validate(spec.MaxRunners); err != nil {
		return 0, err
	}
	zone := schedule.TimeZone
	if zone == "" {
		zone = "UTC"
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		return 0, err
	}
	local := now.In(location)
	clock := local.Format("15:04")
	today := local.Weekday()
	previous := (today + 6) % 7
	var highest uint
	matched := false
	for _, r := range schedule.Ranges {
		day := today
		if r.Start < r.End {
			if clock < r.Start || clock >= r.End {
				continue
			}
		} else {
			switch {
			case clock >= r.Start:
			case clock < r.End:
				day = previous
			default:
				continue
			}
		}
		for _, candidate := range r.Days {
			if candidate == day.String() {
				if !matched || r.MinIdleRunners > highest {
					highest = r.MinIdleRunners
				}
				matched = true
				break
			}
		}
	}
	if matched {
		return highest, nil
	}
	return spec.MinIdleRunners, nil
}

// NextTransitionTime computes the earliest upcoming transition time (start or end of any range)
// strictly after `now`. If schedule is nil or has no ranges, it returns a zero time.
func NextTransitionTime(schedule *garmoperatorv1beta1.RunnerSchedule, now time.Time) (time.Time, error) {
	if schedule == nil || len(schedule.Ranges) == 0 {
		return time.Time{}, nil
	}
	zone := schedule.TimeZone
	if zone == "" {
		zone = "UTC"
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timeZone %q: %w", zone, err)
	}

	localNow := now.In(location)

	var earliest time.Time

	parseHourMin := func(s string) (int, int) {
		parts := strings.Split(s, ":")
		h, _ := strconv.Atoi(parts[0])
		m, _ := strconv.Atoi(parts[1])
		return h, m
	}

	// Examine candidate transition points across the next 8 days.
	for dayOffset := 0; dayOffset <= 8; dayOffset++ {
		baseDate := localNow.AddDate(0, 0, dayOffset)
		weekday := baseDate.Weekday().String()

		for _, r := range schedule.Ranges {
			for _, d := range r.Days {
				if d != weekday {
					continue
				}

				// Start transition
				sH, sM := parseHourMin(r.Start)
				startTime := time.Date(baseDate.Year(), baseDate.Month(), baseDate.Day(), sH, sM, 0, 0, location)
				if startTime.After(localNow) {
					if earliest.IsZero() || startTime.Before(earliest) {
						earliest = startTime
					}
				}

				// End transition
				eH, eM := parseHourMin(r.End)
				var endDate time.Time
				if r.Start < r.End {
					endDate = baseDate
				} else {
					// Overnight range ends on next calendar day
					endDate = baseDate.AddDate(0, 0, 1)
				}
				endTime := time.Date(endDate.Year(), endDate.Month(), endDate.Day(), eH, eM, 0, 0, location)
				if endTime.After(localNow) {
					if earliest.IsZero() || endTime.Before(earliest) {
						earliest = endTime
					}
				}
			}
		}
	}

	return earliest, nil
}
