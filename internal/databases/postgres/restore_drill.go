// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/wal-g/tracelog"

	"github.com/lateos-ai/wal-g/pkg/storages/storage"
	"github.com/lateos-ai/wal-g/utility"
)

// Drill phase names, stable enough to be matched on by scripts.
const (
	DrillCheckTarget = "target-dir"
	DrillCheckSpace  = "space"
	DrillCheckFetch  = "fetch"
	DrillCheckReplay = "replay"
	DrillCheckRTO    = "rto"
	DrillCheckRPO    = "rpo"
)

// DefaultDrillPort is off the usual 5432 so a drill cannot collide with a
// running cluster on the same host.
const DefaultDrillPort = 5433

// DefaultDrillStartTimeout bounds waiting for the restored cluster to come up.
const DefaultDrillStartTimeout = 10 * time.Minute

// RestoreDrillOptions carries the tunables for one restore drill.
type RestoreDrillOptions struct {
	// TargetDir is where the backup is restored. It must be empty or absent,
	// and must not be the live data directory.
	TargetDir string
	// BackupName is the backup to restore; empty means LATEST.
	BackupName string
	// RTO is the recovery time budget the drill is judged against.
	RTO time.Duration
	// RPO is the data loss budget the reachable recovery point is judged against.
	RPO time.Duration
	// StartPostgres runs the restored cluster and measures WAL replay. Without
	// it the drill measures the fetch only, and says so.
	StartPostgres bool
	// PGCtlPath is the pg_ctl to start the restored cluster with.
	PGCtlPath string
	// Port is the port the restored cluster listens on during the drill.
	Port int
	// Keep leaves the restored directory in place for inspection.
	Keep bool
	// WalgBinary is the wal-g used for the restore and for restore_command.
	WalgBinary string
	// ConfigFile is passed through to those invocations, since a subprocess does
	// not inherit a --config flag.
	ConfigFile string
	// SpaceMargin is the free-space multiple required before restoring.
	SpaceMargin float64
	// StartTimeout bounds waiting for the restored cluster to accept connections.
	StartTimeout time.Duration
	// PGDataDir is the live data directory the target must not be, normally PGDATA.
	PGDataDir string
	// Format is "text" or "json".
	Format string
}

// DrillPhase is one step of the drill.
type DrillPhase struct {
	Name    string       `json:"name"`
	Status  DoctorStatus `json:"status"`
	Summary string       `json:"summary"`
	Detail  string       `json:"detail,omitempty"`
	Remedy  string       `json:"remedy,omitempty"`
	Elapsed string       `json:"elapsed,omitempty"`
}

