// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
	"github.com/wal-g/tracelog"

	"github.com/lateos-ai/wal-g/internal"
	conf "github.com/lateos-ai/wal-g/internal/config"
	"github.com/lateos-ai/wal-g/internal/fsutil"
	"github.com/lateos-ai/wal-g/pkg/storages/storage"
	"github.com/lateos-ai/wal-g/utility"
)

// DoctorStatus is the outcome of a single preflight check.
type DoctorStatus string

const (
	// DoctorPass means the check succeeded.
	DoctorPass DoctorStatus = "pass"
	// DoctorWarn means the check succeeded but found something worth attention.
	// Warnings do not affect the exit code.
	DoctorWarn DoctorStatus = "warn"
	// DoctorFail means backup or restore would not work as configured.
	DoctorFail DoctorStatus = "fail"
	// DoctorSkip means the check did not run.
	DoctorSkip DoctorStatus = "skip"
)

// Check names, stable enough to be matched on by scripts.
const (
	CheckConfig       = "config"
	CheckStorage      = "storage"
	CheckEncryption   = "encryption"
	CheckPostgres     = "postgres"
	CheckArchiving    = "archiving"
	CheckBackups      = "backups"
	CheckRestoreSpace = "restore-space"
)

// canaryPayload is written and read back by the storage and encryption checks.
// It is small on purpose: these checks run on production hosts.
const canaryPayload = "wal-g doctor canary"

// DoctorCheck is the result of one preflight check.
type DoctorCheck struct {
	Name    string       `json:"name"`
	Status  DoctorStatus `json:"status"`
	Summary string       `json:"summary"`
	Detail  string       `json:"detail,omitempty"`
	// Remedy states what to do about a warn or fail. A check that can fail
	// should always be able to say how to fix it.
	Remedy  string `json:"remedy,omitempty"`
	Elapsed string `json:"elapsed"`
}

// DoctorResult is the full preflight report.
type DoctorResult struct {
	Pass    bool          `json:"pass"`
	Checks  []DoctorCheck `json:"checks"`
	Passed  int           `json:"passed"`
	Warned  int           `json:"warned"`
	Failed  int           `json:"failed"`
	Skipped int           `json:"skipped"`
	Elapsed string        `json:"elapsed"`
}

// DoctorOptions carries the tunables for a doctor run.
type DoctorOptions struct {
	// Format is "text" or "json".
	Format string
	// SkipPG omits the checks that need a live PostgreSQL connection, for hosts
	// that only restore or only hold credentials.
	SkipPG bool
	// DataDir overrides PGDATA when sizing the restore-space check.
	DataDir string
	// SpaceMargin is the multiple of the latest backup's uncompressed size that
	// must be free for a restore to be considered safe.
	SpaceMargin float64
	// StaleAfter is the age past which the latest backup is reported as stale.
	StaleAfter time.Duration
}

// DefaultSpaceMargin leaves headroom for WAL replayed during recovery and for
// the cluster to accept writes once it is up.
const DefaultSpaceMargin = 1.2

// DefaultStaleAfter is just over a day, so a daily backup schedule that slips by
// an hour or two does not read as a failure.
const DefaultStaleAfter = 26 * time.Hour

// HandleDoctor runs every preflight check and reports the results. It returns the
// process exit code: 0 when nothing failed, 1 otherwise.
func HandleDoctor(rootFolder storage.Folder, opts DoctorOptions, output io.Writer) int {
	if opts.SpaceMargin <= 0 {
		opts.SpaceMargin = DefaultSpaceMargin
	}
	if opts.StaleAfter <= 0 {
		opts.StaleAfter = DefaultStaleAfter
	}

	startTime := time.Now()
	result := &DoctorResult{}

	result.add(checkConfig())
	result.add(checkStorage(rootFolder))
	result.add(checkEncryption())

	pgChecks := runPostgresChecks(opts)
	for _, check := range pgChecks {
		result.add(check)
	}

	latest, backupsCheck := checkBackups(rootFolder, opts)
	result.add(backupsCheck)
	result.add(checkRestoreSpace(rootFolder, latest, opts))

	result.Pass = result.Failed == 0
	result.Elapsed = time.Since(startTime).Round(time.Millisecond).String()

	writeDoctorOutput(result, opts.Format, output)

	if result.Pass {
		return 0
	}
	return 1
}

