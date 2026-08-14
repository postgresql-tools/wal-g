// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/wal-g/tracelog"

	"github.com/lateos-ai/wal-g/pkg/neon"
	"github.com/lateos-ai/wal-g/pkg/storages/storage"
	"github.com/lateos-ai/wal-g/utility"
)

// Neon drill phase names, stable enough to be matched on by scripts.
const (
	DrillCheckNeonAuth    = "neon-auth"
	DrillCheckNeonBranch  = "neon-branch"
	DrillCheckNeonLoad    = "neon-load"
	DrillCheckNeonCleanup = "neon-cleanup"
)

// Defaults for the Neon side of the drill.
const (
	DefaultNeonRole     = "neondb_owner"
	DefaultNeonDatabase = "neondb"

	// MaintenanceDB is the database every cluster has and almost nobody keeps
	// data in. It is the connection target for asking the restored cluster what
	// it holds, and the last-resort source when it holds nothing else.
	MaintenanceDB = "postgres"
)

// NeonClient is the slice of the Neon control plane a drill uses. It is an
// interface so the drill can be tested without a Neon account.
type NeonClient interface {
	Ping(ctx context.Context) error
	CreateBranch(ctx context.Context, name, parentID string) (neon.Branch, error)
	DeleteBranch(ctx context.Context, branch neon.Branch) error
	ConnectionURI(ctx context.Context, branchID, databaseName, roleName string) (string, error)
}

// NeonDrillOptions carries the tunables for one Neon drill.
//
// It embeds RestoreDrillOptions because the first half of a Neon drill is an
// ordinary restore drill: the physical restore is still the thing under test.
type NeonDrillOptions struct {
	RestoreDrillOptions

	// Client talks to the Neon control plane. When nil, one is built from the
	// credential fields below.
	Client NeonClient

	// APIKey and APIEndpoint configure the control plane connection. APIKey is
	// secret and is never written to the report.
	APIKey      string
	APIEndpoint string

	// ProjectID is the Neon project the branch is created in.
	ProjectID string
	// ParentBranch is the branch to fork from. Empty forks the default branch.
	ParentBranch string
	// BranchName overrides the generated branch name. It must still carry the
	// drill prefix, or cleanup will refuse to remove it.
	BranchName string
	// KeepBranch leaves the branch in place for inspection.
	KeepBranch bool

	// Role and Database identify what the dump is loaded into on the branch.
	Role     string
	Database string

	// SourceDatabase is the database dumped out of the restored cluster. Empty
	// means ask the cluster: see resolveSourceDatabase.
	SourceDatabase string

	// PgDumpPath and PsqlPath are the client binaries used for the transfer.
	PgDumpPath string
	PsqlPath   string
}

// NeonDrillReport is the result of a Neon drill.
//
// It embeds RestoreDrillReport so the restore half reports identically to a
// plain drill, and a consumer of the JSON sees the same field names.
type NeonDrillReport struct {
	RestoreDrillReport

	NeonProjectID  string `json:"neon_project_id,omitempty"`
	NeonBranchID   string `json:"neon_branch_id,omitempty"`
	NeonBranchName string `json:"neon_branch_name,omitempty"`

	// SourceDatabase is the database actually dumped, which is worth recording
	// because the drill usually works it out rather than being told.
	SourceDatabase string `json:"source_database,omitempty"`

	// LoadSeconds is the logical dump-and-load, reported separately because it
	// is not part of the recovery being budgeted. See rtoPhase.
	LoadSeconds float64 `json:"neon_load_seconds,omitempty"`
	LoadedBytes int64   `json:"neon_loaded_bytes,omitempty"`

	// BranchRetained says whether a branch is still there at the end, and is
	// what an operator greps for when the Neon bill grows.
	BranchRetained bool `json:"neon_branch_retained"`
}

