// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package mongo

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/wal-g/tracelog"

	"github.com/lateos-ai/wal-g/internal/databases/mongo"
	"github.com/lateos-ai/wal-g/internal/databases/mongo/common"
)

const BackupShowShortDescription = "Prints information about backup"

// backupShowCmd represents the backupList command

var backupShowCmd = &cobra.Command{
	Use: "backup-show <backup-name>",

	Short: BackupShowShortDescription,

	Args: cobra.ExactArgs(1),

	Run: func(cmd *cobra.Command, args []string) {
		backupName := args[0]

		backupFolder, err := common.GetBackupFolder()

		tracelog.ErrorLogger.FatalOnError(err)

		err = mongo.HandleBackupShow(backupFolder, backupName, os.Stdout, true)

		tracelog.ErrorLogger.FatalOnError(err)
	},
}

func init() {
	cmd.AddCommand(backupShowCmd)
}
