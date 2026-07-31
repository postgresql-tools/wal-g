// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package pg

import (
	"github.com/spf13/cobra"

	"github.com/lateos-ai/wal-g/internal"
	"github.com/lateos-ai/wal-g/internal/databases/postgres"
)

const (
	catchupPushShortDescription = "Creates incremental backup from lsn"
)

var (

	// catchupPushCmd represents the catchup-push command

	catchupPushCmd = &cobra.Command{
		Use: "catchup-push PGDATA --from-lsn LSN",

		Short: catchupPushShortDescription,

		Args: cobra.ExactArgs(1),

		Run: func(cmd *cobra.Command, args []string) {
			internal.ConfigureLimiters()

			postgres.HandleCatchupPush(cmd.Context(), args[0], postgres.LSN(fromLSN))
		},
	}

	fromLSN uint64
)

func init() {
	Cmd.AddCommand(catchupPushCmd)

	catchupPushCmd.Flags().Uint64Var(&fromLSN, "from-lsn", 0, "LSN to start incremental backup")
}