// HandleNeonDrill restores a backup, loads the result into a fresh Neon branch,
// and reports on it. It returns the process exit code: 0 when nothing failed,
// 1 otherwise.
//
// A wal-g backup cannot be restored into Neon directly - Neon has no data
// directory to write and no replication protocol to stream into - so the drill
// is two-stage: a real physical restore into a scratch directory, then a
// logical dump of the recovered cluster into the branch. The physical restore
// is still the thing being tested; the branch is what it leaves behind.
func HandleNeonDrill(rootFolder storage.Folder, opts *NeonDrillOptions, output io.Writer) int {
	applyNeonDefaults(opts)

	report := &NeonDrillReport{
		RestoreDrillReport: RestoreDrillReport{ScratchDir: opts.TargetDir},
		NeonProjectID:      opts.ProjectID,
	}
	started := time.Now()

	client, err := resolveNeonClient(opts)
	report.add(neonAuthPhase(client, err))

	if report.failed() {
		// Nothing has been restored and no branch exists: fail before spending
		// an hour fetching a backup that has nowhere to go.
		return finishNeonDrill(report, opts, started, nil, output)
	}

	created, targetErr := ValidateDrillTarget(opts.TargetDir, opts.PGDataDir)
	report.add(targetPhase(opts.RestoreDrillOptions, targetErr))

	if report.failed() {
		return finishNeonDrill(report, opts, started, nil, output)
	}

	backup, resolveErr := resolveBackup(rootFolder, opts.BackupName)
	if resolveErr != nil {
		report.add(DrillPhase{
			Name:    DrillCheckFetch,
			Status:  DoctorFail,
			Summary: "could not resolve the backup to restore",
			Detail:  resolveErr.Error(),
		})

		return finishNeonDrill(report, opts, started, nil, output)
	}
	report.BackupName = backup.Name

	report.add(spacePhase(backup, opts.RestoreDrillOptions))
	if report.failed() {
		return finishNeonDrill(report, opts, started, nil, output)
	}

	// From here the restore may leave a directory behind.
	cleanupScratch := func() { cleanupDrill(&report.RestoreDrillReport, opts.RestoreDrillOptions, created) }

	runFetchPhase(&report.RestoreDrillReport, opts.RestoreDrillOptions, backup.Name)

	if report.failed() {
		return finishNeonDrill(report, opts, started, cleanupScratch, output)
	}

	// The cluster must stay up: the dump reads from it.
	runReplayPhase(&report.RestoreDrillReport, opts.RestoreDrillOptions, true)

	// Recovery is over at this point. The Neon load that follows is a transfer,
	// not a recovery, so it is deliberately outside the RTO measurement.
	recoveryElapsed := time.Since(started)

	if report.failed() {
		stopDrillCluster(opts.RestoreDrillOptions)

		return finishNeonDrill(report, opts, started, cleanupScratch, output)
	}

	runNeonBranchPhases(report, opts, client)

	stopDrillCluster(opts.RestoreDrillOptions)

	report.add(rtoPhase(&report.RestoreDrillReport, opts.RestoreDrillOptions, recoveryElapsed))
	report.add(rpoPhase(rootFolder, opts.RestoreDrillOptions))

	return finishNeonDrill(report, opts, started, cleanupScratch, output)
}

// runNeonBranchPhases creates the branch, loads into it, and removes it again.
//
// Branch deletion is wired to a defer and to the interrupt signals before the
// load starts. A leaked branch is a running compute endpoint somebody pays for,
// so "the process died" must not be a way to keep one.
func runNeonBranchPhases(report *NeonDrillReport, opts *NeonDrillOptions, client NeonClient) {
	ctx := context.Background()

	branchName := opts.BranchName
	if branchName == "" {
		branchName = neon.DrillBranchPrefix + utility.TimeNowCrossPlatformUTC().Format("20060102T150405Z")
	}

	branch, err := client.CreateBranch(ctx, branchName, opts.ParentBranch)
	report.add(neonBranchPhase(branch, branchName, err))

	if err != nil {
		return
	}

	report.NeonBranchID = branch.ID
	report.NeonBranchName = branch.Name
	report.BranchRetained = true

	var once sync.Once

	deleteBranch := func() {
		once.Do(func() {
			// A fresh context: the drill's may already be canceled, and this is
			// the call that must still go out.
			deleteCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()

			if delErr := client.DeleteBranch(deleteCtx, branch); delErr != nil {
				tracelog.WarningLogger.Printf(
					"Failed to delete Neon branch %s (%s): %v\nDelete it by hand: it is still billable.\n",
					branch.Name, branch.ID, delErr)

				return
			}

			report.BranchRetained = false
		})
	}

	if !opts.KeepBranch {
		restore := interruptGuard(deleteBranch)
		defer restore()
	}

	runNeonLoadPhase(report, opts, client, branch)

	report.add(neonCleanupPhase(report, opts, deleteBranch))
}

