// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

package pg

import "testing"

func TestComplianceStatusForExitCode(t *testing.T) {
	tests := []struct {
		name     string
		check    string
		exitCode int
		want     ComplianceCheckStatus
	}{
		{"doctor pass", "doctor", 0, ComplianceCheckPass},
		{"doctor fail", "doctor", 1, ComplianceCheckFail},
		{"retention-validate fail", "retention-validate", 1, ComplianceCheckFail},
		{"pitr-window fail", "pitr-window", 1, ComplianceCheckFail},
		{"restore-test fail", "restore-test", 1, ComplianceCheckFail},
		{"backup-verify pass", "backup-verify", 0, ComplianceCheckPass},
		{"backup-verify fail", "backup-verify", 1, ComplianceCheckFail},
		{"backup-verify could not complete", "backup-verify", 2, ComplianceCheckError},
		// exit code 2 only carries the special "could not complete" meaning for
		// backup-verify - no other check documents a 2, so treat it as a plain fail.
		{"doctor exit 2 is just a fail", "doctor", 2, ComplianceCheckFail},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := complianceStatusForExitCode(tt.check, tt.exitCode)
			if got != tt.want {
				t.Errorf("complianceStatusForExitCode(%q, %d) = %q, want %q", tt.check, tt.exitCode, got, tt.want)
			}
		})
	}
}

func TestComplianceReportPass(t *testing.T) {
	tests := []struct {
		name   string
		checks []ComplianceCheckResult
		want   bool
	}{
		{
			name:   "all pass",
			checks: []ComplianceCheckResult{{Status: ComplianceCheckPass}, {Status: ComplianceCheckPass}},
			want:   true,
		},
		{
			name: "skipped check does not fail the report",
			checks: []ComplianceCheckResult{
				{Status: ComplianceCheckPass},
				{Status: ComplianceCheckSkipped},
			},
			want: true,
		},
		{
			name: "one failed check fails the report",
			checks: []ComplianceCheckResult{
				{Status: ComplianceCheckPass},
				{Status: ComplianceCheckFail},
			},
			want: false,
		},
		{
			name: "one errored check fails the report",
			checks: []ComplianceCheckResult{
				{Status: ComplianceCheckPass},
				{Status: ComplianceCheckError},
			},
			want: false,
		},
		{
			name:   "no checks pass vacuously",
			checks: nil,
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := complianceReportPass(tt.checks); got != tt.want {
				t.Errorf("complianceReportPass() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestComplianceCheckRuns(t *testing.T) {
	t.Run("restore-test skipped when no target dir given", func(t *testing.T) {
		orig := complianceReportTargetDir
		complianceReportTargetDir = ""
		defer func() { complianceReportTargetDir = orig }()

		runs := complianceCheckRuns()

		last := runs[len(runs)-1]
		if last.name != "restore-test" {
			t.Fatalf("expected the last check to be restore-test, got %q", last.name)
		}
		if last.skip == "" {
			t.Errorf("expected restore-test to be skipped when no target dir is set")
		}
		if len(last.args) != 0 {
			t.Errorf("expected no args for a skipped check, got %v", last.args)
		}
	})

	t.Run("restore-test forwarded when target dir given", func(t *testing.T) {
		origDir := complianceReportTargetDir
		complianceReportTargetDir = "/tmp/drill"
		defer func() { complianceReportTargetDir = origDir }()

		runs := complianceCheckRuns()

		last := runs[len(runs)-1]
		if last.skip != "" {
			t.Errorf("expected restore-test to run, got skip reason %q", last.skip)
		}
		wantArgs := []string{"restore-test", "--target-dir", "/tmp/drill"}
		if len(last.args) != len(wantArgs) {
			t.Fatalf("args = %v, want %v", last.args, wantArgs)
		}
		for i := range wantArgs {
			if last.args[i] != wantArgs[i] {
				t.Errorf("args[%d] = %q, want %q", i, last.args[i], wantArgs[i])
			}
		}
	})

	t.Run("backup-verify forwards backup name and sample", func(t *testing.T) {
		origName, origSample := complianceReportBackupName, complianceReportSample
		complianceReportBackupName = "base_000000010000000000000001"
		complianceReportSample = 10
		defer func() {
			complianceReportBackupName = origName
			complianceReportSample = origSample
		}()

		runs := complianceCheckRuns()

		var verify complianceCheckRun
		for _, r := range runs {
			if r.name == "backup-verify" {
				verify = r
			}
		}

		wantArgs := []string{"backup-verify", "base_000000010000000000000001", "--sample", "10"}
		if len(verify.args) != len(wantArgs) {
			t.Fatalf("args = %v, want %v", verify.args, wantArgs)
		}
		for i := range wantArgs {
			if verify.args[i] != wantArgs[i] {
				t.Errorf("args[%d] = %q, want %q", i, verify.args[i], wantArgs[i])
			}
		}
	})
}
