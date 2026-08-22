// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

package pg

import (
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/wal-g/tracelog"

	"github.com/lateos-ai/wal-g/internal"
	conf "github.com/lateos-ai/wal-g/internal/config"
	"github.com/lateos-ai/wal-g/internal/databases/postgres"
)

const (
	neonDrillUse   = "neon-drill"
	neonDrillShort = "Restore a backup and load the result into a fresh Neon branch"
	neonDrillLong  = `Rehearse a restore, then leave the recovered data somewhere you can query it.

A backup that has never been restored is a backup nobody has tested, and a
restore nobody looked at is a restore nobody checked. This restores a backup for
real, then loads it into a throwaway Neon branch and removes the branch again.

This is a two-stage drill, and it has to be. Neon has no data directory to write
and no replication protocol to stream into, so a wal-g physical backup cannot be
restored into it. What happens instead is:

  1. backup-fetch restores the backup into a scratch directory, for real
  2. the restored cluster is started and replays WAL to consistency
  3. pg_dump streams that cluster into a new Neon branch
  4. the branch is deleted, unless --keep-branch

The physical restore is still the thing being tested. The branch is what it
leaves behind. Stage 3 is a logical copy and is NOT part of the RTO verdict:
it is a transfer, not a recovery, and timing it against a recovery budget would
fail the drill for a reason that has nothing to do with backup health.

Phases:
  neon-auth      the Neon credentials work and the project exists
  target-dir     the destination is empty and is not the live data directory
  space          free space against the size of the backup being restored
  fetch          the restore itself, timed, with throughput
  replay         starting the restored cluster and reaching consistency
  neon-branch    creating the branch and waiting for its endpoint
  neon-load      the dump into the branch, timed separately
  neon-cleanup   removing the branch
  rto            elapsed recovery (fetch and replay) against the declared budget
  rpo            the newest point still restorable, against the declared budget

Credentials come from WALG_NEON_API_KEY and WALG_NEON_PROJECT_ID. The API key is
never written to the report and never logged.

Safety: the target directory must be empty or absent and must not be PGDATA.
The branch is deleted on every exit path, including an interrupt, because a
branch left behind is a running compute endpoint somebody pays for. Only
branches wal-g created are ever deleted.

The exit code is 0 when nothing failed and 1 otherwise.`

	neonDrillTargetFlag        = "target-dir"
	neonDrillTargetDescription = "Directory to restore into. Must be empty or absent, and never PGDATA."

	neonDrillRTOFlag        = "rto"
	neonDrillRTODescription = "Recovery time budget for fetch and replay, e.g. 2h. Default: WALG_RTO."

	neonDrillRPOFlag        = "rpo"
	neonDrillRPODescription = "Data loss budget for the reachable recovery point. Default: WALG_RPO."

	neonDrillProjectFlag        = "neon-project"
	neonDrillProjectDescription = "Neon project to create the branch in. Default: WALG_NEON_PROJECT_ID."

	neonDrillParentFlag        = "neon-parent-branch"
	neonDrillParentDescription = "Branch to fork from. Default: WALG_NEON_PARENT_BRANCH, or the project default."

	neonDrillBranchNameFlag        = "branch-name"
	neonDrillBranchNameDescription = "Name for the created branch. Must start with walg-drill- so cleanup will remove it."

	neonDrillKeepBranchFlag        = "keep-branch"
	neonDrillKeepBranchDescription = "Leave the branch in place instead of deleting it. It stays billable."

	neonDrillRoleFlag        = "neon-role"
	neonDrillRoleDescription = "Role to load as on the branch. Default: WALG_NEON_ROLE."

	neonDrillDatabaseFlag        = "neon-database"
	neonDrillDatabaseDescription = "Database to load into on the branch. Default: WALG_NEON_DATABASE."

	neonDrillSourceDBFlag        = "source-database"
	neonDrillSourceDBDescription = "Database to dump out of the restored cluster. " +
		"Default: ask the cluster, and use its single user database."

	neonDrillPgDumpFlag        = "pg-dump"
	neonDrillPgDumpDescription = "Path to pg_dump. Must be at least as new as the restored cluster."

	neonDrillPsqlFlag        = "psql"
	neonDrillPsqlDescription = "Path to psql, used to load the dump into the branch."

	neonDrillPGCtlFlag        = "pg-ctl"
	neonDrillPGCtlDescription = "Path to pg_ctl, used to start the restored cluster."

	neonDrillPortFlag        = "port"
	neonDrillPortDescription = "Port for the restored cluster during the drill."

	neonDrillKeepFlag        = "keep"
	neonDrillKeepDescription = "Leave the restored cluster on disk instead of removing it."

	neonDrillBinaryFlag        = "wal-g-binary"
	neonDrillBinaryDescription = "wal-g to run the restore with. Default: this binary."

	neonDrillTimeoutFlag        = "start-timeout"
	neonDrillTimeoutDescription = "How long to wait for the restored cluster to reach consistency."
)

