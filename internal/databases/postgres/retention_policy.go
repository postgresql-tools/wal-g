// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

package postgres

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Retention check names, stable enough to be matched on by scripts.
const (
	RetentionCheckRPO     = "rpo"
	RetentionCheckWindow  = "retention-window"
	RetentionCheckPolicy  = "policy-outcome"
	RetentionCheckCadence = "backup-cadence"
)

// ParseObjectiveDuration parses a duration, additionally accepting a d suffix
// for days.
//
// Retention windows are stated in days far more often than in hours, and
// time.ParseDuration stops at h - so a policy of 30 days has to be written
// 720h, which is both easy to get wrong and hard to read back. A day here is 24
// hours flat: no calendar, no daylight saving, which is the right unit for
// "how much history must I be able to reach".
func ParseObjectiveDuration(value string) (time.Duration, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, nil
	}

	if days, ok := strings.CutSuffix(trimmed, "d"); ok {
		count, err := strconv.ParseFloat(days, 64)
		if err != nil {
			return 0, fmt.Errorf("could not parse %q as a number of days: %w", value, err)
		}
		return time.Duration(count * float64(24*time.Hour)), nil
	}

	parsed, err := time.ParseDuration(trimmed)
	if err != nil {
		return 0, fmt.Errorf("could not parse %q as a duration: %w", value, err)
	}

	return parsed, nil
}

// RetentionObjectives are the recovery guarantees being claimed, and the policy
// claimed to deliver them.
type RetentionObjectives struct {
	// RPO is the most recent data loss tolerated: how far behind now the latest
	// restorable point is allowed to be.
	RPO time.Duration
	// RetentionWindow is how far back a restore must remain possible.
	RetentionWindow time.Duration
	// RetainCount is the backup count the policy keeps, as passed to
	// "delete retain". Zero means no policy was declared, and the policy-outcome
	// and cadence checks are skipped.
	RetainCount int
	// Format is "text" or "json".
	Format string
}

// RetentionCheck is the result of validating one objective.
type RetentionCheck struct {
	Name    string       `json:"name"`
	Status  DoctorStatus `json:"status"`
	Summary string       `json:"summary"`
	Detail  string       `json:"detail,omitempty"`
	Remedy  string       `json:"remedy,omitempty"`
}

