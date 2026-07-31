// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package etcd

import (
	"github.com/spf13/cobra"
	"github.com/wal-g/tracelog"

	"github.com/lateos-ai/wal-g/internal"
	"github.com/lateos-ai/wal-g/utility"
)

const backupListShortDescription = "Prints available backups"

// backupListCmd represents the backupList command

var backupListCmd = &cobra.Command{

	Use: "backup-list",

	Short: backupListShortDescription,

	Args: cobra.NoArgs,

	Run: func(cmd *cobra.Command, args []string) {

		storage, err := internal.ConfigureStorage()

		tracelog.ErrorLogger.FatalOnError(err)

		internal.HandleDefaultBackupList(storage.RootFolder().GetSubFolder(utility.BaseBackupPath), false, false)

	},
}

func init() {
	cmd.AddCommand(backupListCmd)
}
