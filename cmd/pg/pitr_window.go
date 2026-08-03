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
	pitrWindowUse   = "pitr-window"
	pitrWindowShort = "Report the range of time this storage can be recovered to"
	pitrWindowLong  = `Report which moments in time the cluster can currently be restored to.

A restore needs a backup that can reach a consistent state and an unbroken run
of WAL from it. So the window opens when a backup finishes - not when it starts,
since recovery cannot target a moment part-way through one - and runs until the
first missing WAL segment after it. Overlapping windows are merged; what is left
between them is a gap, a stretch of history that no backup in storage can
recover to.

Windows are reported per timeline. A restore can follow a timeline switch, but
which fork a moment belongs to depends on the recovery target, so folding
timelines together would report windows no single restore could deliver.

Window ends are dated from the upload time of the last WAL segment, which
approximates the commit times inside it without fetching and decrypting every
segment.

Backups that cannot serve a restore at all - a delta whose base is gone, or a
backup missing the WAL it needs to reach consistency - are listed separately.
They still occupy storage and still appear in backup-list.

The exit code is 1 when nothing is restorable, or when --min-window is set and
the recoverable span falls short of it; 0 otherwise.`

	pitrWindowFormatFlag        = "format"
	pitrWindowFormatDescription = "Output format: text or json. Default: text."

	pitrWindowMinFlag        = "min-window"
	pitrWindowMinDescription = "Exit non-zero if less than this much recoverable time remains (e.g. 72h)."
)

var (
	pitrWindowFormat  string
	pitrWindowMinimum time.Duration

	pitrWindowCmd = &cobra.Command{
		Use:   pitrWindowUse,
		Short: pitrWindowShort,
		Long:  pitrWindowLong,
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			storage, err := internal.ConfigureStorage()
			tracelog.ErrorLogger.FatalOnError(err)

			exitCode := postgres.HandlePITRWindow(
				storage.RootFolder(),
				postgres.PITRWindowOptions{
					Format:    pitrWindowFormat,
					MinWindow: pitrWindowMinimum,
				},
				os.Stdout,
			)

			os.Exit(exitCode)
		},
	}
)

func init() {
	Cmd.AddCommand(pitrWindowCmd)

	pitrWindowCmd.Flags().StringVar(&pitrWindowFormat, pitrWindowFormatFlag, "text", pitrWindowFormatDescription)
	pitrWindowCmd.Flags().DurationVar(&pitrWindowMinimum, pitrWindowMinFlag, 0, pitrWindowMinDescription)
}