// interruptGuard runs cleanup if the process is interrupted, and returns a
// function that unregisters the handler.
func interruptGuard(cleanup func()) func() {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)

	done := make(chan struct{})

	go func() {
		select {
		case sig := <-signals:
			tracelog.WarningLogger.Printf("Interrupted (%s): removing the Neon branch before exiting.\n", sig)
			cleanup()
			os.Exit(1)
		case <-done:
		}
	}()

	return func() {
		signal.Stop(signals)
		close(done)
	}
}

func neonAuthPhase(client NeonClient, err error) DrillPhase {
	phase := DrillPhase{Name: DrillCheckNeonAuth}

	if err != nil {
		phase.Status = DoctorFail
		phase.Summary = "could not reach the Neon control plane"
		phase.Detail = err.Error()
		phase.Remedy = "Check WALG_NEON_API_KEY and WALG_NEON_PROJECT_ID, and that the key has " +
			"access to that project."

		return phase
	}

	if pingErr := client.Ping(context.Background()); pingErr != nil {
		phase.Status = DoctorFail
		phase.Summary = "the Neon credentials were rejected"
		phase.Detail = pingErr.Error()
		phase.Remedy = "Check WALG_NEON_API_KEY and WALG_NEON_PROJECT_ID."

		return phase
	}

	phase.Status = DoctorPass
	phase.Summary = "the Neon project is reachable"

	return phase
}

func neonBranchPhase(branch neon.Branch, requestedName string, err error) DrillPhase {
	phase := DrillPhase{Name: DrillCheckNeonBranch}

	if err != nil {
		phase.Status = DoctorFail
		phase.Summary = "could not create the Neon branch"
		phase.Detail = err.Error()
		phase.Remedy = "Check the project's branch limit and compute quota."

		return phase
	}

	phase.Status = DoctorPass
	phase.Summary = fmt.Sprintf("created branch %s (%s)", branch.Name, branch.ID)

	if branch.Name != requestedName {
		phase.Detail = "the control plane named the branch " + branch.Name
	}

	return phase
}

// runNeonLoadPhase dumps the recovered cluster into the branch.
func runNeonLoadPhase(report *NeonDrillReport, opts *NeonDrillOptions, client NeonClient, branch neon.Branch) {
	phase := DrillPhase{Name: DrillCheckNeonLoad}
	start := time.Now()

	if err := checkDumpVersion(opts.PgDumpPath, opts.TargetDir); err != nil {
		phase.Status = DoctorFail
		phase.Summary = "the pg_dump on this host cannot dump the restored cluster"
		phase.Detail = err.Error()
		phase.Remedy = "Use a pg_dump at least as new as the restored cluster, via --pg-dump."
		report.add(phase)

		return
	}

	ctx := context.Background()

	sourceDB, fellBack, err := resolveSourceDatabase(ctx, opts)
	if err != nil {
		phase.Status = DoctorFail
		phase.Summary = "could not decide which database to dump"
		phase.Detail = err.Error()
		phase.Remedy = "Name the database with --source-database or WALG_NEON_SOURCE_DATABASE."
		report.add(phase)

		return
	}

	opts.SourceDatabase = sourceDB
	report.SourceDatabase = sourceDB

	uri, err := client.ConnectionURI(ctx, branch.ID, opts.Database, opts.Role)
	if err != nil {
		phase.Status = DoctorFail
		phase.Summary = "could not get a connection string for the branch"
		phase.Detail = err.Error()
		phase.Remedy = fmt.Sprintf(
			"Check that role %q and database %q exist on the branch (--neon-role, --neon-database).",
			opts.Role, opts.Database)
		report.add(phase)

		return
	}

	loadEnv, err := psqlEnvFromURI(uri)
	if err != nil {
		phase.Status = DoctorFail
		phase.Summary = "the branch connection string could not be parsed"
		// Deliberately not including the URI: it carries a password.
		phase.Detail = err.Error()
		report.add(phase)

		return
	}

	transferred, err := runDumpAndLoad(ctx, opts, loadEnv)
	elapsed := time.Since(start)

	phase.Elapsed = elapsed.Round(time.Second).String()
	report.LoadSeconds = elapsed.Seconds()
	report.LoadedBytes = transferred

	if err != nil {
		phase.Status = DoctorFail
		phase.Summary = "loading the restored data into the branch failed"
		phase.Detail = err.Error()
		phase.Remedy = "Run the dump by hand against the restored cluster to see the full output."
		report.add(phase)

		return
	}

	phase.Status = DoctorPass
	phase.Summary = fmt.Sprintf("loaded %s into %s in %s",
		formatBytes(transferred), branch.Name, formatDuration(elapsed))
	phase.Detail = fmt.Sprintf("dumped %s from the restored cluster on port %d",
		sourceDB, opts.Port)

	// A cluster with nothing but the maintenance database almost certainly means
	// the drill loaded nothing. That is a warning, not a pass: a green drill
	// that moved an empty database is exactly the false assurance this command
	// exists to prevent.
	if fellBack {
		phase.Status = DoctorWarn
		phase.Summary = fmt.Sprintf("loaded %s into %s, but the restored cluster holds no user database",
			formatBytes(transferred), branch.Name)
		phase.Remedy = "Check the backup really contains the database you expect. " +
			"Name it with --source-database if it is called something unusual."
	}

	report.add(phase)
}

