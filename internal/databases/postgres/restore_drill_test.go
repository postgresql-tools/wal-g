// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

package postgres

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The drill deletes what it restores. Every one of these cases is a way that
// deletion could land on something that matters.
func TestValidateDrillTarget_RefusesTheLiveDataDirectory(t *testing.T) {
	pgdata := t.TempDir()

	if _, err := ValidateDrillTarget(pgdata, pgdata); err == nil {
		t.Fatal("expected the live data directory to be refused")
	}
}

func TestValidateDrillTarget_RefusesInsideTheLiveDataDirectory(t *testing.T) {
	pgdata := t.TempDir()
	inside := filepath.Join(pgdata, "drill")

	_, err := ValidateDrillTarget(inside, pgdata)
	if err == nil {
		t.Fatal("expected a directory inside PGDATA to be refused")
	}
	if !strings.Contains(err.Error(), "live data directory") {
		t.Errorf("expected the reason to name the data directory, got %q", err)
	}
}

// A sibling whose name merely starts with the same characters is not inside it.
func TestValidateDrillTarget_AllowsSiblingWithSharedPrefix(t *testing.T) {
	base := t.TempDir()
	pgdata := filepath.Join(base, "data")
	sibling := filepath.Join(base, "data-drill")

	if err := os.Mkdir(pgdata, 0o700); err != nil {
		t.Fatalf("failed to create pgdata: %v", err)
	}

	created, err := ValidateDrillTarget(sibling, pgdata)
	if err != nil {
		t.Fatalf("expected a sibling directory to be allowed, got %v", err)
	}
	if !created {
		t.Error("a directory that does not exist yet should be reported as created")
	}
}

func TestValidateDrillTarget_RefusesNonEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "something"), []byte("x"), 0o600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	_, err := ValidateDrillTarget(dir, "")
	if err == nil {
		t.Fatal("expected a non-empty directory to be refused")
	}
	if !strings.Contains(err.Error(), "not empty") {
		t.Errorf("expected the reason to be emptiness, got %q", err)
	}
}

func TestValidateDrillTarget_AcceptsEmptyDirectoryWithoutClaimingToCreateIt(t *testing.T) {
	dir := t.TempDir()

	created, err := ValidateDrillTarget(dir, "")
	if err != nil {
		t.Fatalf("expected an empty directory to be accepted, got %v", err)
	}
	if created {
		t.Error("an existing directory must not be reported as created, or cleanup would remove it")
	}
}

func TestValidateDrillTarget_RefusesAFile(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "afile")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	if _, err := ValidateDrillTarget(file, ""); err == nil {
		t.Fatal("expected a file to be refused")
	}
}

func TestValidateDrillTarget_RefusesEmptyPath(t *testing.T) {
	if _, err := ValidateDrillTarget("   ", ""); err == nil {
		t.Fatal("expected an empty path to be refused")
	}
}

// PostgreSQL 12 replaced recovery.conf with a signal file plus ordinary
// settings, and a drill has to restore backups from both eras.
func TestWriteRecoveryConfig_ModernUsesSignalFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "PG_VERSION"), []byte("15\n"), 0o600); err != nil {
		t.Fatalf("failed to write PG_VERSION: %v", err)
	}

	opts := RestoreDrillOptions{TargetDir: dir, WalgBinary: "/usr/bin/wal-g"}
	if err := writeRecoveryConfig(opts); err != nil {
		t.Fatalf("writeRecoveryConfig failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "recovery.signal")); err != nil {
		t.Errorf("expected recovery.signal to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "recovery.conf")); err == nil {
		t.Error("PG 12+ must not get a recovery.conf")
	}

	conf, err := os.ReadFile(filepath.Join(dir, "postgresql.conf"))
	if err != nil {
		t.Fatalf("failed to read postgresql.conf: %v", err)
	}
	if !strings.Contains(string(conf), "restore_command") {
		t.Errorf("expected a restore_command in postgresql.conf, got:\n%s", conf)
	}
	if !strings.Contains(string(conf), "wal-fetch") {
		t.Errorf("expected restore_command to call wal-fetch, got:\n%s", conf)
	}
}

func TestWriteRecoveryConfig_LegacyUsesRecoveryConf(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "PG_VERSION"), []byte("11\n"), 0o600); err != nil {
		t.Fatalf("failed to write PG_VERSION: %v", err)
	}

	opts := RestoreDrillOptions{TargetDir: dir, WalgBinary: "/usr/bin/wal-g"}
	if err := writeRecoveryConfig(opts); err != nil {
		t.Fatalf("writeRecoveryConfig failed: %v", err)
	}

	recoveryConf, err := os.ReadFile(filepath.Join(dir, "recovery.conf"))
	if err != nil {
		t.Fatalf("expected a recovery.conf: %v", err)
	}
	if !strings.Contains(string(recoveryConf), "restore_command") {
		t.Errorf("expected a restore_command, got:\n%s", recoveryConf)
	}
	if _, err := os.Stat(filepath.Join(dir, "recovery.signal")); err == nil {
		t.Error("PG 11 must not get a recovery.signal")
	}
}

