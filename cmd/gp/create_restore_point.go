// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package gp

import (
	"github.com/spf13/cobra"
	"github.com/wal-g/tracelog"

	"github.com/lateos-ai/wal-g/internal/databases/greenplum"
)

const (
	createRestorePointDescription = "Creates cluster-wide restore point with the specified name"
)

var (

	// createRestorePointCmd represents the createRestorePoint command

	createRestorePointCmd = &cobra.Command{
		Use: "create-restore-point name",

		Short: createRestorePointDescription, // TODO : improve description

		Args: cobra.ExactArgs(1),

		Run: func(cmd *cobra.Command, args []string) {
			name := args[0]

			restorePointCreator, err := greenplum.NewRestorePointCreator(name)

			tracelog.ErrorLogger.FatalOnError(err)

			restorePointCreator.Create()
		},
	}
)

func init() {
	cmd.AddCommand(createRestorePointCmd)
}
