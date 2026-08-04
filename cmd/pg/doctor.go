// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

package pg

import (
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/wal-g/tracelog"

	"github.com/lateos-ai/wal-g/internal"
	"github.com/lateos-ai/wal-g/internal/databases/postgres"
)

const (
	doctorUse   = "doctor"
	doctorShort = "Check that this host is configured to back up and to restore"
	doctorLong  = `Run preflight checks against the current configuration and report what would
break, before it matters.

Checks performed:
  config         required settings resolve, and which layer supplied each value
  storage        list, write, read-back, and delete against the configured storage
  encryption     the configured crypter can decrypt what it encrypts
  postgres       connectivity and server version
  archiving      archive_mode is on and archive_command calls wal-g wal-push
  backups        a backup exists in storage, and how old it is
  restore-space  free space on the data directory vs. the size of the latest backup

Each check reports pass, warn, fail, or skip. The exit code is 0 when nothing
failed and 1 otherwise; warnings do not affect it. Use --format json to consume
the report from a monitoring or CI pipeline.

The storage check writes and then deletes one small probe object. Nothing else
is modified.`

	doctorSkipPGFlag        = "skip-pg"
	doctorSkipPGDescription = "Skip checks that need a live PostgreSQL connection (for restore-only hosts)."

	doctorDataDirFlag        = "data-dir"
	doctorDataDirDescription = "Data directory to size the restore-space check against. Default: PGDATA."

	doctorFormatFlag        = "format"
	doctorFormatDescription = "Output format: text or json. Default: text."

	doctorSpaceMarginFlag        = "space-margin"
	doctorSpaceMarginDescription = "Multiple of the latest backup's uncompressed size that must be free to pass the restore-space check."

	doctorStaleAfterFlag        = "stale-after"
	doctorStaleAfterDescription = "Age past which the newest backup is reported as stale."
)

var (
	doctorSkipPG      bool
	doctorDataDir     string
	doctorFormat      string
	doctorSpaceMargin float64
	doctorStaleAfter  time.Duration

	doctorCmd = &cobra.Command{
		Use:   doctorUse,
		Short: doctorShort,
		Long:  doctorLong,
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			storage, err := internal.ConfigureStorage()
			tracelog.ErrorLogger.FatalOnError(err)

			exitCode := postgres.HandleDoctor(
				storage.RootFolder(),
				postgres.DoctorOptions{
					Format:      doctorFormat,
					SkipPG:      doctorSkipPG,
					DataDir:     doctorDataDir,
					SpaceMargin: doctorSpaceMargin,
					StaleAfter:  doctorStaleAfter,
				},
				os.Stdout,
			)

			os.Exit(exitCode)
		},
	}
)

func init() {
	Cmd.AddCommand(doctorCmd)

	doctorCmd.Flags().BoolVar(&doctorSkipPG, doctorSkipPGFlag, false, doctorSkipPGDescription)
	doctorCmd.Flags().StringVar(&doctorDataDir, doctorDataDirFlag, "", doctorDataDirDescription)
	doctorCmd.Flags().StringVar(&doctorFormat, doctorFormatFlag, "text", doctorFormatDescription)
	doctorCmd.Flags().Float64Var(&doctorSpaceMargin, doctorSpaceMarginFlag,
		postgres.DefaultRestoreSpaceMargin, doctorSpaceMarginDescription)
	doctorCmd.Flags().DurationVar(&doctorStaleAfter, doctorStaleAfterFlag,
		postgres.DefaultStaleAfter, doctorStaleAfterDescription)
}