// resolveSourceDatabase decides which database to dump out of the restored
// cluster.
//
// Guessing a name here is how a drill ends up loading an empty database and
// reporting success, so it asks the cluster instead of assuming. The cluster is
// running by this point - the replay phase started it - so the catalog is the
// authoritative answer to "what did we just restore".
func resolveSourceDatabase(ctx context.Context, opts *NeonDrillOptions) (name string, fallback bool, err error) {
	// An explicit choice is never second-guessed, and skipping the query keeps
	// the drill working even if the catalog cannot be read.
	if opts.SourceDatabase != "" {
		return opts.SourceDatabase, false, nil
	}

	available, err := listRestoredDatabases(ctx, opts.Port)
	if err != nil {
		return "", false, fmt.Errorf("could not ask the restored cluster which databases it holds: %w", err)
	}

	return chooseSourceDatabase(available)
}

// listRestoredDatabases returns the connectable, non-template databases in the
// restored cluster.
//
// It builds an explicit DSN rather than going through Connect(), which falls
// back to localhost:5432 on failure - and would silently query the live cluster
// instead of the restored one.
func listRestoredDatabases(ctx context.Context, port int) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	dsn := fmt.Sprintf("host=127.0.0.1 port=%d dbname=%s connect_timeout=10", port, MaintenanceDB)

	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)

	rows, err := conn.Query(ctx,
		"SELECT datname FROM pg_database WHERE NOT datistemplate AND datallowconn ORDER BY datname")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var names []string

	for rows.Next() {
		var name string

		if err := rows.Scan(&name); err != nil {
			return nil, err
		}

		names = append(names, name)
	}

	return names, rows.Err()
}

// chooseSourceDatabase picks the one database worth dumping, or explains why it
// cannot.
//
// One user database is the common case and is chosen silently. Several is
// genuinely ambiguous - a Neon branch holds one database, so the drill cannot
// load them all without inventing a policy - and guessing there would be worse
// than asking.
func chooseSourceDatabase(available []string) (name string, fallback bool, err error) {
	candidates := make([]string, 0, len(available))

	for _, database := range available {
		if database != MaintenanceDB {
			candidates = append(candidates, database)
		}
	}

	switch len(candidates) {
	case 0:
		// Nothing but the maintenance database. Dumping it is almost certainly
		// pointless, but it is a truthful answer and the caller says so.
		return MaintenanceDB, true, nil
	case 1:
		return candidates[0], false, nil
	default:
		return "", false, fmt.Errorf(
			"the restored cluster holds %d databases (%s) and a Neon branch holds one: "+
				"choose with --source-database",
			len(candidates), strings.Join(candidates, ", "))
	}
}

// runDumpAndLoad streams pg_dump into psql and returns how many bytes crossed.
//
// It is a pipe rather than a temporary file: the restored cluster is already on
// disk once, and a drill should not need room for a second copy of it.
func runDumpAndLoad(ctx context.Context, opts *NeonDrillOptions, loadEnv []string) (int64, error) {
	dumpCmd := exec.CommandContext(ctx, opts.PgDumpPath,
		"--host=127.0.0.1",
		"--port="+strconv.Itoa(opts.Port),
		"--dbname="+opts.SourceDatabase,
		// Neon does not grant the ownership and tablespace control a plain dump
		// assumes, so the dump is written to not need it.
		"--no-owner",
		"--no-privileges",
		"--no-tablespaces",
		"--no-security-labels",
		"--quote-all-identifiers",
	)
	dumpCmd.Env = os.Environ()

	loadCmd := exec.CommandContext(ctx, opts.PsqlPath,
		"--no-psqlrc",
		"--quiet",
		"--set=ON_ERROR_STOP=1",
	)
	// The connection details, password included, go in the environment. Passing
	// them as arguments would publish the password to every process on the host
	// through the process list.
	loadCmd.Env = append(os.Environ(), loadEnv...)

	dumpOut, dumpErrBuf, err := utility.StartCommandWithStdoutStderr(dumpCmd)
	if err != nil {
		return 0, fmt.Errorf("could not start %s: %w", opts.PgDumpPath, err)
	}

	counter := &countingReader{inner: dumpOut}
	loadCmd.Stdin = counter

	loadOut, loadErr := loadCmd.CombinedOutput()
	dumpWaitErr := dumpCmd.Wait()

	if loadErr != nil {
		return counter.count, fmt.Errorf("psql failed: %w\n%s", loadErr, lastLines(string(loadOut), 5))
	}

	if dumpWaitErr != nil {
		return counter.count, fmt.Errorf("pg_dump failed: %w\n%s",
			dumpWaitErr, lastLines(dumpErrBuf.String(), 5))
	}

	return counter.count, nil
}

