// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package gp

import (
	"github.com/spf13/cobra"
	"github.com/wal-g/tracelog"

	"github.com/lateos-ai/wal-g/internal/databases/greenplum"
	"github.com/lateos-ai/wal-g/internal/multistorage/policies"
	"github.com/lateos-ai/wal-g/utility"
)

const (
	restorePointListShortDescription = "Prints available restore points"
)

var (

	// restorePointListCmd represents the restorePointList command

	restorePointListCmd = &cobra.Command{
		Use: "restore-point-list",

		Short: restorePointListShortDescription, // TODO : improve description

		Args: cobra.NoArgs,

		Run: func(cmd *cobra.Command, args []string) {
			rootFolder, err := getMultistorageRootFolder(true, policies.UniteAllStorages)

			tracelog.ErrorLogger.FatalOnError(err)

			greenplum.HandleRestorePointList(rootFolder.GetSubFolder(utility.BaseBackupPath), pretty, jsonOutput)
		},
	}
)

func init() {
	cmd.AddCommand(restorePointListCmd)

	restorePointListCmd.Flags().BoolVar(&pretty, PrettyFlag, false, "Prints more readable output")

	restorePointListCmd.Flags().BoolVar(&jsonOutput, JSONFlag, false, "Prints output in json format")
}