// RestoreDrillReport is the result of a restore drill.
type RestoreDrillReport struct {
	Pass       bool         `json:"pass"`
	BackupName string       `json:"backup_name,omitempty"`
	Phases     []DrillPhase `json:"phases"`

	// RestoredBytes and FetchSeconds describe the restore that was actually
	// performed, so a drill can be compared against the last one.
	RestoredBytes  int64   `json:"restored_bytes,omitempty"`
	FetchSeconds   float64 `json:"fetch_seconds,omitempty"`
	ReplaySeconds  float64 `json:"replay_seconds,omitempty"`
	TotalSeconds   float64 `json:"total_seconds"`
	ThroughputMBps float64 `json:"throughput_mibps,omitempty"`

	// PostgresStarted records whether replay was measured or only the fetch was.
	PostgresStarted bool `json:"postgres_started"`

	// ScratchDirRemoved says whether the drill cleaned up after itself.
	ScratchDirRemoved bool `json:"scratch_dir_removed"`
	// ScratchDirPresent says whether anything is actually on disk at the end. It
	// is not simply the inverse of ScratchDirRemoved: a restore that failed
	// before writing anything leaves nothing behind even with --keep, and
	// claiming otherwise would send someone looking for a cluster that is not
	// there.
	ScratchDirPresent bool   `json:"scratch_dir_present"`
	ScratchDir        string `json:"scratch_dir,omitempty"`

	Passed  int `json:"passed"`
	Warned  int `json:"warned"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
}

func (r *RestoreDrillReport) add(phase DrillPhase) {
	r.Phases = append(r.Phases, phase)

	switch phase.Status {
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

// failed reports whether any phase has failed so far, so the drill can stop
// before doing something expensive on top of a broken precondition.
func (r *RestoreDrillReport) failed() bool {
	return r.Failed > 0
}

// ValidateDrillTarget checks that a directory is safe to restore into.
//
// The drill writes a whole cluster and then deletes it. Pointing that at a live
// data directory would destroy the thing being protected, so the two rules are
// absolute: never the live data directory, and never a directory that already
// has anything in it.
//
// It returns whether the directory had to be created, which is what cleanup
// uses to decide between removing the directory and only emptying it.
func ValidateDrillTarget(targetDir, pgDataDir string) (created bool, err error) {
	if strings.TrimSpace(targetDir) == "" {
		return false, fmt.Errorf("no target directory given")
	}

	absTarget, err := filepath.Abs(targetDir)
	if err != nil {
		return false, fmt.Errorf("could not resolve %s: %w", targetDir, err)
	}

	if pgDataDir != "" {
		absData, err := filepath.Abs(pgDataDir)
		if err == nil && sameOrInside(absTarget, absData) {
			return false, fmt.Errorf(
				"%s is the live data directory, or inside it: a drill would destroy it", absTarget)
		}
	}

	info, err := os.Stat(absTarget)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("could not inspect %s: %w", absTarget, err)
	}
	if !info.IsDir() {
		return false, fmt.Errorf("%s is not a directory", absTarget)
	}

	entries, err := os.ReadDir(absTarget)
	if err != nil {
		return false, fmt.Errorf("could not read %s: %w", absTarget, err)
	}
	if len(entries) > 0 {
		return false, fmt.Errorf(
			"%s is not empty: refusing to restore over %d existing entr(ies)", absTarget, len(entries))
	}

	return false, nil
}

// sameOrInside reports whether path is root or lives under it.
func sameOrInside(path, root string) bool {
	if path == root {
		return true
	}

	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}

	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// HandleRestoreDrill restores a backup into a scratch directory, measures how
// long recovery takes, and judges it against the declared objectives. It
// returns the process exit code: 0 when nothing failed, 1 otherwise.
//
// The restore runs as a subprocess rather than in this process. A fetch that
// fails hard calls os.Exit, and a drill that dies without a verdict - and
// without removing the cluster it half-wrote - is a poor test harness. Running
// the documented command also means the drill exercises the path an operator
// would actually use, config resolution included.
func HandleRestoreDrill(rootFolder storage.Folder, opts RestoreDrillOptions, output io.Writer) int {
	if opts.Port == 0 {
		opts.Port = DefaultDrillPort
	}
	if opts.StartTimeout <= 0 {
		opts.StartTimeout = DefaultDrillStartTimeout
	}
	if opts.SpaceMargin <= 0 {
		opts.SpaceMargin = DefaultRestoreSpaceMargin
	}

	report := &RestoreDrillReport{ScratchDir: opts.TargetDir}
	started := time.Now()

	created, targetErr := ValidateDrillTarget(opts.TargetDir, opts.PGDataDir)
	report.add(targetPhase(opts, targetErr))

	if report.failed() {
		return finishDrill(report, opts, started, nil, output)
	}

	backup, resolveErr := resolveBackup(rootFolder, opts.BackupName)
	if resolveErr != nil {
		report.add(DrillPhase{
			Name:    DrillCheckFetch,
			Status:  DoctorFail,
			Summary: "could not resolve the backup to restore",
			Detail:  resolveErr.Error(),
		})
		return finishDrill(report, opts, started, nil, output)
	}
	report.BackupName = backup.Name

	report.add(spacePhase(backup, opts))
	if report.failed() {
		// Nothing has been written yet: the restore has not started.
		return finishDrill(report, opts, started, nil, output)
	}

	// From here on the restore may leave a directory behind, so every exit runs
	// cleanup - and runs it before reporting, so the report states what actually
	// happened to the restored cluster rather than what is about to.
	cleanup := func() { cleanupDrill(report, opts, created) }

	runFetchPhase(report, opts, backup.Name)

	if !report.failed() {
		runReplayPhase(report, opts)
	}

	report.add(rtoPhase(report, opts, time.Since(started)))
	report.add(rpoPhase(rootFolder, opts))

	return finishDrill(report, opts, started, cleanup, output)
}

func targetPhase(opts RestoreDrillOptions, err error) DrillPhase {
	phase := DrillPhase{Name: DrillCheckTarget}

	if err != nil {
		phase.Status = DoctorFail
		phase.Summary = "target directory is not safe to restore into"
		phase.Detail = err.Error()
		phase.Remedy = "Point --target-dir at an empty or non-existent directory on a volume " +
			"with room for the restore."
		return phase
	}

	phase.Status = DoctorPass
	phase.Summary = fmt.Sprintf("%s is empty and is not the live data directory", opts.TargetDir)

	return phase
}

func spacePhase(backup Backup, opts RestoreDrillOptions) DrillPhase {
	phase := DrillPhase{Name: DrillCheckSpace}

	// The same sizing backup-fetch runs, so a drill cannot disagree with the
	// restore it is rehearsing.
	preflight, err := CheckRestoreSpace(backup, opts.TargetDir, opts.SpaceMargin)
	if err != nil {
		phase.Status = DoctorWarn
		phase.Summary = "could not size the restore"
		phase.Detail = err.Error()
		return phase
	}

	phase.Detail = preflight.Summary()

	switch preflight.Verdict {
	case SpaceInsufficient:
		phase.Status = DoctorFail
		phase.Summary = "not enough free space to restore into the target directory"
		phase.Remedy = "Free space on the target volume, or point --target-dir somewhere larger."
	case SpaceUnknown:
		phase.Status = DoctorSkip
		phase.Summary = "skipped (" + preflight.Reason + ")"
	default:
		phase.Status = DoctorPass
		phase.Summary = fmt.Sprintf("%s free for a restore of about %s",
			formatBytes(preflight.FreeBytes), formatBytes(preflight.RequiredBytes))
	}

	return phase
}

// runFetchPhase performs the restore and times it.
func runFetchPhase(report *RestoreDrillReport, opts RestoreDrillOptions, backupName string) {
	phase := DrillPhase{Name: DrillCheckFetch}
	start := time.Now()

	args := []string{"backup-fetch", opts.TargetDir, backupName}
	if opts.ConfigFile != "" {
		args = append([]string{"--config", opts.ConfigFile}, args...)
	}

	cmd := exec.Command(opts.WalgBinary, args...)
	cmd.Env = os.Environ()

	combined, err := cmd.CombinedOutput()
	elapsed := time.Since(start)

	phase.Elapsed = elapsed.Round(time.Second).String()
	report.FetchSeconds = elapsed.Seconds()

	if err != nil {
		phase.Status = DoctorFail
		phase.Summary = "the restore failed"
		phase.Detail = lastLines(string(combined), 5)
		phase.Remedy = "This is the failure a real restore would hit. Run `wal-g backup-fetch` " +
			"directly for the full output."
		report.add(phase)
		return
	}

	restored, sizeErr := directorySize(opts.TargetDir)
	if sizeErr == nil {
		report.RestoredBytes = restored
		if elapsed > 0 {
			report.ThroughputMBps = float64(restored) / (1024 * 1024) / elapsed.Seconds()
		}
	}

	phase.Status = DoctorPass
	phase.Summary = fmt.Sprintf("%s restored in %s",
		formatBytes(restored), formatDuration(elapsed))
	if report.ThroughputMBps > 0 {
		phase.Detail = fmt.Sprintf("%.0f MiB/s", report.ThroughputMBps)
	}

	report.add(phase)
}

// runReplayPhase starts the restored cluster and waits for it to finish
// recovery, which is the part of an RTO that a fetch alone cannot measure.
func runReplayPhase(report *RestoreDrillReport, opts RestoreDrillOptions) {
	phase := DrillPhase{Name: DrillCheckReplay}

	if !opts.StartPostgres {
		phase.Status = DoctorSkip
		phase.Summary = "skipped (--start-postgres not given)"
		report.add(phase)
		return
	}

	start := time.Now()

	if err := writeRecoveryConfig(opts); err != nil {
		phase.Status = DoctorFail
		phase.Summary = "could not configure the restored cluster for recovery"
		phase.Detail = err.Error()
		report.add(phase)
		return
	}

	startCmd := exec.Command(opts.PGCtlPath, "-D", opts.TargetDir, "-w",
		"-t", strconv.Itoa(int(opts.StartTimeout.Seconds())),
		"-o", fmt.Sprintf("-p %d", opts.Port), "start")
	startCmd.Env = os.Environ()

	combined, err := startCmd.CombinedOutput()
	elapsed := time.Since(start)

	phase.Elapsed = elapsed.Round(time.Second).String()
	report.ReplaySeconds = elapsed.Seconds()

	if err != nil {
		phase.Status = DoctorFail
		phase.Summary = "the restored cluster did not finish recovery"
		phase.Detail = lastLines(string(combined), 5)
		phase.Remedy = "Check the cluster log in " + filepath.Join(opts.TargetDir, "log") +
			". This is the failure a real recovery would hit."
		report.add(phase)
		return
	}

	report.PostgresStarted = true
	phase.Status = DoctorPass
	phase.Summary = fmt.Sprintf("reached consistency in %s (port %d)",
		formatDuration(elapsed), opts.Port)

	if point, err := queryRecoveryPoint(opts); err == nil {
		phase.Detail = point
	} else {
		// The cluster came up, which is the thing being measured. Not being able
		// to log in to ask it questions is worth mentioning, not failing on:
		// the restored pg_hba.conf is whatever the backup happened to contain.
		phase.Detail = "could not query the recovered cluster: " + err.Error()
	}

	report.add(phase)

	stopDrillCluster(opts)
}

// writeRecoveryConfig puts the restored cluster into recovery, in whichever way
// its version expects. PostgreSQL 12 replaced recovery.conf with a signal file
// and ordinary settings, and a drill has to restore backups from both eras.
func writeRecoveryConfig(opts RestoreDrillOptions) error {
	versionRaw, err := os.ReadFile(filepath.Join(opts.TargetDir, "PG_VERSION"))
	if err != nil {
		return fmt.Errorf("could not read PG_VERSION from the restored cluster: %w", err)
	}

	version, err := strconv.Atoi(strings.TrimSpace(strings.Split(strings.TrimSpace(string(versionRaw)), ".")[0]))
	if err != nil {
		return fmt.Errorf("could not parse PG_VERSION %q: %w", strings.TrimSpace(string(versionRaw)), err)
	}

	restoreCommand := fmt.Sprintf("%s wal-fetch %%f %%p", quoteForConf(opts.WalgBinary))
	if opts.ConfigFile != "" {
		restoreCommand = fmt.Sprintf("%s --config %s wal-fetch %%f %%p",
			quoteForConf(opts.WalgBinary), quoteForConf(opts.ConfigFile))
	}

	settings := fmt.Sprintf("restore_command = '%s'\n", restoreCommand)

	if version >= 12 {
		signal := filepath.Join(opts.TargetDir, "recovery.signal")
		if err := os.WriteFile(signal, nil, 0o600); err != nil {
			return fmt.Errorf("could not write recovery.signal: %w", err)
		}

		conf, err := os.OpenFile(filepath.Join(opts.TargetDir, "postgresql.conf"),
			os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
		if err != nil {
			return fmt.Errorf("could not open postgresql.conf: %w", err)
		}
		defer conf.Close()

		if _, err := conf.WriteString("\n# added by wal-g restore-test\n" + settings); err != nil {
			return fmt.Errorf("could not append recovery settings: %w", err)
		}

		return nil
	}

	if err := os.WriteFile(filepath.Join(opts.TargetDir, "recovery.conf"),
		[]byte(settings), 0o600); err != nil {
		return fmt.Errorf("could not write recovery.conf: %w", err)
	}

	return nil
}

// quoteForConf escapes a value for a single-quoted postgresql.conf string.
func quoteForConf(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

// queryRecoveryPoint asks the recovered cluster how far it actually replayed,
// which is the only measured answer to "what would we have lost".
func queryRecoveryPoint(opts RestoreDrillOptions) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	dsn := fmt.Sprintf("host=127.0.0.1 port=%d dbname=postgres connect_timeout=10", opts.Port)

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return "", err
	}
	defer conn.Close(ctx)

	var inRecovery bool
	var replayTime *time.Time

	err = conn.QueryRow(ctx,
		"SELECT pg_is_in_recovery(), pg_last_xact_replay_timestamp()").Scan(&inRecovery, &replayTime)
	if err != nil {
		return "", err
	}

	if replayTime == nil {
		return "cluster is up; it reports no replayed transaction timestamp", nil
	}

	return fmt.Sprintf("replayed transactions up to %s", formatPITRTime(*replayTime)), nil
}

func stopDrillCluster(opts RestoreDrillOptions) {
	stop := exec.Command(opts.PGCtlPath, "-D", opts.TargetDir, "-m", "immediate", "-w", "stop")
	stop.Env = os.Environ()

	if out, err := stop.CombinedOutput(); err != nil {
		tracelog.WarningLogger.Printf(
			"Failed to stop the drill cluster on port %d: %v\n%s\n"+
				"Stop it by hand before removing %s.\n",
			opts.Port, err, lastLines(string(out), 3), opts.TargetDir)
	}
}

// rtoPhase judges the elapsed recovery against the budget, and is explicit
// about which phases that elapsed time actually covers.
func rtoPhase(report *RestoreDrillReport, opts RestoreDrillOptions, elapsed time.Duration) DrillPhase {
	phase := DrillPhase{Name: DrillCheckRTO, Elapsed: elapsed.Round(time.Second).String()}
	report.TotalSeconds = elapsed.Seconds()

	scope := "fetch only"
	if report.PostgresStarted {
		scope = "fetch and replay"
	}

	if opts.RTO <= 0 {
		phase.Status = DoctorSkip
		phase.Summary = fmt.Sprintf("recovery took %s (%s), no RTO declared", formatDuration(elapsed), scope)
		phase.Remedy = "Pass --rto, or set WALG_RTO, to judge the drill against a budget."
		return phase
	}

	if elapsed > opts.RTO {
		phase.Status = DoctorFail
		phase.Summary = fmt.Sprintf("recovery took %s, over the %s budget (%s)",
			formatDuration(elapsed), formatDuration(opts.RTO), scope)
		phase.Remedy = "Restore is slower than the objective allows. Consider more restore " +
			"concurrency, faster storage, or a smaller delta chain."
		return phase
	}

	phase.Status = DoctorPass
	phase.Summary = fmt.Sprintf("recovery took %s of the %s budget (%s)",
		formatDuration(elapsed), formatDuration(opts.RTO), scope)

	if !report.PostgresStarted {
		phase.Remedy = "Replay time is NOT included. Pass --start-postgres to measure it."
	}

	return phase
}

// rpoPhase reports how much data the drill shows would be lost. Without a
// started cluster this is the reachable recovery point rather than a replayed
// one, which is the same measure retention-validate uses.
func rpoPhase(rootFolder storage.Folder, opts RestoreDrillOptions) DrillPhase {
	phase := DrillPhase{Name: DrillCheckRPO}

	if opts.RPO <= 0 {
		phase.Status = DoctorSkip
		phase.Summary = "skipped (no RPO declared)"
		phase.Remedy = "Pass --rpo, or set WALG_RPO, to judge the reachable recovery point."
		return phase
	}

	inputs, err := LoadPITRInputs(rootFolder)
	if err != nil {
		phase.Status = DoctorWarn
		phase.Summary = "could not determine the reachable recovery point"
		phase.Detail = err.Error()
		return phase
	}

	window := inputs.Compute()
	if window.Empty() {
		phase.Status = DoctorFail
		phase.Summary = "nothing is restorable"
		return phase
	}

	exposure := utility.TimeNowCrossPlatformUTC().Sub(window.LatestRestorableTime)

	phase.Detail = fmt.Sprintf("newest restorable point is %s",
		formatPITRTime(window.LatestRestorableTime))

	if exposure > opts.RPO {
		phase.Status = DoctorFail
		phase.Summary = fmt.Sprintf("%s of data at risk, RPO allows %s",
			formatDuration(exposure), formatDuration(opts.RPO))
		phase.Remedy = "WAL archiving is behind or has stopped. Check archive_command and " +
			"`wal-g wal-verify integrity`."
		return phase
	}

	phase.Status = DoctorPass
	phase.Summary = fmt.Sprintf("restorable to within %s, inside the %s RPO",
		formatDuration(exposure), formatDuration(opts.RPO))

	return phase
}

// cleanupDrill removes what the drill created. A drill that leaves a full copy
// of the cluster behind on every run fills the volume it was measuring.
func cleanupDrill(report *RestoreDrillReport, opts RestoreDrillOptions, created bool) {
	if opts.Keep {
		tracelog.InfoLogger.Printf("Leaving the restored cluster in %s (--keep).\n", opts.TargetDir)
		return
	}

	if report.PostgresStarted {
		// Already stopped on the success path; harmless if it is not running.
		stopDrillCluster(opts)
	}

	var err error
	if created {
		err = os.RemoveAll(opts.TargetDir)
	} else {
		err = emptyDirectory(opts.TargetDir)
	}

	if err != nil {
		tracelog.WarningLogger.Printf("Failed to clean up %s: %v\n", opts.TargetDir, err)
		return
	}

	report.ScratchDirRemoved = true
}

func emptyDirectory(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return err
		}
	}

	return nil
}

func directorySize(dir string) (int64, error) {
	var total int64

	err := filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})

	return total, err
}

// lastLines keeps a failure's output to the part that usually says why.
func lastLines(output string, count int) string {
	lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
	if len(lines) > count {
		lines = lines[len(lines)-count:]
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func finishDrill(report *RestoreDrillReport, opts RestoreDrillOptions,
	started time.Time, cleanup func(), output io.Writer) int {
	if report.TotalSeconds == 0 {
		report.TotalSeconds = time.Since(started).Seconds()
	}

	if cleanup != nil {
		cleanup()
	}

	report.ScratchDirPresent = directoryHasContent(opts.TargetDir)
	report.Pass = report.Failed == 0

	if err := WriteRestoreDrillReport(report, opts.Format, output); err != nil {
		tracelog.ErrorLogger.Printf("Failed to write the report: %v\n", err)
		return 1
	}

	if report.Pass {
		return 0
	}

	return 1
}

// WriteRestoreDrillReport renders a drill report.
func WriteRestoreDrillReport(report *RestoreDrillReport, format string, output io.Writer) error {
	if format == "json" {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}
		_, err = output.Write(append(data, '\n'))
		return err
	}

	writeRestoreDrillText(report, output)

	return nil
}

func writeRestoreDrillText(report *RestoreDrillReport, output io.Writer) {
	fmt.Fprintf(output, "wal-g restore-test\n\n")

	if report.BackupName != "" {
		fmt.Fprintf(output, "Restoring  %s into %s\n\n", report.BackupName, report.ScratchDir)
	}

	for _, phase := range report.Phases {
		fmt.Fprintf(output, "%-6s %-12s %s\n", statusLabel(phase.Status), phase.Name, phase.Summary)
		if phase.Detail != "" {
			fmt.Fprintf(output, "       %s\n", phase.Detail)
		}
		if phase.Remedy != "" {
			fmt.Fprintf(output, "       -> %s\n", phase.Remedy)
		}
	}

	fmt.Fprintf(output, "\n%d passed, %d warned, %d failed, %d skipped\n",
		report.Passed, report.Warned, report.Failed, report.Skipped)

	if report.Pass {
		fmt.Fprintf(output, "Drill passed.")
	} else {
		fmt.Fprintf(output, "Drill FAILED: %d phase(s) failed.", report.Failed)
	}

	switch {
	case report.ScratchDirRemoved:
		fmt.Fprintf(output, " Scratch directory removed.\n")
	case report.ScratchDirPresent:
		fmt.Fprintf(output, " Restored cluster left in %s.\n", report.ScratchDir)
	default:
		fmt.Fprintln(output)
	}
}

// directoryHasContent reports whether anything is actually at path, so the
// report can describe the disk rather than the intention.
func directoryHasContent(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}

	return len(entries) > 0
}
