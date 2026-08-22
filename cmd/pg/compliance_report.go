// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

package pg

import (
	"bytes"
	// aliased: package pg already declares a package-level var named "json"
	// (cmd/pg/backup_list.go), which collides with the unaliased import name.
	encjson "encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/wal-g/tracelog"

	conf "github.com/lateos-ai/wal-g/internal/config"
	"github.com/lateos-ai/wal-g/internal/databases/postgres"
	"github.com/lateos-ai/wal-g/utility"
)

const (
	complianceReportUse   = "compliance-report"
	complianceReportShort = "Aggregate doctor, backup-verify, retention-validate, pitr-window, and " +
		"restore-test into one evidence report"
	complianceReportLong = `Run the fork's backup/recovery checks and collect their output into a single
report, for handing to an auditor or attaching to a change record.

This is an evidence aggregator, not a certified compliance report: it does not
produce a SOC2 report (that requires an independent auditor's opinion) or a
CMMC assessment (that requires a formal assessor). The categories attached to
each check are illustrative starting points, not a vetted mapping to CMMC
practices or SOC2 Trust Services Criteria - review them with your compliance
team before citing this report in an audit.

Checks run:
  doctor              preflight configuration, storage, and connectivity
  backup-verify       Tier 1 integrity verification of a backup
  retention-validate  whether the retention policy delivers its declared RPO/window
  pitr-window         the range of time currently restorable
  restore-test        a real scratch-directory restore rehearsal, opt-in via
                      --restore-test-target-dir (skipped otherwise, since it
                      starts a restored cluster and needs somewhere to put it)

Each check runs as a separate wal-g process - this same binary, re-invoked -
exactly as an operator would run it by hand. Its own --format json output is
embedded verbatim as evidence. Pass/fail per check comes from its exit code:
0 pass, 1 fail; backup-verify's documented exit code 2 ("could not complete")
is reported as its own "error" status rather than folded into "fail".

The exit code is 0 when every check that ran passed, 1 otherwise. A skipped
check does not affect it.`

	complianceReportFormatFlag        = "format"
	complianceReportFormatDescription = "Output format: text or json. Default: text."

	complianceReportTargetDirFlag        = "restore-test-target-dir"
	complianceReportTargetDirDescription = "Run restore-test into this directory as part of the report. " +
		"Default: restore-test is skipped."

	complianceReportBackupNameFlag        = "backup-name"
	complianceReportBackupNameDescription = "Backup to pass to backup-verify. Default: LATEST."

	complianceReportSampleFlag        = "sample"
	complianceReportSampleDescription = "Percentage of tar partitions for backup-verify Tier 2 sampling. " +
		"Default: Tier 1 only."

	complianceReportMinWindowFlag        = "min-window"
	complianceReportMinWindowDescription = "Passed through to pitr-window --min-window."
)

// ComplianceCheckStatus is the pass/fail verdict compliance-report assigns to
// one underlying check, derived from that check's own process exit code
// rather than from parsing its report body - the five checks don't agree on
// where (or whether) a "pass" field lives in their JSON: pitr-window's report
// has none at all.
type ComplianceCheckStatus string

const (
	ComplianceCheckPass    ComplianceCheckStatus = "pass"
	ComplianceCheckFail    ComplianceCheckStatus = "fail"
	ComplianceCheckError   ComplianceCheckStatus = "error"
	ComplianceCheckSkipped ComplianceCheckStatus = "skipped"
)

// ComplianceCheckResult is one row of a ComplianceReport.
type ComplianceCheckResult struct {
	Check      string                `json:"check"`
	Status     ComplianceCheckStatus `json:"status"`
	ExitCode   int                   `json:"exit_code,omitempty"`
	Categories []string              `json:"categories,omitempty"`
	Summary    string                `json:"summary"`
	Detail     encjson.RawMessage    `json:"detail,omitempty"`
	Error      string                `json:"error,omitempty"`
}

// ComplianceReport aggregates the evidence from one compliance-report run.
type ComplianceReport struct {
	GeneratedAt string                  `json:"generated_at"`
	Pass        bool                    `json:"pass"`
	Checks      []ComplianceCheckResult `json:"checks"`
}