func (r *DoctorResult) add(check DoctorCheck) {
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

// timed stamps a check with how long it took, so a slow storage backend is
// visible rather than just felt.
func timed(start time.Time, check DoctorCheck) DoctorCheck {
	check.Elapsed = time.Since(start).Round(time.Millisecond).String()
	return check
}

// checkConfig reports whether the settings needed to reach storage resolved, and
// where each value came from. "Which of my three config sources won?" is
// otherwise guesswork across 180-odd settings.
func checkConfig() DoctorCheck {
	start := time.Now()
	check := DoctorCheck{Name: CheckConfig}

	var missing []string
	var resolved []string

	for setting, required := range conf.RequiredSettings {
		if !required {
			continue
		}
		value, ok := conf.GetSetting(setting)
		if !ok || value == "" {
			missing = append(missing, setting)
			continue
		}
		resolved = append(resolved, fmt.Sprintf("%s (from %s)", setting, settingSource(setting)))
	}

	if len(missing) > 0 {
		check.Status = DoctorFail
		check.Summary = fmt.Sprintf("%d required setting(s) unresolved", len(missing))
		check.Detail = "missing: " + strings.Join(missing, ", ")
		check.Remedy = "Set the listed variables in the environment or in the config file " +
			"(--config, $WALG_CONFIG_PATH, or ~/.walg.json)."
		return timed(start, check)
	}

	check.Status = DoctorPass
	check.Summary = fmt.Sprintf("%d required setting(s) resolved", len(resolved))
	if len(resolved) > 0 {
		check.Detail = strings.Join(resolved, "; ")
	}
	return timed(start, check)
}

// settingSource reports which configuration layer supplied a setting's value.
func settingSource(setting string) string {
	if os.Getenv(setting) != "" {
		return "environment"
	}
	if viper.InConfig(setting) || viper.InConfig(strings.ToLower(setting)) {
		return "config file"
	}
	return "default or flag"
}

// checkStorage proves the credentials in hand can list, write, and delete, rather
// than only that the bucket name parses. Write access matters even on a host that
// only restores: wal-push and backup-push both fail late without it.
func checkStorage(rootFolder storage.Folder) DoctorCheck {
	start := time.Now()
	check := DoctorCheck{Name: CheckStorage}

	if _, _, err := rootFolder.ListFolder(); err != nil {
		check.Status = DoctorFail
		check.Summary = "cannot list storage"
		check.Detail = err.Error()
		check.Remedy = "Verify the storage prefix and that the credentials grant list access to it."
		return timed(start, check)
	}

	canaryName := fmt.Sprintf("wal-g-doctor-canary-%d", time.Now().UnixNano())

	if err := rootFolder.PutObject(canaryName, strings.NewReader(canaryPayload)); err != nil {
		check.Status = DoctorFail
		check.Summary = "cannot write to storage"
		check.Detail = err.Error()
		check.Remedy = "Grant write access to the storage prefix; backup-push and wal-push both need it."
		return timed(start, check)
	}

	readBack, readErr := readStorageObject(rootFolder, canaryName)

	// Always attempt cleanup, even if the read failed, so a probe object is not
	// left behind in the user's bucket.
	deleteErr := rootFolder.DeleteObjects([]storage.Object{
		storage.NewLocalObject(canaryName, time.Time{}, int64(len(canaryPayload))),
	})

	switch {
	case readErr != nil:
		check.Status = DoctorFail
		check.Summary = "wrote to storage but could not read back"
		check.Detail = readErr.Error()
		check.Remedy = "Grant read access to the storage prefix."
		return timed(start, check)
	case readBack != canaryPayload:
		check.Status = DoctorFail
		check.Summary = "storage returned different content than was written"
		check.Detail = fmt.Sprintf("wrote %q, read back %q", canaryPayload, readBack)
		check.Remedy = "Check for a proxy, cache, or bucket policy rewriting object content."
		return timed(start, check)
	case deleteErr != nil:
		check.Status = DoctorWarn
		check.Summary = "read and write OK, but delete failed"
		check.Detail = fmt.Sprintf("probe object %s could not be removed: %v", canaryName, deleteErr)
		check.Remedy = "Grant delete access, otherwise retention (`delete`) cannot reclaim space. " +
			"Remove the probe object manually."
		return timed(start, check)
	}

	check.Status = DoctorPass
	check.Summary = "list, write, read, and delete all OK"
	return timed(start, check)
}

func readStorageObject(folder storage.Folder, name string) (string, error) {
	reader, err := folder.ReadObject(name)
	if err != nil {
		return "", err
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// checkEncryption round-trips a canary through the configured crypter. A key that
// encrypts but cannot decrypt produces backups that are unreadable exactly when
// they are needed, and nothing else in the normal backup path notices.
func checkEncryption() DoctorCheck {
	start := time.Now()
	check := DoctorCheck{Name: CheckEncryption}

	crypter, err := internal.ConfigureCrypterForSpecificConfig(viper.GetViper())
	if err != nil {
		check.Status = DoctorFail
		check.Summary = "crypter is misconfigured"
		check.Detail = err.Error()
		check.Remedy = "Fix the encryption settings; backups cannot be written or read until the crypter loads."
		return timed(start, check)
	}

	if crypter == nil {
		check.Status = DoctorWarn
		check.Summary = "no encryption configured"
		check.Detail = "backups are stored unencrypted"
		check.Remedy = "Configure WALG_PGP_KEY, WALG_LIBSODIUM_KEY, or a KMS key if the storage " +
			"is not already encrypted at rest."
		return timed(start, check)
	}

	var encrypted bytes.Buffer
	writer, err := crypter.Encrypt(&encrypted)
	if err != nil {
		check.Status = DoctorFail
		check.Summary = fmt.Sprintf("%s crypter cannot encrypt", crypter.Name())
		check.Detail = err.Error()
		check.Remedy = "Check the encryption key material is readable by the user running wal-g."
		return timed(start, check)
	}
	if _, err := io.WriteString(writer, canaryPayload); err != nil {
		check.Status = DoctorFail
		check.Summary = fmt.Sprintf("%s crypter cannot encrypt", crypter.Name())
		check.Detail = err.Error()
		check.Remedy = "Check the encryption key material is readable by the user running wal-g."
		return timed(start, check)
	}
	if err := writer.Close(); err != nil {
		check.Status = DoctorFail
		check.Summary = fmt.Sprintf("%s crypter cannot finalize encryption", crypter.Name())
		check.Detail = err.Error()
		check.Remedy = "Check the encryption key material is readable by the user running wal-g."
		return timed(start, check)
	}

	reader, err := crypter.Decrypt(&encrypted)
	if err != nil {
		check.Status = DoctorFail
		check.Summary = fmt.Sprintf("%s crypter cannot decrypt what it just encrypted", crypter.Name())
		check.Detail = err.Error()
		check.Remedy = "The private key or passphrase needed for restore is missing on this host. " +
			"Backups written here would not be restorable here."
		return timed(start, check)
	}

	decrypted, err := io.ReadAll(reader)
	if err != nil {
		check.Status = DoctorFail
		check.Summary = fmt.Sprintf("%s crypter cannot decrypt what it just encrypted", crypter.Name())
		check.Detail = err.Error()
		check.Remedy = "The private key or passphrase needed for restore is missing or wrong on this host."
		return timed(start, check)
	}

	if string(decrypted) != canaryPayload {
		check.Status = DoctorFail
		check.Summary = fmt.Sprintf("%s crypter round-trip corrupted the payload", crypter.Name())
		check.Detail = fmt.Sprintf("encrypted %q, decrypted %q", canaryPayload, string(decrypted))
		check.Remedy = "Do not rely on backups from this host until the crypter is fixed."
		return timed(start, check)
	}

	check.Status = DoctorPass
	check.Summary = fmt.Sprintf("%s encrypt/decrypt round-trip OK", crypter.Name())
	return timed(start, check)
}

// runPostgresChecks connects once and derives both the connectivity and the WAL
// archiving checks from that connection.
func runPostgresChecks(opts DoctorOptions) []DoctorCheck {
	start := time.Now()

	if opts.SkipPG {
		skipped := DoctorCheck{
			Name:    CheckPostgres,
			Status:  DoctorSkip,
			Summary: "skipped (--skip-pg)",
			Elapsed: "0s",
		}
		archiving := skipped
		archiving.Name = CheckArchiving
		return []DoctorCheck{skipped, archiving}
	}

	pgCheck := DoctorCheck{Name: CheckPostgres}
	archivingCheck := DoctorCheck{Name: CheckArchiving}

	ctx := context.Background()

	conn, err := Connect(ctx)
	if err != nil {
		pgCheck.Status = DoctorFail
		pgCheck.Summary = "cannot connect to PostgreSQL"
		pgCheck.Detail = err.Error()
		pgCheck.Remedy = "Check PGHOST/PGPORT/PGUSER/PGPASSWORD and that the server is running. " +
			"Pass --skip-pg if this host only restores backups."

		archivingCheck.Status = DoctorSkip
		archivingCheck.Summary = "skipped (no PostgreSQL connection)"
		archivingCheck.Elapsed = "0s"

		return []DoctorCheck{timed(start, pgCheck), archivingCheck}
	}
	defer func() {
		if closeErr := conn.Close(ctx); closeErr != nil {
			tracelog.DebugLogger.Printf("doctor: failed to close PostgreSQL connection: %v", closeErr)
		}
	}()

	queryRunner, err := NewPgQueryRunner(ctx, conn)
	if err != nil {
		pgCheck.Status = DoctorFail
		pgCheck.Summary = "connected, but could not query server version"
		pgCheck.Detail = err.Error()
		pgCheck.Remedy = "Ensure the connecting role can read pg_settings."

		archivingCheck.Status = DoctorSkip
		archivingCheck.Summary = "skipped (server version unknown)"
		archivingCheck.Elapsed = "0s"

		return []DoctorCheck{timed(start, pgCheck), archivingCheck}
	}

	standby, err := queryRunner.IsStandby(ctx)
	if err != nil {
		pgCheck.Status = DoctorWarn
		pgCheck.Summary = fmt.Sprintf("connected to PostgreSQL %s", formatPgVersion(queryRunner.Version))
		pgCheck.Detail = fmt.Sprintf("could not determine recovery state: %v", err)
	} else {
		pgCheck.Status = DoctorPass
		role := "primary"
		if standby {
			role = "standby"
		}
		pgCheck.Summary = fmt.Sprintf("connected to PostgreSQL %s (%s)",
			formatPgVersion(queryRunner.Version), role)
	}

	pgCheck = timed(start, pgCheck)

	return []DoctorCheck{pgCheck, checkArchiving(ctx, queryRunner, standby, archivingCheck)}
}

// checkArchiving verifies WAL archiving is switched on and pointed at wal-g.
// Without it, backups exist but point-in-time recovery does not.
func checkArchiving(
	ctx context.Context,
	queryRunner *PgQueryRunner,
	standby bool,
	check DoctorCheck,
) DoctorCheck {
	start := time.Now()

	if standby {
		check.Status = DoctorSkip
		check.Summary = "skipped (server is a standby, which does not archive)"
		return timed(start, check)
	}

	archiveMode, err := queryRunner.GetArchiveMode(ctx)
	if err != nil {
		check.Status = DoctorWarn
		check.Summary = "could not read archive_mode"
		check.Detail = err.Error()
		check.Remedy = "Ensure the connecting role can read pg_settings."
		return timed(start, check)
	}

	if archiveMode != "on" && archiveMode != "always" {
		check.Status = DoctorFail
		check.Summary = fmt.Sprintf("archive_mode is %q", archiveMode)
		check.Detail = "WAL is not being archived, so point-in-time recovery is not possible " +
			"and base backups cannot be replayed to a consistent state"
		check.Remedy = "Set archive_mode = on in postgresql.conf and restart PostgreSQL."
		return timed(start, check)
	}

	archiveCommand, err := queryRunner.GetArchiveCommand(ctx)
	if err != nil {
		check.Status = DoctorWarn
		check.Summary = "could not read archive_command"
		check.Detail = err.Error()
		check.Remedy = "Ensure the connecting role can read pg_settings."
		return timed(start, check)
	}

	if len(archiveCommand) == 0 || archiveCommand == "(disabled)" {
		check.Status = DoctorFail
		check.Summary = "archive_command is not set"
		check.Detail = "archive_mode is enabled but PostgreSQL has nothing to run, " +
			"so WAL accumulates in pg_wal instead of reaching storage"
		check.Remedy = `Set archive_command = 'wal-g wal-push %p' in postgresql.conf and reload.`
		return timed(start, check)
	}

	if !strings.Contains(archiveCommand, "wal-push") {
		check.Status = DoctorWarn
		check.Summary = "archive_command does not call wal-g wal-push"
		check.Detail = fmt.Sprintf("archive_command = %q", archiveCommand)
		check.Remedy = "If another tool archives WAL, wal-g's own WAL commands (wal-fetch, " +
			"wal-verify, PITR) will not see those segments."
		return timed(start, check)
	}

	check.Status = DoctorPass
	check.Summary = "archive_mode on and archive_command calls wal-push"
	check.Detail = fmt.Sprintf("archive_command = %q", archiveCommand)
	return timed(start, check)
}

func formatPgVersion(version int) string {
	if version == 0 {
		return "(unknown version)"
	}
	major := version / 10000
	minor := version % 10000 / 100
	patch := version % 100
	if major >= 10 {
		return fmt.Sprintf("%d.%d", major, patch)
	}
	return fmt.Sprintf("%d.%d.%d", major, minor, patch)
}

// checkBackups reports whether a restorable backup exists and how old it is. It
// returns the newest backup so the restore-space check can size itself.
func checkBackups(rootFolder storage.Folder, opts DoctorOptions) (*internal.BackupTime, DoctorCheck) {
	start := time.Now()
	check := DoctorCheck{Name: CheckBackups}

	baseBackupFolder := rootFolder.GetSubFolder(utility.BaseBackupPath)

	backups, err := internal.GetBackups(baseBackupFolder)
	if err != nil || len(backups) == 0 {
		check.Status = DoctorFail
		check.Summary = "no backups found in storage"
		if err != nil {
			check.Detail = err.Error()
		}
		check.Remedy = "Run `wal-g backup-push` - there is currently nothing to restore from."
		return nil, timed(start, check)
	}

	latest := backups[len(backups)-1]
	age := time.Since(latest.Time)

	if age > opts.StaleAfter {
		check.Status = DoctorWarn
		check.Summary = fmt.Sprintf("latest backup is %s old", formatDuration(age))
		check.Detail = fmt.Sprintf("%d backup(s) in storage, newest is %s from %s",
			len(backups), latest.BackupName, latest.Time.Format(time.RFC3339))
		check.Remedy = fmt.Sprintf(
			"Backups older than %s mean a longer replay on restore and a larger data-loss window. "+
				"Check the backup schedule.", formatDuration(opts.StaleAfter))
		return &latest, timed(start, check)
	}

	check.Status = DoctorPass
	check.Summary = fmt.Sprintf("%d backup(s), newest is %s old", len(backups), formatDuration(age))
	check.Detail = fmt.Sprintf("newest is %s from %s", latest.BackupName, latest.Time.Format(time.RFC3339))
	return &latest, timed(start, check)
}

// checkRestoreSpace compares free space against the size the newest backup would
// occupy once unpacked. A restore that will run out of disk is knowable in
// milliseconds; discovering it hours in is the expensive way to find out.
func checkRestoreSpace(
	rootFolder storage.Folder,
	latest *internal.BackupTime,
	opts DoctorOptions,
) DoctorCheck {
	start := time.Now()
	check := DoctorCheck{Name: CheckRestoreSpace}

	if latest == nil {
		check.Status = DoctorSkip
		check.Summary = "skipped (no backup to size against)"
		return timed(start, check)
	}

	dataDir := opts.DataDir
	if dataDir == "" {
		dataDir, _ = conf.GetSetting(conf.PgDataSetting)
	}
	if dataDir == "" {
		check.Status = DoctorSkip
		check.Summary = "skipped (no data directory known)"
		check.Remedy = "Set PGDATA or pass --data-dir to size a restore against this host's free space."
		return timed(start, check)
	}

	if _, err := os.Stat(dataDir); err != nil {
		check.Status = DoctorSkip
		check.Summary = "skipped (data directory not present on this host)"
		check.Detail = err.Error()
		return timed(start, check)
	}

	backup, err := NewBackup(rootFolder.GetSubFolder(utility.BaseBackupPath), latest.BackupName)
	if err != nil {
		check.Status = DoctorWarn
		check.Summary = "could not open the latest backup to size a restore"
		check.Detail = err.Error()
		return timed(start, check)
	}

	sentinel, err := backup.GetSentinel()
	if err != nil {
		check.Status = DoctorWarn
		check.Summary = "could not read the latest backup's sentinel to size a restore"
		check.Detail = err.Error()
		return timed(start, check)
	}

	required := sentinel.UncompressedSize
	if required <= 0 {
		check.Status = DoctorSkip
		check.Summary = "skipped (backup does not record an uncompressed size)"
		check.Detail = "backups taken before size recording cannot be sized without downloading them"
		return timed(start, check)
	}

	space, err := fsutil.GetDiskSpace(dataDir)
	if err != nil {
		check.Status = DoctorWarn
		check.Summary = "could not determine free disk space"
		check.Detail = err.Error()
		return timed(start, check)
	}

	needed := int64(float64(required) * opts.SpaceMargin)

	check.Detail = fmt.Sprintf("%s: %s free, restore of %s needs ~%s (%.0f%% margin)",
		dataDir,
		formatBytes(int64(space.FreeBytes)),
		latest.BackupName,
		formatBytes(needed),
		(opts.SpaceMargin-1)*100)

	if int64(space.FreeBytes) < needed {
		check.Status = DoctorFail
		check.Summary = fmt.Sprintf("not enough free space to restore (%s free, ~%s needed)",
			formatBytes(int64(space.FreeBytes)),
			formatBytes(needed))
		check.Remedy = "Free space on the data directory's filesystem, or restore to a larger volume " +
			"with `backup-fetch <target-dir>`."
		return timed(start, check)
	}

	check.Status = DoctorPass
	check.Summary = fmt.Sprintf("%s free, enough to restore the latest backup",
		formatBytes(int64(space.FreeBytes)))
	return timed(start, check)
}

// formatBytes renders a byte count for humans reading an incident-time report.
func formatBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}

	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

func formatDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dd%dh", int(d.Hours())/24, int(d.Hours())%24)
	}
}