// countingReader measures the dump as it streams past, so the report can say
// how much data actually moved without buffering any of it.
type countingReader struct {
	inner io.Reader
	count int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.inner.Read(p)
	r.count += int64(n)

	return n, err
}

// checkDumpVersion refuses a pg_dump older than the cluster it is pointed at.
//
// pg_dump reads catalogs it has to understand, so an older one fails partway
// through with an error about a missing column. Checking up front turns that
// into a sentence an operator can act on.
func checkDumpVersion(pgDumpPath, targetDir string) error {
	clusterMajor, err := readClusterMajorVersion(targetDir)
	if err != nil {
		return err
	}

	out, err := exec.Command(pgDumpPath, "--version").Output()
	if err != nil {
		return fmt.Errorf("could not run %s --version: %w", pgDumpPath, err)
	}

	dumpMajor, err := parsePgDumpMajor(string(out))
	if err != nil {
		return err
	}

	if dumpMajor < clusterMajor {
		return fmt.Errorf("pg_dump is version %d but the restored cluster is version %d",
			dumpMajor, clusterMajor)
	}

	return nil
}

func readClusterMajorVersion(targetDir string) (int, error) {
	raw, err := os.ReadFile(filepath.Join(targetDir, "PG_VERSION"))
	if err != nil {
		return 0, fmt.Errorf("could not read PG_VERSION from the restored cluster: %w", err)
	}

	text := strings.TrimSpace(string(raw))

	major, err := strconv.Atoi(strings.Split(text, ".")[0])
	if err != nil {
		return 0, fmt.Errorf("could not parse PG_VERSION %q: %w", text, err)
	}

	return major, nil
}

// parsePgDumpMajor reads the major version out of "pg_dump (PostgreSQL) 16.2".
//
// It takes the first version-shaped token rather than the last field: packaged
// builds append their own provenance, as in
// "pg_dump (PostgreSQL) 15.6 (Debian 15.6-1.pgdg120+2)", where the last field
// is the distro's and the first number is PostgreSQL's.
func parsePgDumpMajor(version string) (int, error) {
	for _, field := range strings.Fields(strings.TrimSpace(version)) {
		digits := field
		if index := strings.IndexAny(digits, ".-+"); index >= 0 {
			digits = digits[:index]
		}

		if major, err := strconv.Atoi(digits); err == nil {
			return major, nil
		}
	}

	return 0, fmt.Errorf("could not parse the pg_dump version from %q", version)
}

// psqlEnvFromURI turns a Neon connection URI into libpq environment variables.
//
// The password stays in the environment of the child process and never reaches
// argv, a log line, or the report.
func psqlEnvFromURI(uri string) ([]string, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("could not parse the branch connection string: %w", err)
	}

	if parsed.Host == "" {
		return nil, fmt.Errorf("the branch connection string has no host")
	}

	port := parsed.Port()
	if port == "" {
		port = "5432"
	}

	// Neon terminates TLS and rejects plaintext, so require is the floor. A URI
	// asking for something stricter is honored.
	sslMode := parsed.Query().Get("sslmode")
	if sslMode == "" {
		sslMode = "require"
	}

	env := []string{
		"PGHOST=" + parsed.Hostname(),
		"PGPORT=" + port,
		"PGDATABASE=" + strings.TrimPrefix(parsed.Path, "/"),
		"PGSSLMODE=" + sslMode,
	}

	if parsed.User != nil {
		env = append(env, "PGUSER="+parsed.User.Username())

		if password, ok := parsed.User.Password(); ok {
			env = append(env, "PGPASSWORD="+password)
		}
	}

	if options := parsed.Query().Get("options"); options != "" {
		env = append(env, "PGOPTIONS="+options)
	}

	return env, nil
}

