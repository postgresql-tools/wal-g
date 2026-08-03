// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

package postgres

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

var retentionNow = time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)

// windowFrom builds a report describing one continuous restorable window.
func windowFrom(start, end time.Time) PITRWindowReport {
	return PITRWindowReport{
		Windows: []PITRWindow{{
			Timeline: 1,
			Start:    start,
			End:      end,
			Backups:  []string{"base_a"},
		}},
		EarliestRestorableTime: start,
		LatestRestorableTime:   end,
		TotalBackups:           1,
		RestorableBackups:      1,
	}
}

// explanationOver builds the delete explanation the validator reads, with a
// backup every interval across the window so a cadence can be measured.
func explanationOver(before, after PITRWindowReport, backupTimes ...time.Time) DeleteExplanation {
	explanation := DeleteExplanation{
		RecoveryWindowBefore: before,
		RecoveryWindowAfter:  after,
	}

	for _, at := range backupTimes {
		explanation.BackupsRetained = append(explanation.BackupsRetained,
			ExplainedBackup{Name: "base_" + at.Format("0102T1504"), FinishTime: at, Restorable: true})
	}

	return explanation
}

func dailyBackups(count int, endingAt time.Time) []time.Time {
	times := make([]time.Time, 0, count)
	for i := count - 1; i >= 0; i-- {
		times = append(times, endingAt.Add(-time.Duration(i)*24*time.Hour))
	}
	return times
}

func findRetentionCheck(t *testing.T, report RetentionReport, name string) RetentionCheck {
	t.Helper()

	for _, check := range report.Checks {
		if check.Name == name {
			return check
		}
	}
	t.Fatalf("report has no %q check", name)
	return RetentionCheck{}
}