func writeDoctorOutput(result *DoctorResult, format string, output io.Writer) {
	if format == "json" {
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			fmt.Fprintf(output, `{"error":%q}`, err.Error())
			return
		}
		if _, err := output.Write(append(data, '\n')); err != nil {
			tracelog.ErrorLogger.Printf("failed to write JSON output: %v", err)
		}
		return
	}

	writeDoctorTextOutput(result, output)
}

func writeDoctorTextOutput(result *DoctorResult, output io.Writer) {
	fmt.Fprintf(output, "wal-g doctor\n\n")

	for _, check := range result.Checks {
		fmt.Fprintf(output, "%-6s %-14s %s\n", statusLabel(check.Status), check.Name, check.Summary)
		if check.Detail != "" {
			fmt.Fprintf(output, "       %s\n", check.Detail)
		}
		if check.Remedy != "" {
			fmt.Fprintf(output, "       -> %s\n", check.Remedy)
		}
	}

	fmt.Fprintf(output, "\n%d passed, %d warned, %d failed, %d skipped in %s\n",
		result.Passed, result.Warned, result.Failed, result.Skipped, result.Elapsed)

	if result.Pass {
		if result.Warned > 0 {
			fmt.Fprintf(output, "Ready, with warnings noted above.\n")
		} else {
			fmt.Fprintf(output, "Ready.\n")
		}
		return
	}

	fmt.Fprintf(output, "NOT ready: %d check(s) failed.\n", result.Failed)
}

func statusLabel(status DoctorStatus) string {
	switch status {
	case DoctorPass:
		return "[ OK ]"
	case DoctorWarn:
		return "[WARN]"
	case DoctorFail:
		return "[FAIL]"
	default:
		return "[SKIP]"
	}
}
