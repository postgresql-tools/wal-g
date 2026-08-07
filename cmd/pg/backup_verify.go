// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

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
coverage, deploy metadata presence, and WAL chain continuity. It also runs a
decrypt canary: the smallest tar partition is fetched and its first tar header is
read, proving the crypter and compression codec configured on this host can
actually open the backup. Completes in seconds with near-zero egress cost. Pass
--no-canary to stay strictly metadata-only.

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
	noCanaryFlag          = "no-canary"
	noCanaryDescription   = "Skip the Tier 1 decrypt canary, making the run purely metadata-based (no object data is fetched)."
)

var (
	backupVerifySample     int
	backupVerifySeed       int64
	backupVerifyTargetLSN  string
	backupVerifyTargetTime string
	backupVerifyFormat     string
	backupVerifyNoCanary   bool

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

			exitCode := postgres.HandleBackupVerify(
				storage.RootFolder(),
				postgres.BackupVerifyOptions{
					BackupName: backupName,
					SamplePct:  backupVerifySample,
					Seed:       backupVerifySeed,
					TargetLSN:  backupVerifyTargetLSN,
					TargetTime: backupVerifyTargetTime,
					Format:     backupVerifyFormat,
					SkipCanary: backupVerifyNoCanary,
				},
				os.Stdout,
			)

			os.Exit(exitCode)
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
	backupVerifyCmd.Flags().BoolVar(&backupVerifyNoCanary, noCanaryFlag, false, noCanaryDescription)
}