// RetentionReport is the full validation of a retention policy against the
// objectives it is supposed to meet.
type RetentionReport struct {
	Pass   bool             `json:"pass"`
	Checks []RetentionCheck `json:"checks"`

	// Declared restates the objectives, so a stored report says what it was
	// judged against rather than only how it scored.
	Declared struct {
		RPO             string `json:"rpo,omitempty"`
		RetentionWindow string `json:"retention_window,omitempty"`
		RetainCount     int    `json:"retain_count,omitempty"`
	} `json:"declared"`

	Passed  int `json:"passed"`
	Warned  int `json:"warned"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

func (r *RetentionReport) add(check RetentionCheck) {
	r.Checks = append(r.Checks, check)

	switch check.Status {
	case DoctorPass:
		r.Passed++
	case DoctorWarn:
		r.Warned++
	case DoctorFail:
		r.Failed++
	case DoctorSkip:
		r.Skipped++
	}
}

// ValidateRetention judges a retention policy against the objectives it is
// meant to deliver.
//
// The policy is not modeled or approximated here: explanation carries the
// recovery window computed before and after running the real delete through the
// real handler, so what is validated is the delete that would actually happen.
//
// Two questions are asked, and they fail independently. Does storage meet the
// objectives right now? And would it still meet them once the declared policy
// has been applied? A storage that passes the first and fails the second is the
// case worth catching: it looks healthy only because the policy has not caught
// up with it yet.
func ValidateRetention(
	explanation *DeleteExplanation,
	objectives RetentionObjectives,
	now time.Time,
) RetentionReport {
	report := RetentionReport{}
	report.Declared.RetainCount = objectives.RetainCount
	if objectives.RPO > 0 {
		report.Declared.RPO = formatDuration(objectives.RPO)
	}
	if objectives.RetentionWindow > 0 {
		report.Declared.RetentionWindow = formatDuration(objectives.RetentionWindow)
	}

	before := &explanation.RecoveryWindowBefore
	after := &explanation.RecoveryWindowAfter

	report.add(checkRPO(before, objectives, now))
	report.add(checkRetentionWindow(RetentionCheckWindow, "Storage", before, objectives, now))

	if objectives.RetainCount > 0 {
		report.add(checkPolicyOutcome(after, objectives, now))
		report.add(checkCadence(explanation, objectives))
	} else {
		report.add(RetentionCheck{
			Name:    RetentionCheckPolicy,
			Status:  DoctorSkip,
			Summary: "skipped (no retention policy declared)",
			Remedy:  "Pass --retain, or set WALG_RETENTION_COUNT, to validate the policy you actually run.",
		})
		report.add(RetentionCheck{
			Name:    RetentionCheckCadence,
			Status:  DoctorSkip,
			Summary: "skipped (no retention policy declared)",
		})
	}

	report.Pass = report.Failed == 0

	return report
}

// checkRPO measures how much data a failure right now would lose: the distance
// between the newest restorable point and the present moment.
func checkRPO(before *PITRWindowReport, objectives RetentionObjectives, now time.Time) RetentionCheck {
	check := RetentionCheck{Name: RetentionCheckRPO}

	if objectives.RPO <= 0 {
		check.Status = DoctorSkip
		check.Summary = "skipped (no RPO declared)"
		check.Remedy = "Pass --rpo, or set WALG_RPO, to state the data loss you are willing to accept."
		return check
	}

	if before.Empty() {
		check.Status = DoctorFail
		check.Summary = "nothing is restorable, so every committed transaction would be lost"
		check.Remedy = "Take a backup with `wal-g backup-push` and confirm WAL archiving is running."
		return check
	}

	exposure := now.Sub(before.LatestRestorableTime)
	check.Detail = fmt.Sprintf("newest restorable point is %s, %s ago",
		formatPITRTime(before.LatestRestorableTime), formatDuration(exposure))

	if exposure > objectives.RPO {
		check.Status = DoctorFail
		check.Summary = fmt.Sprintf("%s of data at risk, RPO allows %s",
			formatDuration(exposure), formatDuration(objectives.RPO))
		check.Remedy = "WAL archiving is behind or has stopped. Check archive_command and " +
			"`wal-g wal-verify integrity`."
		return check
	}

	check.Status = DoctorPass
	check.Summary = fmt.Sprintf("%s of data at risk, within the %s RPO",
		formatDuration(exposure), formatDuration(objectives.RPO))

	return check
}

// checkRetentionWindow asks whether every moment from the start of the declared
// window through to the newest restorable point can actually be recovered.
//
// A total of recoverable hours is not enough to answer that: the same total can
// be one continuous window or several with holes between them, and only one of
// those is a retention window. So the required interval is checked for full
// coverage, and any part of it that is not covered is named.
//
// The interval ends at the newest restorable point rather than at now, because
// the distance from there to now is what the RPO check is for. Judging it twice
// would report one problem as two.
func checkRetentionWindow(
	name, subject string,
	report *PITRWindowReport,
	objectives RetentionObjectives,
	now time.Time,
) RetentionCheck {
	check := RetentionCheck{Name: name}

	if objectives.RetentionWindow <= 0 {
		check.Status = DoctorSkip
		check.Summary = "skipped (no retention window declared)"
		check.Remedy = "Pass --retention-window, or set WALG_RETENTION_WINDOW, to state how far " +
			"back a restore must be possible."
		return check
	}

	required := now.Add(-objectives.RetentionWindow)

	if report.Empty() {
		check.Status = DoctorFail
		check.Summary = fmt.Sprintf("%s has nothing restorable, %s required",
			subject, formatDuration(objectives.RetentionWindow))
		check.Remedy = "Take a backup with `wal-g backup-push` and confirm WAL archiving is running."
		return check
	}

	if report.EarliestRestorableTime.After(required) {
		short := report.EarliestRestorableTime.Sub(required)
		check.Status = DoctorFail
		check.Summary = fmt.Sprintf("%s reaches back to %s, %s short of the %s required",
			subject, formatPITRTime(report.EarliestRestorableTime), formatDuration(short),
			formatDuration(objectives.RetentionWindow))
		check.Detail = fmt.Sprintf("a restore to any point before %s is not possible",
			formatPITRTime(report.EarliestRestorableTime))
		check.Remedy = "Keep more backups, or keep them longer, so the window covers the retention period."
		return check
	}

	if holes := gapsWithin(report, required, report.LatestRestorableTime); len(holes) > 0 {
		check.Status = DoctorFail
		check.Summary = fmt.Sprintf("%s covers the %s but has %d gap(s) inside it",
			subject, formatDuration(objectives.RetentionWindow), len(holes))
		check.Detail = describeGaps(holes)
		check.Remedy = "The missing WAL cannot be recreated. Take a fresh backup so the window " +
			"becomes continuous again, and investigate the archiving failure that caused the gap."
		return check
	}

	check.Status = DoctorPass
	check.Summary = fmt.Sprintf("%s is continuously restorable back to %s, covering the %s required",
		subject, formatPITRTime(report.EarliestRestorableTime),
		formatDuration(objectives.RetentionWindow))

	return check
}

// checkPolicyOutcome validates the window the declared policy would leave.
func checkPolicyOutcome(
	after *PITRWindowReport,
	objectives RetentionObjectives,
	now time.Time,
) RetentionCheck {
	check := checkRetentionWindow(RetentionCheckPolicy,
		fmt.Sprintf("after `delete retain %d`, storage", objectives.RetainCount),
		after, objectives, now)

	if check.Status == DoctorFail {
		check.Remedy = fmt.Sprintf(
			"Retaining %d backup(s) does not sustain the declared window. Raise the retain count, "+
				"or lower the declared window to what the policy can keep.", objectives.RetainCount)
	}

	return check
}

// checkCadence projects whether the policy can keep meeting the window, rather
// than whether it happens to right now.
//
// Retaining N backups holds a window as wide as the interval between the oldest
// and newest of them. At the observed cadence that is roughly (N-1) intervals.
// Storage can satisfy the window today purely because backups nobody has pruned
// yet are still there; this is the check that says so before the pruning
// happens.
func checkCadence(explanation *DeleteExplanation, objectives RetentionObjectives) RetentionCheck {
	check := RetentionCheck{Name: RetentionCheckCadence}

	if objectives.RetentionWindow <= 0 {
		check.Status = DoctorSkip
		check.Summary = "skipped (no retention window declared)"
		return check
	}

	interval, ok := medianBackupInterval(explanation)
	if !ok {
		check.Status = DoctorSkip
		check.Summary = "skipped (need at least two backups to measure a cadence)"
		return check
	}

	if objectives.RetainCount < 2 {
		check.Status = DoctorWarn
		check.Summary = fmt.Sprintf("retaining %d backup(s) leaves no window at all between backups",
			objectives.RetainCount)
		check.Remedy = "Retain at least two backups so a window exists between the oldest and newest."
		return check
	}

	sustainable := time.Duration(objectives.RetainCount-1) * interval
	check.Detail = fmt.Sprintf("backups arrive about every %s, so retaining %d holds about %s",
		formatDuration(interval), objectives.RetainCount, formatDuration(sustainable))

	if sustainable < objectives.RetentionWindow {
		needed := int(objectives.RetentionWindow/interval) + 1
		check.Status = DoctorFail
		check.Summary = fmt.Sprintf("retaining %d backup(s) sustains only %s of the %s required",
			objectives.RetainCount, formatDuration(sustainable),
			formatDuration(objectives.RetentionWindow))
		check.Remedy = fmt.Sprintf(
			"At this cadence, retain at least %d backup(s), or take backups more often.", needed)
		return check
	}

	check.Status = DoctorPass
	check.Summary = fmt.Sprintf("retaining %d backup(s) at this cadence sustains about %s, covering the %s required",
		objectives.RetainCount, formatDuration(sustainable),
		formatDuration(objectives.RetentionWindow))

	return check
}

// medianBackupInterval measures the cadence from the backups present. The median
// is used rather than the mean so one long outage, or one manual backup taken
// minutes after another, does not redefine the schedule.
func medianBackupInterval(explanation *DeleteExplanation) (time.Duration, bool) {
	times := make([]time.Time, 0,
		len(explanation.BackupsDeleted)+len(explanation.BackupsRetained))

	for _, backup := range explanation.BackupsDeleted {
		if !backup.FinishTime.IsZero() {
			times = append(times, backup.FinishTime)
		}
	}
	for _, backup := range explanation.BackupsRetained {
		if !backup.FinishTime.IsZero() {
			times = append(times, backup.FinishTime)
		}
	}

	if len(times) < 2 {
		return 0, false
	}

	sort.Slice(times, func(i, j int) bool { return times[i].Before(times[j]) })

	intervals := make([]time.Duration, 0, len(times)-1)
	for i := 1; i < len(times); i++ {
		intervals = append(intervals, times[i].Sub(times[i-1]))
	}

	sort.Slice(intervals, func(i, j int) bool { return intervals[i] < intervals[j] })

	median := intervals[len(intervals)/2]
	if median <= 0 {
		return 0, false
	}

	return median, true
}

// gapsWithin returns the parts of a report's gaps that fall inside [from, to].
func gapsWithin(report *PITRWindowReport, from, to time.Time) []PITRGap {
	var inside []PITRGap

	for _, gap := range report.Gaps {
		clipped := gap

		if clipped.Start.Before(from) {
			clipped.Start = from
		}
		if clipped.End.After(to) {
			clipped.End = to
		}
		if !clipped.End.After(clipped.Start) {
			continue
		}

		inside = append(inside, clipped)
	}

	return inside
}

func describeGaps(gaps []PITRGap) string {
	parts := make([]string, 0, len(gaps))

	for _, gap := range gaps {
		parts = append(parts, fmt.Sprintf("%s .. %s (%s)",
			formatPITRTime(gap.Start), formatPITRTime(gap.End), formatDuration(gap.Duration())))
	}

	return "not recoverable: " + joinWithComma(parts)
}

// WriteRetentionReport renders a validation report.
func WriteRetentionReport(report *RetentionReport, format string, output io.Writer) error {
	return WriteReport(format, report, func(w io.Writer) { writeRetentionText(report, w) }, output)
}

func writeRetentionText(report *RetentionReport, output io.Writer) {
	fmt.Fprintf(output, "wal-g retention-validate\n\n")

	fmt.Fprintf(output, "Declared  RPO %s, retention window %s, retain %s\n\n",
		orDash(report.Declared.RPO), orDash(report.Declared.RetentionWindow),
		retainLabel(report.Declared.RetainCount))

	for _, check := range report.Checks {
		fmt.Fprintf(output, "%-6s %-16s %s\n", statusLabel(check.Status), check.Name, check.Summary)
		if check.Detail != "" {
			fmt.Fprintf(output, "       %s\n", check.Detail)
		}
		if check.Remedy != "" {
			fmt.Fprintf(output, "       -> %s\n", check.Remedy)
		}
	}

	fmt.Fprintf(output, "\n%d passed, %d warned, %d failed, %d skipped\n",
		report.Passed, report.Warned, report.Failed, report.Skipped)

	// Nothing declared means nothing was judged. Reporting that as "objectives
	// met" would turn an empty configuration into a clean bill of health.
	if report.Skipped == len(report.Checks) {
		fmt.Fprintf(output, "No objectives declared, so nothing was checked.\n")
		return
	}

	if report.Pass {
		if report.Warned > 0 {
			fmt.Fprintf(output, "Objectives met, with warnings noted above.\n")
		} else {
			fmt.Fprintf(output, "Objectives met.\n")
		}
		return
	}

	fmt.Fprintf(output, "Objectives NOT met: %d check(s) failed.\n", report.Failed)
}

func orDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

func retainLabel(count int) string {
	if count <= 0 {
		return "-"
	}
	return fmt.Sprintf("%d", count)
}