func neonCleanupPhase(report *NeonDrillReport, opts *NeonDrillOptions, deleteBranch func()) DrillPhase {
	phase := DrillPhase{Name: DrillCheckNeonCleanup}

	if opts.KeepBranch {
		phase.Status = DoctorSkip
		phase.Summary = fmt.Sprintf("branch %s left in place (--keep-branch)", report.NeonBranchName)
		phase.Remedy = "Delete it when you are done: a branch is a running compute endpoint."

		return phase
	}

	deleteBranch()

	if report.BranchRetained {
		phase.Status = DoctorFail
		phase.Summary = "the Neon branch could not be deleted"
		phase.Detail = fmt.Sprintf("branch %s (%s) is still there", report.NeonBranchName, report.NeonBranchID)
		phase.Remedy = "Delete it by hand. `wal-g neon-branches` lists the ones still present."

		return phase
	}

	phase.Status = DoctorPass
	phase.Summary = "branch removed"

	return phase
}

// applyNeonDefaults fills in what the caller left blank.
func applyNeonDefaults(opts *NeonDrillOptions) {
	if opts.Port == 0 {
		opts.Port = DefaultDrillPort
	}

	if opts.StartTimeout <= 0 {
		opts.StartTimeout = DefaultDrillStartTimeout
	}

	if opts.SpaceMargin <= 0 {
		opts.SpaceMargin = DefaultRestoreSpaceMargin
	}

	if opts.Role == "" {
		opts.Role = DefaultNeonRole
	}

	if opts.Database == "" {
		opts.Database = DefaultNeonDatabase
	}

	// SourceDatabase is deliberately not defaulted: empty means "ask the
	// restored cluster", which is decided once it is running.

	if opts.PgDumpPath == "" {
		opts.PgDumpPath = "pg_dump"
	}

	if opts.PsqlPath == "" {
		opts.PsqlPath = "psql"
	}

	// The dump reads from the restored cluster, so it has to be running. This
	// is not a choice the caller gets to make.
	opts.StartPostgres = true
}

// resolveNeonClient returns the injected client, or builds one.
func resolveNeonClient(opts *NeonDrillOptions) (NeonClient, error) {
	if opts.Client != nil {
		return opts.Client, nil
	}

	return neon.NewClient(neon.Config{
		APIKey:    opts.APIKey,
		ProjectID: opts.ProjectID,
		Endpoint:  opts.APIEndpoint,
	})
}

func finishNeonDrill(report *NeonDrillReport, opts *NeonDrillOptions,
	started time.Time, cleanup func(), output io.Writer) int {
	if report.TotalSeconds == 0 {
		report.TotalSeconds = time.Since(started).Seconds()
	}

	if cleanup != nil {
		cleanup()
	}

	report.ScratchDirPresent = directoryHasContent(opts.TargetDir)
	report.Pass = report.Failed == 0

	if err := WriteNeonDrillReport(report, opts.Format, output); err != nil {
		tracelog.ErrorLogger.Printf("Failed to write the report: %v\n", err)

		return 1
	}

	if report.Pass {
		return 0
	}

	return 1
}

// WriteNeonDrillReport renders a Neon drill report.
func WriteNeonDrillReport(report *NeonDrillReport, format string, output io.Writer) error {
	if format == "json" {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return err
		}

		_, err = output.Write(append(data, '\n'))

		return err
	}

	writeNeonDrillText(report, output)

	return nil
}

func writeNeonDrillText(report *NeonDrillReport, output io.Writer) {
	fmt.Fprintf(output, "wal-g neon-drill\n\n")

	if report.BackupName != "" {
		fmt.Fprintf(output, "Restoring  %s into %s\n", report.BackupName, report.ScratchDir)
	}

	if report.NeonBranchName != "" {
		fmt.Fprintf(output, "Loading    into Neon branch %s\n", report.NeonBranchName)
	}

	fmt.Fprintln(output)

	for _, phase := range report.Phases {
		fmt.Fprintf(output, "%-6s %-14s %s\n", statusLabel(phase.Status), phase.Name, phase.Summary)

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

	if report.BranchRetained && report.NeonBranchName != "" {
		fmt.Fprintf(output, " Neon branch %s left in place.\n", report.NeonBranchName)
	} else {
		fmt.Fprintln(output)
	}
}
