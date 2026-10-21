<!-- SPDX-License-Identifier: MIT -->

# Scheduled available runners for ScaleSets

Use `spec.runnerSchedule` on a `garm-operator.mercedes-benz.com/v1beta1`
ScaleSet to change how many idle runners GARM keeps available during recurring
weekly time ranges. The schedule overrides `spec.minIdleRunners`, not
`spec.maxRunners` or `spec.enabled`.

Add this configuration to your existing ScaleSet spec:

```yaml
spec:
  minIdleRunners: 0
  maxRunners: 20
  runnerSchedule:
    timeZone: America/Chicago
    ranges:
      - days: [Monday, Tuesday, Wednesday, Thursday, Friday]
        start: "08:00"
        end: "18:00"
        minIdleRunners: 5
      - days: [Saturday]
        start: "22:00"
        end: "06:00"
        minIdleRunners: 2
```

This keeps a minimum of five idle runners during weekday business hours and two
from Saturday at 22:00 until Sunday at 06:00. Outside those ranges, the baseline
minimum is zero. Other required ScaleSet fields are omitted from this excerpt.

The current applied idle runner target is exposed under `status.effectiveMinIdleRunners`.

## Matching rules

- Times use local, 24-hour `HH:MM` values from `00:00` to `23:59`. Quote them in YAML.
- Start is inclusive; end is exclusive. `08:00`–`18:00` matches at 08:00 but not at 18:00.
- If end is earlier than start, the range continues into the next day. `days`
  always identifies the starting weekday, including ranges crossing Sunday/Monday.
- If multiple ranges match, the **highest scheduled minimum** wins. The baseline
  is not included in that comparison: a schedule can lower the minimum or set it to zero.
- If no range matches, the ordinary `spec.minIdleRunners` applies. Removing
  `runnerSchedule` restores baseline-only behavior.
- Without `runnerSchedule`, existing ScaleSets behave as before.

## Timezones and daylight saving

`timeZone` accepts IANA timezone names, such as `America/Chicago`, `Europe/Berlin`,
or `UTC`. Omitted timezone defaults to UTC, never the operator host's local timezone.
`Local` is not allowed. The operator includes timezone data for minimal images.

Schedules follow local wall-clock time through daylight-saving changes:

- Spring-forward minutes that do not exist are never matched.
- Repeated fall-back minutes are evaluated in both occurrences.
- An overnight range ends at its configured local time, not after a fixed number
  of elapsed hours.

## Validation and operation

A schedule requires 1–100 ranges. Each range needs 1–7 unique, full weekday names,
different start/end times, and a nonnegative `minIdleRunners` no greater than
`spec.maxRunners`. Equal start/end does not mean all day; use adjacent ranges if
you need all-day coverage. The baseline minimum must also remain within the maximum.

The CRD validates range structure, weekdays, times, and counts. The operator also
validates timezone names; invalid configuration marks the ScaleSet not ready (`InvalidSpec`)
and does not send requests to GARM.

The operator reevaluates scheduled ScaleSets dynamically at transition points (start and end
of scheduled ranges) and on ordinary Kubernetes resource events, avoiding unnecessary periodic
polling. On restart, it applies the schedule for the current time. Paused ScaleSets are not
reconciled.

Only the desired minimum idle count changes. GARM still controls provisioning and
scale-down, and runner startup is asynchronous: scheduling five runners at 08:00
does not guarantee five runners are already booted at 08:00. Schedule changes do
not recreate a ScaleSet, enable a disabled set, or directly delete busy runners.
The stored `spec.minIdleRunners` remains the fallback baseline throughout.

Update both the operator and the ScaleSet CRD before using this field. An older
CRD may prune unknown schedule fields.