// An old-style minor version like "9.6" must not be read as major 9 only by
// accident of parsing - it has to parse at all.
func TestWriteRecoveryConfig_HandlesMinorVersionedPGVersion(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "PG_VERSION"), []byte("9.6\n"), 0o600); err != nil {
		t.Fatalf("failed to write PG_VERSION: %v", err)
	}

	opts := RestoreDrillOptions{TargetDir: dir, WalgBinary: "/usr/bin/wal-g"}
	if err := writeRecoveryConfig(opts); err != nil {
		t.Fatalf("writeRecoveryConfig failed: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "recovery.conf")); err != nil {
		t.Errorf("expected 9.6 to use recovery.conf: %v", err)
	}
}

func TestWriteRecoveryConfig_MissingPGVersionIsAnError(t *testing.T) {
	dir := t.TempDir()

	if err := writeRecoveryConfig(RestoreDrillOptions{TargetDir: dir}); err == nil {
		t.Fatal("expected a missing PG_VERSION to be an error")
	}
}

// A path with a quote in it must not break out of the single-quoted setting.
func TestWriteRecoveryConfig_QuotesArePreserved(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "PG_VERSION"), []byte("16\n"), 0o600); err != nil {
		t.Fatalf("failed to write PG_VERSION: %v", err)
	}

	opts := RestoreDrillOptions{TargetDir: dir, WalgBinary: "/opt/wal'g/wal-g"}
	if err := writeRecoveryConfig(opts); err != nil {
		t.Fatalf("writeRecoveryConfig failed: %v", err)
	}

	conf, err := os.ReadFile(filepath.Join(dir, "postgresql.conf"))
	if err != nil {
		t.Fatalf("failed to read postgresql.conf: %v", err)
	}
	if !strings.Contains(string(conf), "wal''g") {
		t.Errorf("expected the quote to be doubled for postgresql.conf, got:\n%s", conf)
	}
}

func TestRTOPhase_OverBudgetFails(t *testing.T) {
	report := &RestoreDrillReport{}

	phase := rtoPhase(report, RestoreDrillOptions{RTO: time.Hour}, 90*time.Minute)

	if phase.Status != DoctorFail {
		t.Fatalf("expected a failure over budget, got %s: %s", phase.Status, phase.Summary)
	}
	if !strings.Contains(phase.Summary, "over the") {
		t.Errorf("expected the summary to say it was over budget, got %q", phase.Summary)
	}
}

// A fetch-only drill must not let an RTO pass be read as a full recovery
// rehearsal, because replay time is missing from it.
func TestRTOPhase_FetchOnlyPassSaysReplayIsNotIncluded(t *testing.T) {
	report := &RestoreDrillReport{PostgresStarted: false}

	phase := rtoPhase(report, RestoreDrillOptions{RTO: time.Hour}, 10*time.Minute)

	if phase.Status != DoctorPass {
		t.Fatalf("expected a pass under budget, got %s", phase.Status)
	}
	if !strings.Contains(phase.Summary, "fetch only") {
		t.Errorf("expected the scope to be stated, got %q", phase.Summary)
	}
	if !strings.Contains(phase.Remedy, "NOT included") {
		t.Errorf("expected the missing replay time to be called out, got %q", phase.Remedy)
	}
}

func TestRTOPhase_WithReplayReportsTheFullScope(t *testing.T) {
	report := &RestoreDrillReport{PostgresStarted: true}

	phase := rtoPhase(report, RestoreDrillOptions{RTO: time.Hour}, 10*time.Minute)

	if !strings.Contains(phase.Summary, "fetch and replay") {
		t.Errorf("expected the scope to include replay, got %q", phase.Summary)
	}
	if phase.Remedy != "" {
		t.Errorf("a full drill needs no caveat, got %q", phase.Remedy)
	}
}

func TestRTOPhase_NoBudgetSkipsButStillReportsTheTime(t *testing.T) {
	report := &RestoreDrillReport{}

	phase := rtoPhase(report, RestoreDrillOptions{}, 12*time.Minute)

	if phase.Status != DoctorSkip {
		t.Fatalf("expected a skip without a declared RTO, got %s", phase.Status)
	}
	if !strings.Contains(phase.Summary, "12m") {
		t.Errorf("expected the measured time to be reported anyway, got %q", phase.Summary)
	}
}

func TestLastLines(t *testing.T) {
	output := "one\ntwo\nthree\nfour\nfive\n"

	got := lastLines(output, 2)
	if got != "four\nfive" {
		t.Errorf("expected the last two lines, got %q", got)
	}

	if got := lastLines("only\n", 5); got != "only" {
		t.Errorf("expected the whole output when it is shorter, got %q", got)
	}
}

