// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package pg

import (
	"github.com/spf13/cobra"
	"github.com/wal-g/tracelog"

	"github.com/lateos-ai/wal-g/internal/databases/postgres/pgbackrest"
)

var pgbackrestWalFetchCmd = &cobra.Command{
	Use: "wal-fetch wal_name destination_filename",

	Short: WalFetchShortDescription,

	Args: cobra.ExactArgs(2),

	Run: func(cmd *cobra.Command, args []string) {
		folder, stanza := configurePgbackrestSettings()

		err := pgbackrest.HandleWalFetch(folder, stanza, args[0], args[1])

		tracelog.ErrorLogger.FatalOnError(err)
	},
}

func init() {
	pgbackrestCmd.AddCommand(pgbackrestWalFetchCmd)
}