var (
	neonDrillTargetDir  string
	neonDrillRTO        string
	neonDrillRPO        string
	neonDrillProject    string
	neonDrillParent     string
	neonDrillBranchName string
	neonDrillKeepBranch bool
	neonDrillRole       string
	neonDrillDatabase   string
	neonDrillSourceDB   string
	neonDrillPgDump     string
	neonDrillPsql       string
	neonDrillPGCtl      string
	neonDrillPort       int
	neonDrillKeep       bool
	neonDrillBinary     string
	neonDrillFormat     string
	neonDrillTimeout    time.Duration
	neonDrillBackupName string

	neonDrillCmd = &cobra.Command{
		Use:   neonDrillUse + " [backup_name]",
		Short: neonDrillShort,
		Long:  neonDrillLong,
		Args:  cobra.MaximumNArgs(1),
		Run:   runNeonDrill,
	}
)

func runNeonDrill(_ *cobra.Command, args []string) {
	tracelog.ErrorLogger.FatalOnError(postgres.ValidateReportFormat(neonDrillFormat))

	if len(args) == 1 {
		neonDrillBackupName = args[0]
	}

	opts := &postgres.NeonDrillOptions{
		RestoreDrillOptions: postgres.RestoreDrillOptions{
			TargetDir:    neonDrillTargetDir,
			BackupName:   neonDrillBackupName,
			PGCtlPath:    neonDrillPGCtl,
			Port:         neonDrillPort,
			Keep:         neonDrillKeep,
			Format:       neonDrillFormat,
			StartTimeout: neonDrillTimeout,
			ConfigFile:   conf.CfgFile,
			PGDataDir:    viper.GetString(conf.PgDataSetting),
		},
		APIKey:         viper.GetString(conf.NeonAPIKeySetting),
		APIEndpoint:    viper.GetString(conf.NeonAPIEndpointSetting),
		ProjectID:      neonDrillProject,
		ParentBranch:   neonDrillParent,
		BranchName:     neonDrillBranchName,
		KeepBranch:     neonDrillKeepBranch,
		Role:           neonDrillRole,
		Database:       neonDrillDatabase,
		SourceDatabase: neonDrillSourceDB,
		PgDumpPath:     neonDrillPgDump,
		PsqlPath:       neonDrillPsql,
	}

	// Flags win, then config. Matching how the other drill commands resolve.
	if opts.ProjectID == "" {
		opts.ProjectID = viper.GetString(conf.NeonProjectIDSetting)
	}

	if opts.ParentBranch == "" {
		opts.ParentBranch = viper.GetString(conf.NeonParentBranchSetting)
	}

	if opts.Role == "" {
		opts.Role = viper.GetString(conf.NeonRoleSetting)
	}

	if opts.Database == "" {
		opts.Database = viper.GetString(conf.NeonDatabaseSetting)
	}

	// Left empty on purpose when neither is set: the drill then asks the
	// restored cluster which database it holds.
	if opts.SourceDatabase == "" {
		opts.SourceDatabase = viper.GetString(conf.NeonSourceDatabaseSetting)
	}

	var err error

	opts.RTO, err = resolveDurationObjective(neonDrillRTO, conf.RTOSetting)
	tracelog.ErrorLogger.FatalfOnError("Invalid --rto: %v", err)

	opts.RPO, err = resolveDurationObjective(neonDrillRPO, conf.RPOSetting)
	tracelog.ErrorLogger.FatalfOnError("Invalid --rpo: %v", err)

	// Not resolveWalgBinary(): that one reads restore-test's own flag. Prefer an
	// explicit path, then this executable, so a drill rehearses the deployed build.
	opts.WalgBinary = neonDrillBinary
	if opts.WalgBinary == "" {
		opts.WalgBinary, err = os.Executable()
		tracelog.ErrorLogger.FatalfOnError("Could not determine which wal-g to restore with: %v", err)
	}

	storage, err := internal.ConfigureStorage()
	tracelog.ErrorLogger.FatalOnError(err)

	os.Exit(postgres.HandleNeonDrill(storage.RootFolder(), opts, os.Stdout))
}

