// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package st

import (
	"github.com/spf13/cobra"

	"github.com/lateos-ai/wal-g/utility"
)

const pgWALsShortDescription = "Moves all PostgreSQL WAL files from one storage to another"

// pgWALsCmd represents the pg-wals command

var pgWALsCmd = &cobra.Command{
	Use: "pg-wals --source='source_storage' [--target='target_storage']",

	Short: pgWALsShortDescription,

	Args: cobra.NoArgs,

	Run: func(_ *cobra.Command, _ []string) {
		transferFiles(utility.WalPath)
	},
}

func init() {
	transferCmd.AddCommand(pgWALsCmd)
}
