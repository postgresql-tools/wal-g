// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package pg

import (
	"github.com/spf13/cobra"
	"github.com/wal-g/tracelog"

	"github.com/lateos-ai/wal-g/internal"
	"github.com/lateos-ai/wal-g/internal/databases/postgres"
	"github.com/lateos-ai/wal-g/utility"
)

const (
	catchupListShortDescription = "Prints available incremental backups"
)

var (

	// catchupListCmd represents the catchupList command

	catchupListCmd = &cobra.Command{
		Use: "catchup-list",

		Short: catchupListShortDescription, // TODO : improve description

		Args: cobra.NoArgs,

		Run: func(cmd *cobra.Command, args []string) {
			storage, err := internal.ConfigureStorage()

			tracelog.ErrorLogger.FatalOnError(err)

			if detail {
				postgres.HandleDetailedBackupList(storage.RootFolder().GetSubFolder(utility.CatchupPath), pretty, json)
			} else {
				internal.HandleDefaultBackupList(storage.RootFolder().GetSubFolder(utility.CatchupPath), pretty, json)
			}
		},
	}
)

func init() {
	Cmd.AddCommand(catchupListCmd)

	catchupListCmd.Flags().BoolVar(&pretty, PrettyFlag, false, "Prints more readable output")

	catchupListCmd.Flags().BoolVar(&json, JSONFlag, false, "Prints output in json format")

	catchupListCmd.Flags().BoolVar(&detail, DetailFlag, false, "Prints extra backup details")
}
