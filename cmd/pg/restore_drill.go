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
	restoreTestUse   = "restore-test"
	restoreTestShort = "Restore a backup into a scratch directory and time the recovery"
	restoreTestLong  = `Rehearse a restore, measure how long it takes, and judge it against the
recovery objectives you declare.

A backup that has never been restored is a backup nobody has tested. This
restores one for real - into a scratch directory, never the live data directory
- times it, and removes it again.

Phases:
  target-dir  the destination is empty and is not the live data directory
  space       free space against the size of the backup being restored
  fetch       the restore itself, timed, with throughput
  replay      starting the restored cluster and reaching consistency
  rto         elapsed recovery against the declared budget
  rpo         the newest point still restorable, against the declared budget

By default the drill measures the fetch only, and the rto phase says so.
Pass --start-postgres to also start the restored cluster on a scratch port,
replay WAL to consistency, and include that in the measured time. That needs
pg_ctl, which is found via --pg-ctl or PATH.

Safety: the target directory must be empty or absent, and must not be PGDATA or
inside it. The drill refuses to run otherwise. What it creates it removes,
unless --keep is given.

The restore runs as a separate wal-g process, so a restore that fails hard still
produces a verdict and still cleans up. It is also the same command an operator
would run, which is the point of rehearsing it.

The exit code is 0 when nothing failed and 1 otherwise.`

	restoreTestTargetFlag        = "target-dir"
	restoreTestTargetDescription = "Directory to restore into. Must be empty or absent, and never PGDATA."

	restoreTestRTOFlag        = "rto"
	restoreTestRTODescription = "Recovery time budget, e.g. 2h. Default: WALG_RTO."

	restoreTestRPOFlag        = "rpo"
	restoreTestRPODescription = "Data loss budget for the reachable recovery point. Default: WALG_RPO."

	restoreTestStartPGFlag        = "start-postgres"
	restoreTestStartPGDescription = "Start the restored cluster and measure WAL replay as part of the RTO."

	restoreTestPGCtlFlag        = "pg-ctl"
	restoreTestPGCtlDescription = "Path to pg_ctl, used with --start-postgres. Default: pg_ctl on PATH."

	restoreTestPortFlag        = "port"
	restoreTestPortDescription = "Port for the restored cluster during the drill."

	restoreTestKeepFlag        = "keep"
	restoreTestKeepDescription = "Leave the restored cluster in place instead of removing it."

	restoreTestBinaryFlag        = "wal-g-binary"
	restoreTestBinaryDescription = "wal-g to run the restore with. Default: this binary."

	restoreTestFormatFlag        = "format"
	restoreTestFormatDescription = "Output format: text or json. Default: text."

	restoreTestTimeoutFlag        = "start-timeout"
	restoreTestTimeoutDescription = "How long to wait for the restored cluster to reach consistency."
)

var (
	restoreTestTargetDir  string
	restoreTestRTO        string
	restoreTestRPO        string
	restoreTestStartPG    bool
	restoreTestPGCtl      string
	restoreTestPort       int
	restoreTestKeep       bool
	restoreTestBinary     string
	restoreTestFormat     string
	restoreTestTimeout    time.Duration
	restoreTestBackupName string

	restoreTestCmd = &cobra.Command{
		Use:   restoreTestUse + " [backup_name]",
		Short: restoreTestShort,
		Long:  restoreTestLong,
		Args:  cobra.MaximumNArgs(1),
		Run:   runRestoreTest,
	}
)

func runRestoreTest(cmd *cobra.Command, args []string) {
	if len(args) == 1 {
		restoreTestBackupName = args[0]
	}

	opts := postgres.RestoreDrillOptions{
		TargetDir:     restoreTestTargetDir,
		BackupName:    restoreTestBackupName,
		StartPostgres: restoreTestStartPG,
		PGCtlPath:     restoreTestPGCtl,
		Port:          restoreTestPort,
		Keep:          restoreTestKeep,
		Format:        restoreTestFormat,
		StartTimeout:  restoreTestTimeout,
		ConfigFile:    conf.CfgFile,
		PGDataDir:     viper.GetString(conf.PgDataSetting),
	}

	var err error

	opts.RTO, err = resolveDurationObjective(restoreTestRTO, conf.RTOSetting)
	tracelog.ErrorLogger.FatalfOnError("Invalid --rto: %v", err)

	opts.RPO, err = resolveDurationObjective(restoreTestRPO, conf.RPOSetting)
	tracelog.ErrorLogger.FatalfOnError("Invalid --rpo: %v", err)

	opts.WalgBinary, err = resolveWalgBinary()
	tracelog.ErrorLogger.FatalfOnError("Could not determine which wal-g to restore with: %v", err)

	storage, err := internal.ConfigureStorage()
	tracelog.ErrorLogger.FatalOnError(err)

	os.Exit(postgres.HandleRestoreDrill(storage.RootFolder(), opts, os.Stdout))
}

// resolveWalgBinary prefers an explicit path, then this executable, so a drill
// rehearses the same build that is deployed.
func resolveWalgBinary() (string, error) {
	if restoreTestBinary != "" {
		return restoreTestBinary, nil
	}

	return os.Executable()
}

func init() {
	Cmd.AddCommand(restoreTestCmd)

	restoreTestCmd.Flags().StringVar(&restoreTestTargetDir, restoreTestTargetFlag, "",
		restoreTestTargetDescription)
	restoreTestCmd.Flags().StringVar(&restoreTestRTO, restoreTestRTOFlag, "", restoreTestRTODescription)
	restoreTestCmd.Flags().StringVar(&restoreTestRPO, restoreTestRPOFlag, "", restoreTestRPODescription)
	restoreTestCmd.Flags().BoolVar(&restoreTestStartPG, restoreTestStartPGFlag, false,
		restoreTestStartPGDescription)
	restoreTestCmd.Flags().StringVar(&restoreTestPGCtl, restoreTestPGCtlFlag, "pg_ctl",
		restoreTestPGCtlDescription)
	restoreTestCmd.Flags().IntVar(&restoreTestPort, restoreTestPortFlag, postgres.DefaultDrillPort,
		restoreTestPortDescription)
	restoreTestCmd.Flags().BoolVar(&restoreTestKeep, restoreTestKeepFlag, false, restoreTestKeepDescription)
	restoreTestCmd.Flags().StringVar(&restoreTestBinary, restoreTestBinaryFlag, "",
		restoreTestBinaryDescription)
	restoreTestCmd.Flags().StringVar(&restoreTestFormat, restoreTestFormatFlag, "text",
		restoreTestFormatDescription)
	restoreTestCmd.Flags().DurationVar(&restoreTestTimeout, restoreTestTimeoutFlag,
		postgres.DefaultDrillStartTimeout, restoreTestTimeoutDescription)

	_ = restoreTestCmd.MarkFlagRequired(restoreTestTargetFlag)
}