func TestRestoreDrillReport_JSONRoundTrips(t *testing.T) {
	report := &RestoreDrillReport{
		BackupName: "base_000000010000000000000002",
		ScratchDir: "/mnt/drill",
	}
	report.add(DrillPhase{Name: DrillCheckTarget, Status: DoctorPass, Summary: "ok"})
	report.add(DrillPhase{Name: DrillCheckFetch, Status: DoctorFail, Summary: "the restore failed"})
	report.Pass = report.Failed == 0

	buf := &bytes.Buffer{}
	if err := WriteRestoreDrillReport(report, "json", buf); err != nil {
		t.Fatalf("WriteRestoreDrillReport failed: %v", err)
	}

	var parsed RestoreDrillReport
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("drill JSON does not parse: %v\noutput: %s", err, buf.String())
	}
	if parsed.Pass {
		t.Error("a report with a failed phase must not pass")
	}
	if len(parsed.Phases) != 2 {
		t.Errorf("expected 2 phases, got %d", len(parsed.Phases))
	}
}

// The text report has to say where the restored cluster went, since a drill
// that kept it has left a full copy of the database on disk.
func TestRestoreDrillReport_TextSaysWhereTheClusterWent(t *testing.T) {
	kept := &RestoreDrillReport{ScratchDir: "/mnt/drill", Pass: true, ScratchDirPresent: true}
	kept.add(DrillPhase{Name: DrillCheckFetch, Status: DoctorPass, Summary: "restored"})

	buf := &bytes.Buffer{}
	if err := WriteRestoreDrillReport(kept, "text", buf); err != nil {
		t.Fatalf("WriteRestoreDrillReport failed: %v", err)
	}
	if !strings.Contains(buf.String(), "left in /mnt/drill") {
		t.Errorf("expected the kept directory to be named, got:\n%s", buf.String())
	}

	removed := &RestoreDrillReport{ScratchDir: "/mnt/drill", Pass: true, ScratchDirRemoved: true}
	removed.add(DrillPhase{Name: DrillCheckFetch, Status: DoctorPass, Summary: "restored"})

	buf.Reset()
	if err := WriteRestoreDrillReport(removed, "text", buf); err != nil {
		t.Fatalf("WriteRestoreDrillReport failed: %v", err)
	}
	if !strings.Contains(buf.String(), "removed") {
		t.Errorf("expected the cleanup to be reported, got:\n%s", buf.String())
	}

	// A restore that failed before writing anything leaves nothing behind, even
	// with --keep. Pointing someone at a directory that is not there is the same
	// kind of wrong as claiming cleanup that did not happen.
	nothing := &RestoreDrillReport{ScratchDir: "/mnt/drill"}
	nothing.add(DrillPhase{Name: DrillCheckFetch, Status: DoctorFail, Summary: "the restore failed"})

	buf.Reset()
	if err := WriteRestoreDrillReport(nothing, "text", buf); err != nil {
		t.Fatalf("WriteRestoreDrillReport failed: %v", err)
	}
	if strings.Contains(buf.String(), "left in") {
		t.Errorf("must not claim a cluster was left when nothing is on disk, got:\n%s", buf.String())
	}
}

// Cleanup must remove a directory it created, but only empty one it was handed.
func TestCleanupDrill_RemovesOnlyWhatItCreated(t *testing.T) {
	parent := t.TempDir()
	created := filepath.Join(parent, "made-by-drill")
	if err := os.MkdirAll(filepath.Join(created, "base"), 0o700); err != nil {
		t.Fatalf("failed to build tree: %v", err)
	}

	report := &RestoreDrillReport{}
	cleanupDrill(report, RestoreDrillOptions{TargetDir: created}, true)

	if _, err := os.Stat(created); !os.IsNotExist(err) {
		t.Errorf("expected a created directory to be removed entirely")
	}
	if !report.ScratchDirRemoved {
		t.Error("cleanup should record that it removed the directory")
	}

	handed := t.TempDir()
	if err := os.MkdirAll(filepath.Join(handed, "base"), 0o700); err != nil {
		t.Fatalf("failed to build tree: %v", err)
	}

	report = &RestoreDrillReport{}
	cleanupDrill(report, RestoreDrillOptions{TargetDir: handed}, false)

	if _, err := os.Stat(handed); err != nil {
		t.Errorf("a directory the drill did not create must survive: %v", err)
	}
	entries, err := os.ReadDir(handed)
	if err != nil {
		t.Fatalf("failed to read directory: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected the directory to be emptied, got %d entr(ies)", len(entries))
	}
}

func TestCleanupDrill_KeepLeavesEverythingAlone(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "base"), []byte("x"), 0o600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	report := &RestoreDrillReport{}
	cleanupDrill(report, RestoreDrillOptions{TargetDir: dir, Keep: true}, true)

	if _, err := os.Stat(filepath.Join(dir, "base")); err != nil {
		t.Errorf("--keep must leave the restored cluster in place: %v", err)
	}
	if report.ScratchDirRemoved {
		t.Error("--keep must not report the directory as removed")
	}
}