func init() {
	Cmd.AddCommand(neonDrillCmd)

	neonDrillCmd.Flags().StringVar(&neonDrillTargetDir, neonDrillTargetFlag, "", neonDrillTargetDescription)
	neonDrillCmd.Flags().StringVar(&neonDrillRTO, neonDrillRTOFlag, "", neonDrillRTODescription)
	neonDrillCmd.Flags().StringVar(&neonDrillRPO, neonDrillRPOFlag, "", neonDrillRPODescription)
	neonDrillCmd.Flags().StringVar(&neonDrillProject, neonDrillProjectFlag, "", neonDrillProjectDescription)
	neonDrillCmd.Flags().StringVar(&neonDrillParent, neonDrillParentFlag, "", neonDrillParentDescription)
	neonDrillCmd.Flags().StringVar(&neonDrillBranchName, neonDrillBranchNameFlag, "",
		neonDrillBranchNameDescription)
	neonDrillCmd.Flags().BoolVar(&neonDrillKeepBranch, neonDrillKeepBranchFlag, false,
		neonDrillKeepBranchDescription)
	neonDrillCmd.Flags().StringVar(&neonDrillRole, neonDrillRoleFlag, "", neonDrillRoleDescription)
	neonDrillCmd.Flags().StringVar(&neonDrillDatabase, neonDrillDatabaseFlag, "", neonDrillDatabaseDescription)
	neonDrillCmd.Flags().StringVar(&neonDrillSourceDB, neonDrillSourceDBFlag, "",
		neonDrillSourceDBDescription)
	neonDrillCmd.Flags().StringVar(&neonDrillPgDump, neonDrillPgDumpFlag, "pg_dump", neonDrillPgDumpDescription)
	neonDrillCmd.Flags().StringVar(&neonDrillPsql, neonDrillPsqlFlag, "psql", neonDrillPsqlDescription)
	neonDrillCmd.Flags().StringVar(&neonDrillPGCtl, neonDrillPGCtlFlag, "pg_ctl", neonDrillPGCtlDescription)
	neonDrillCmd.Flags().IntVar(&neonDrillPort, neonDrillPortFlag, postgres.DefaultDrillPort,
		neonDrillPortDescription)
	neonDrillCmd.Flags().BoolVar(&neonDrillKeep, neonDrillKeepFlag, false, neonDrillKeepDescription)
	neonDrillCmd.Flags().StringVar(&neonDrillBinary, neonDrillBinaryFlag, "", neonDrillBinaryDescription)
	neonDrillCmd.Flags().StringVar(&neonDrillFormat, formatFlag, "text", formatDescription)
	neonDrillCmd.Flags().DurationVar(&neonDrillTimeout, neonDrillTimeoutFlag,
		postgres.DefaultDrillStartTimeout, neonDrillTimeoutDescription)

	_ = neonDrillCmd.MarkFlagRequired(neonDrillTargetFlag)
}
