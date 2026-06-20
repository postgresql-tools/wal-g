package pg

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/wal-g/tracelog"

	"github.com/lateos-ai/wal-g/internal"
	"github.com/lateos-ai/wal-g/internal/databases/postgres"
)

const (
	backupVerifyUse   = "backup-verify [backup_name]"
	backupVerifyShort = "Verify backup content integrity by re-reading tar partitions from storage"
	backupVerifyLong  = `Run metadata-only (Tier 1) or spot-check (Tier 2 with --sample) verification of a backup.

Tier 1 (default): verifies sentinel integrity, manifest completeness, checksum
coverage, deploy metadata presence, and WAL chain continuity. No data blocks are
downloaded. Completes in seconds with near-zero egress cost.

Tier 2 (--sample <pct>): downloads a random sample of tar partitions, re-computes
SHA256 checksums, and compares against stored values from backup-push. For backups
taken before checksum support (pre-PR1), only readability is verified.

Passing backup-verify does NOT guarantee Postgres starts cleanly on restore.
This tool narrows the population of restore failures catchable cheaply; it is a
complement to periodic full restore tests, not a replacement.`
	sampleFlag            = "sample"
	sampleShortFlag       = "s"
	sampleDescription     = "Percentage of tar partitions to download and verify (Tier 2). Default 0 (Tier 1 only)."
	seedFlag              = "seed"
	seedDescription       = "Random seed for reproducible Tier 2 sampling. Default 0 (time-based)."
	targetLSNFlag         = "target-lsn"
	targetLSNDescription  = "End LSN for WAL chain verification scope."
	targetTimeFlag        = "target-time"
	targetTimeDescription = "End timestamp (RFC3339) for WAL chain verification scope."
	formatFlag            = "format"
	formatDescription     = "Output format: text or json. Default: text."
)

var (
	backupVerifySample     int
	backupVerifySeed       int64
	backupVerifyTargetLSN  string
	backupVerifyTargetTime string
	backupVerifyFormat     string

	backupVerifyCmd = &cobra.Command{
		Use:   backupVerifyUse,
		Short: backupVerifyShort,
		Long:  backupVerifyLong,
		Args:  cobra.MaximumNArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			storage, err := internal.ConfigureStorage()
			tracelog.ErrorLogger.FatalOnError(err)

			backupName := ""
			if len(args) > 0 {
				backupName = args[0]
			}

			postgres.HandleBackupVerify(
				storage.RootFolder(),
				backupName,
				backupVerifySample,
				backupVerifySeed,
				backupVerifyTargetLSN,
				backupVerifyTargetTime,
				backupVerifyFormat,
				os.Stdout,
			)
		},
	}
)

func init() {
	Cmd.AddCommand(backupVerifyCmd)

	backupVerifyCmd.Flags().IntVarP(&backupVerifySample, sampleFlag, sampleShortFlag, 0, sampleDescription)
	backupVerifyCmd.Flags().Int64Var(&backupVerifySeed, seedFlag, 0, seedDescription)
	backupVerifyCmd.Flags().StringVar(&backupVerifyTargetLSN, targetLSNFlag, "", targetLSNDescription)
	backupVerifyCmd.Flags().StringVar(&backupVerifyTargetTime, targetTimeFlag, "", targetTimeDescription)
	backupVerifyCmd.Flags().StringVar(&backupVerifyFormat, formatFlag, "text", formatDescription)
}
