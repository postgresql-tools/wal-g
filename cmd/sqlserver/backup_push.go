// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package sqlserver

import (
	"github.com/spf13/cobra"

	"github.com/lateos-ai/wal-g/internal"
	"github.com/lateos-ai/wal-g/internal/databases/sqlserver"
)

const backupPushShortDescription = "Creates new backup and pushes it to the storage"

var backupPushDatabases []string

var backupUpdateLatest bool

var backupPushCmd = &cobra.Command{
	Use: "backup-push",

	Short: backupPushShortDescription,

	Run: func(cmd *cobra.Command, args []string) {
		internal.ConfigureLimiters()

		sqlserver.HandleBackupPush(backupPushDatabases, backupUpdateLatest)
	},
}

func init() {
	backupPushCmd.PersistentFlags().StringSliceVarP(&backupPushDatabases, "databases", "d", []string{},

		"List of databases to backup. All not-system databases as default")

	backupPushCmd.PersistentFlags().BoolVarP(&backupUpdateLatest, "update-latest", "u", false,

		"Update latest backup instead of creating new one")

	cmd.AddCommand(backupPushCmd)
}
