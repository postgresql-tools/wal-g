// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package sqlserver

import (
	"github.com/spf13/cobra"

	"github.com/lateos-ai/wal-g/internal/databases/sqlserver"
)

const databaseListShortDescription = "List datbases in the backup"

var databaseListCmd = &cobra.Command{
	Use: "database-list",

	Short: databaseListShortDescription,

	Run: func(cmd *cobra.Command, args []string) {
		sqlserver.HandleDatabaseList(args[0])
	},
}

func init() {
	cmd.AddCommand(databaseListCmd)
}