// complianceCategories are illustrative starting points for describing each
// check in a compliance framework's language. They are not a vetted mapping
// to any specific CMMC practice ID or SOC2 Trust Services Criterion.
var complianceCategories = map[string][]string{
	"doctor":             {"system monitoring", "preventive maintenance"},
	"backup-verify":      {"backup integrity", "data recovery"},
	"retention-validate": {"data retention", "recovery point objective"},
	"restore-test":       {"recovery testing", "recovery time objective"},
	"pitr-window":        {"recovery point objective", "data availability"},
}

var (
	complianceReportFormat     string
	complianceReportTargetDir  string
	complianceReportBackupName string
	complianceReportSample     int
	complianceReportMinWindow  time.Duration

	complianceReportCmd = &cobra.Command{
		Use:   complianceReportUse,
		Short: complianceReportShort,
		Long:  complianceReportLong,
		Args:  cobra.NoArgs,
		Run:   runComplianceReport,
	}
)

// complianceCheckRun describes one wal-g subcommand to run as evidence, or
// (when skip is non-empty) a check deliberately not run.
type complianceCheckRun struct {
	name string
	args []string
	skip string
}

func runComplianceReport(cmd *cobra.Command, args []string) {
	tracelog.ErrorLogger.FatalOnError(postgres.ValidateReportFormat(complianceReportFormat))

	self, err := os.Executable()
	tracelog.ErrorLogger.FatalfOnError("Could not determine this binary's path: %v", err)

	report := ComplianceReport{
		GeneratedAt: utility.TimeNowCrossPlatformUTC().Format(time.RFC3339),
	}

	for _, run := range complianceCheckRuns() {
		report.Checks = append(report.Checks, runComplianceCheck(self, run))
	}

	report.Pass = complianceReportPass(report.Checks)

	writeComplianceReport(&report, complianceReportFormat, os.Stdout)

	if !report.Pass {
		os.Exit(1)
	}
}

// complianceCheckRuns builds the ordered list of checks to run, honoring
// which flags were actually supplied so an unsupplied restore-test target
// directory skips that check instead of failing it.
func complianceCheckRuns() []complianceCheckRun {
	backupVerifyArgs := []string{"backup-verify"}
	if complianceReportBackupName != "" {
		backupVerifyArgs = append(backupVerifyArgs, complianceReportBackupName)
	}
	if complianceReportSample > 0 {
		backupVerifyArgs = append(backupVerifyArgs, "--sample", fmt.Sprint(complianceReportSample))
	}

	pitrWindowArgs := []string{"pitr-window"}
	if complianceReportMinWindow > 0 {
		pitrWindowArgs = append(pitrWindowArgs, "--min-window", complianceReportMinWindow.String())
	}

	runs := []complianceCheckRun{
		{name: "doctor", args: []string{"doctor"}},
		{name: "backup-verify", args: backupVerifyArgs},
		{name: "retention-validate", args: []string{"retention-validate"}},
		{name: "pitr-window", args: pitrWindowArgs},
	}

	if complianceReportTargetDir == "" {
		runs = append(runs, complianceCheckRun{
			name: "restore-test",
			skip: "skipped: no --restore-test-target-dir given",
		})
	} else {
		runs = append(runs, complianceCheckRun{
			name: "restore-test",
			args: []string{"restore-test", "--target-dir", complianceReportTargetDir},
		})
	}

	return runs
}

// runComplianceCheck runs one check as a child process of this same binary -
// the same invocation an operator would run by hand - and turns its exit
// code and --format json output into one report row. A --config flag is not
// inherited by a subprocess, so it is forwarded explicitly, the same way
// restore-test forwards it to the wal-g process it spawns for the fetch.
func runComplianceCheck(self string, run complianceCheckRun) ComplianceCheckResult {
	result := ComplianceCheckResult{
		Check:      run.name,
		Categories: complianceCategories[run.name],
	}

	if run.skip != "" {
		result.Status = ComplianceCheckSkipped
		result.Summary = run.skip
		return result
	}

	args := append([]string{}, run.args...)
	args = append(args, "--format", "json")
	if conf.CfgFile != "" {
		args = append([]string{"--config", conf.CfgFile}, args...)
	}

	stdout, runErr := exec.Command(self, args...).Output()

	exitCode := 0
	stderr := ""
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			exitCode = exitErr.ExitCode()
			stderr = string(exitErr.Stderr)
		} else {
			result.Status = ComplianceCheckError
			result.Summary = "failed to run " + run.name
			result.Error = runErr.Error()
			return result
		}
	}

	result.ExitCode = exitCode
	result.Status = complianceStatusForExitCode(run.name, exitCode)
	result.Summary = complianceSummary(result.Status, run.name, stderr)

	if trimmed := bytes.TrimSpace(stdout); len(trimmed) > 0 {
		result.Detail = encjson.RawMessage(trimmed)
	}

	return result
}