func TestParseObjectiveDuration(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want time.Duration
		bad  bool
	}{
		{in: "30d", want: 30 * 24 * time.Hour},
		{in: "1d", want: 24 * time.Hour},
		{in: "0.5d", want: 12 * time.Hour},
		{in: "72h", want: 72 * time.Hour},
		{in: "90m", want: 90 * time.Minute},
		{in: " 7d ", want: 7 * 24 * time.Hour},
		{in: "", want: 0},
		{in: "fortnight", bad: true},
		{in: "xd", bad: true},
	} {
		got, err := ParseObjectiveDuration(tc.in)

		if tc.bad {
			if err == nil {
				t.Errorf("%q: expected an error, got %s", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: unexpected error: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%q: expected %s, got %s", tc.in, tc.want, got)
		}
	}
}

func TestValidateRetention_HealthyPolicyPasses(t *testing.T) {
	before := windowFrom(retentionNow.Add(-40*24*time.Hour), retentionNow.Add(-5*time.Minute))
	after := windowFrom(retentionNow.Add(-35*24*time.Hour), retentionNow.Add(-5*time.Minute))

	explanation := explanationOver(before, after, dailyBackups(36, retentionNow)...)

	report := ValidateRetention(&explanation, RetentionObjectives{
		RPO:             time.Hour,
		RetentionWindow: 30 * 24 * time.Hour,
		RetainCount:     36,
	}, retentionNow)

	if !report.Pass {
		t.Fatalf("expected the policy to pass, got %d failure(s): %+v", report.Failed, report.Checks)
	}
	if report.Failed != 0 {
		t.Errorf("expected no failures, got %d", report.Failed)
	}
}

// WAL archiving having stalled shows up as data at risk, not as a short window.
func TestValidateRetention_StalledArchivingFailsRPO(t *testing.T) {
	before := windowFrom(retentionNow.Add(-40*24*time.Hour), retentionNow.Add(-6*time.Hour))

	explanation := explanationOver(before, before, dailyBackups(36, retentionNow)...)

	report := ValidateRetention(&explanation, RetentionObjectives{
		RPO:             time.Hour,
		RetentionWindow: 30 * 24 * time.Hour,
		RetainCount:     36,
	}, retentionNow)

	check := findRetentionCheck(t, report, RetentionCheckRPO)
	if check.Status != DoctorFail {
		t.Errorf("expected the RPO check to fail, got %s: %s", check.Status, check.Summary)
	}
	if check.Remedy == "" {
		t.Error("a failing RPO check should say what to do about it")
	}

	// The window itself is fine, so that check must not also fail.
	if window := findRetentionCheck(t, report, RetentionCheckWindow); window.Status != DoctorPass {
		t.Errorf("expected the window check to pass, got %s: %s", window.Status, window.Summary)
	}
}

func TestValidateRetention_ShortWindowFails(t *testing.T) {
	before := windowFrom(retentionNow.Add(-10*24*time.Hour), retentionNow.Add(-time.Minute))

	explanation := explanationOver(before, before, dailyBackups(10, retentionNow)...)

	report := ValidateRetention(&explanation, RetentionObjectives{
		RPO:             time.Hour,
		RetentionWindow: 30 * 24 * time.Hour,
		RetainCount:     10,
	}, retentionNow)

	check := findRetentionCheck(t, report, RetentionCheckWindow)
	if check.Status != DoctorFail {
		t.Fatalf("expected the window check to fail, got %s: %s", check.Status, check.Summary)
	}
	if !strings.Contains(check.Summary, "short of") {
		t.Errorf("expected the shortfall to be quantified, got %q", check.Summary)
	}
}

// The case the whole command exists for: storage satisfies the objectives only
// because the policy has not been applied to it yet.
func TestValidateRetention_PolicyBreaksAWindowThatCurrentlyPasses(t *testing.T) {
	// Forty days are on disk, but retaining 5 daily backups keeps only five.
	before := windowFrom(retentionNow.Add(-40*24*time.Hour), retentionNow.Add(-time.Minute))
	after := windowFrom(retentionNow.Add(-5*24*time.Hour), retentionNow.Add(-time.Minute))

	explanation := explanationOver(before, after, dailyBackups(40, retentionNow)...)

	report := ValidateRetention(&explanation, RetentionObjectives{
		RPO:             time.Hour,
		RetentionWindow: 30 * 24 * time.Hour,
		RetainCount:     5,
	}, retentionNow)

	if current := findRetentionCheck(t, report, RetentionCheckWindow); current.Status != DoctorPass {
		t.Errorf("storage should currently pass, got %s: %s", current.Status, current.Summary)
	}

	outcome := findRetentionCheck(t, report, RetentionCheckPolicy)
	if outcome.Status != DoctorFail {
		t.Fatalf("expected the policy outcome to fail, got %s: %s", outcome.Status, outcome.Summary)
	}
	if report.Pass {
		t.Error("a report with a failing check must not pass")
	}
}

// Cadence predicts the same failure from the policy alone, before storage has
// had a chance to show it.
func TestValidateRetention_CadenceCatchesAnUnsustainablePolicy(t *testing.T) {
	before := windowFrom(retentionNow.Add(-40*24*time.Hour), retentionNow.Add(-time.Minute))
	after := windowFrom(retentionNow.Add(-40*24*time.Hour), retentionNow.Add(-time.Minute))

	explanation := explanationOver(before, after, dailyBackups(40, retentionNow)...)

	report := ValidateRetention(&explanation, RetentionObjectives{
		RPO:             time.Hour,
		RetentionWindow: 30 * 24 * time.Hour,
		RetainCount:     5,
	}, retentionNow)

	check := findRetentionCheck(t, report, RetentionCheckCadence)
	if check.Status != DoctorFail {
		t.Fatalf("expected the cadence check to fail, got %s: %s", check.Status, check.Summary)
	}
	// The remedy has to be actionable: how many backups would actually do.
	if !strings.Contains(check.Remedy, "retain at least") {
		t.Errorf("expected the remedy to name a workable retain count, got %q", check.Remedy)
	}
}

func TestValidateRetention_CadenceAcceptsASufficientPolicy(t *testing.T) {
	before := windowFrom(retentionNow.Add(-40*24*time.Hour), retentionNow.Add(-time.Minute))

	explanation := explanationOver(before, before, dailyBackups(40, retentionNow)...)

	report := ValidateRetention(&explanation, RetentionObjectives{
		RetentionWindow: 7 * 24 * time.Hour,
		RetainCount:     10,
	}, retentionNow)

	if check := findRetentionCheck(t, report, RetentionCheckCadence); check.Status != DoctorPass {
		t.Errorf("expected the cadence check to pass, got %s: %s", check.Status, check.Summary)
	}
}

// A gap inside the required period means the period is not recoverable, however
// many hours in total are.
func TestValidateRetention_GapInsideTheWindowFails(t *testing.T) {
	before := PITRWindowReport{
		Windows: []PITRWindow{
			{Timeline: 1, Start: retentionNow.Add(-40 * 24 * time.Hour), End: retentionNow.Add(-20 * 24 * time.Hour)},
			{Timeline: 1, Start: retentionNow.Add(-15 * 24 * time.Hour), End: retentionNow.Add(-time.Minute)},
		},
		Gaps: []PITRGap{
			{Timeline: 1, Start: retentionNow.Add(-20 * 24 * time.Hour), End: retentionNow.Add(-15 * 24 * time.Hour)},
		},
		EarliestRestorableTime: retentionNow.Add(-40 * 24 * time.Hour),
		LatestRestorableTime:   retentionNow.Add(-time.Minute),
	}

	explanation := explanationOver(before, before, dailyBackups(40, retentionNow)...)

	report := ValidateRetention(&explanation, RetentionObjectives{
		RetentionWindow: 30 * 24 * time.Hour,
	}, retentionNow)

	check := findRetentionCheck(t, report, RetentionCheckWindow)
	if check.Status != DoctorFail {
		t.Fatalf("expected a gap inside the window to fail, got %s: %s", check.Status, check.Summary)
	}
	if !strings.Contains(check.Detail, "not recoverable") {
		t.Errorf("expected the gap to be named, got %q", check.Detail)
	}
}

// A gap older than the required period is not this policy's problem.
func TestValidateRetention_GapOutsideTheWindowIsIgnored(t *testing.T) {
	before := PITRWindowReport{
		Windows: []PITRWindow{
			{Timeline: 1, Start: retentionNow.Add(-90 * 24 * time.Hour), End: retentionNow.Add(-60 * 24 * time.Hour)},
			{Timeline: 1, Start: retentionNow.Add(-40 * 24 * time.Hour), End: retentionNow.Add(-time.Minute)},
		},
		Gaps: []PITRGap{
			{Timeline: 1, Start: retentionNow.Add(-60 * 24 * time.Hour), End: retentionNow.Add(-40 * 24 * time.Hour)},
		},
		EarliestRestorableTime: retentionNow.Add(-90 * 24 * time.Hour),
		LatestRestorableTime:   retentionNow.Add(-time.Minute),
	}

	explanation := explanationOver(before, before, dailyBackups(40, retentionNow)...)

	report := ValidateRetention(&explanation, RetentionObjectives{
		RetentionWindow: 30 * 24 * time.Hour,
	}, retentionNow)

	if check := findRetentionCheck(t, report, RetentionCheckWindow); check.Status != DoctorPass {
		t.Errorf("a gap older than the window should not fail it, got %s: %s", check.Status, check.Summary)
	}
}

func TestValidateRetention_NothingRestorableFailsLoudly(t *testing.T) {
	empty := PITRWindowReport{}
	explanation := explanationOver(empty, empty)

	report := ValidateRetention(&explanation, RetentionObjectives{
		RPO:             time.Hour,
		RetentionWindow: 30 * 24 * time.Hour,
	}, retentionNow)

	if report.Pass {
		t.Fatal("an empty storage must not pass")
	}
	if check := findRetentionCheck(t, report, RetentionCheckRPO); check.Status != DoctorFail {
		t.Errorf("expected the RPO check to fail, got %s", check.Status)
	}
	if check := findRetentionCheck(t, report, RetentionCheckWindow); check.Status != DoctorFail {
		t.Errorf("expected the window check to fail, got %s", check.Status)
	}
}

// Undeclared objectives are skipped, not silently passed: a report that scores
// four out of four while being asked nothing would be worse than useless.
func TestValidateRetention_UndeclaredObjectivesAreSkipped(t *testing.T) {
	before := windowFrom(retentionNow.Add(-10*24*time.Hour), retentionNow.Add(-time.Minute))
	explanation := explanationOver(before, before, dailyBackups(10, retentionNow)...)

	report := ValidateRetention(&explanation, RetentionObjectives{}, retentionNow)

	if report.Skipped != 4 {
		t.Errorf("expected all 4 checks to be skipped, got %d skipped: %+v", report.Skipped, report.Checks)
	}
	if !report.Pass {
		t.Error("skipped checks are not failures")
	}
	for _, check := range report.Checks {
		if check.Status == DoctorSkip && check.Name == RetentionCheckRPO && check.Remedy == "" {
			t.Error("a skipped check should say how to enable it")
		}
	}

	// Passing while having judged nothing must not read as a clean bill of health.
	buf := &bytes.Buffer{}
	if err := WriteRetentionReport(&report, "text", buf); err != nil {
		t.Fatalf("WriteRetentionReport failed: %v", err)
	}
	if strings.Contains(buf.String(), "Objectives met") {
		t.Errorf("a report that checked nothing must not claim objectives were met:\n%s", buf.String())
	}
}

func TestValidateRetention_ReportRestatesWhatItJudged(t *testing.T) {
	before := windowFrom(retentionNow.Add(-40*24*time.Hour), retentionNow.Add(-time.Minute))
	explanation := explanationOver(before, before, dailyBackups(40, retentionNow)...)

	report := ValidateRetention(&explanation, RetentionObjectives{
		RPO:             2 * time.Hour,
		RetentionWindow: 30 * 24 * time.Hour,
		RetainCount:     40,
	}, retentionNow)

	if report.Declared.RetainCount != 40 {
		t.Errorf("expected the retain count to be restated, got %d", report.Declared.RetainCount)
	}
	if report.Declared.RPO == "" || report.Declared.RetentionWindow == "" {
		t.Errorf("expected the objectives to be restated, got %+v", report.Declared)
	}
}

func TestWriteRetentionReport_JSONRoundTrips(t *testing.T) {
	before := windowFrom(retentionNow.Add(-40*24*time.Hour), retentionNow.Add(-time.Minute))
	explanation := explanationOver(before, before, dailyBackups(40, retentionNow)...)

	report := ValidateRetention(&explanation, RetentionObjectives{
		RPO:             time.Hour,
		RetentionWindow: 30 * 24 * time.Hour,
		RetainCount:     40,
	}, retentionNow)

	buf := &bytes.Buffer{}
	if err := WriteRetentionReport(&report, "json", buf); err != nil {
		t.Fatalf("WriteRetentionReport failed: %v", err)
	}

	var parsed RetentionReport
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("retention JSON does not parse: %v\noutput: %s", err, buf.String())
	}
	if len(parsed.Checks) != len(report.Checks) {
		t.Errorf("expected %d checks, got %d", len(report.Checks), len(parsed.Checks))
	}
}

func TestWriteRetentionReport_TextNamesTheFailures(t *testing.T) {
	before := windowFrom(retentionNow.Add(-10*24*time.Hour), retentionNow.Add(-time.Minute))
	explanation := explanationOver(before, before, dailyBackups(10, retentionNow)...)

	report := ValidateRetention(&explanation, RetentionObjectives{
		RetentionWindow: 30 * 24 * time.Hour,
		RetainCount:     10,
	}, retentionNow)

	buf := &bytes.Buffer{}
	if err := WriteRetentionReport(&report, "text", buf); err != nil {
		t.Fatalf("WriteRetentionReport failed: %v", err)
	}

	output := buf.String()
	for _, want := range []string{"retention-window", "[FAIL]", "Objectives NOT met"} {
		if !strings.Contains(output, want) {
			t.Errorf("expected the report to contain %q:\n%s", want, output)
		}
	}
}