// complianceStatusForExitCode maps a check's own exit-code contract onto the
// four-value compliance status. Every check but backup-verify uses plain 0/1;
// backup-verify's documented exit code 2 ("could not complete") is kept
// distinct from "fail" rather than folded into it.
func complianceStatusForExitCode(check string, exitCode int) ComplianceCheckStatus {
	switch {
	case exitCode == 0:
		return ComplianceCheckPass
	case check == "backup-verify" && exitCode == 2:
		return ComplianceCheckError
	default:
		return ComplianceCheckFail
	}
}

func complianceSummary(status ComplianceCheckStatus, check, stderr string) string {
	stderr = strings.TrimSpace(stderr)

	switch status {
	case ComplianceCheckPass:
		return check + " passed"
	case ComplianceCheckError:
		if stderr != "" {
			return check + " could not complete: " + firstLine(stderr)
		}
		return check + " could not complete"
	default:
		return check + " failed"
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// complianceReportPass fails the report if any check failed or errored;
// skipped checks do not affect it, the same rule doctor and
// retention-validate already use for warnings.
func complianceReportPass(checks []ComplianceCheckResult) bool {
	for _, check := range checks {
		if check.Status == ComplianceCheckFail || check.Status == ComplianceCheckError {
			return false
		}
	}
	return true
}

func writeComplianceReport(report *ComplianceReport, format string, output io.Writer) {
	err := postgres.WriteReport(format, report, func(w io.Writer) { writeComplianceReportText(report, w) }, output)
	if err != nil {
		tracelog.ErrorLogger.Printf("Failed to write the report: %v\n", err)
	}
}

func writeComplianceReportText(report *ComplianceReport, output io.Writer) {
	fmt.Fprintf(output, "wal-g compliance-report\n\n")
	fmt.Fprintf(output, "Generated %s\n\n", report.GeneratedAt)

	for _, check := range report.Checks {
		fmt.Fprintf(output, "%-6s %-20s %s\n", complianceStatusLabel(check.Status), check.Check, check.Summary)
		if len(check.Categories) > 0 {
			fmt.Fprintf(output, "       categories: %s\n", strings.Join(check.Categories, ", "))
		}
	}

	fmt.Fprintln(output)
	if report.Pass {
		fmt.Fprintln(output, "PASS")
	} else {
		fmt.Fprintln(output, "FAIL")
	}

	fmt.Fprintln(output)
	fmt.Fprintln(output, "This is an evidence aggregator, not a certified compliance report. Categories "+
		"are illustrative, not a vetted mapping to CMMC practices or SOC2 Trust Services Criteria - review "+
		"with your compliance team before citing this report in an audit.")
}

func complianceStatusLabel(status ComplianceCheckStatus) string {
	switch status {
	case ComplianceCheckPass:
		return "PASS"
	case ComplianceCheckFail:
		return "FAIL"
	case ComplianceCheckError:
		return "ERROR"
	case ComplianceCheckSkipped:
		return "SKIP"
	default:
		return "?"
	}
}

func init() {
	Cmd.AddCommand(complianceReportCmd)

	complianceReportCmd.Flags().StringVar(&complianceReportFormat, complianceReportFormatFlag, "text",
		complianceReportFormatDescription)
	complianceReportCmd.Flags().StringVar(&complianceReportTargetDir, complianceReportTargetDirFlag, "",
		complianceReportTargetDirDescription)
	complianceReportCmd.Flags().StringVar(&complianceReportBackupName, complianceReportBackupNameFlag, "",
		complianceReportBackupNameDescription)
	complianceReportCmd.Flags().IntVar(&complianceReportSample, complianceReportSampleFlag, 0,
		complianceReportSampleDescription)
	complianceReportCmd.Flags().DurationVar(&complianceReportMinWindow, complianceReportMinWindowFlag, 0,
		complianceReportMinWindowDescription)
}
