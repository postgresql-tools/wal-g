// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package pg

import (
	"github.com/spf13/cobra"
	"github.com/wal-g/tracelog"

	"github.com/lateos-ai/wal-g/internal"
	"github.com/lateos-ai/wal-g/internal/databases/postgres"
)

const (
	CatchupFetchShortDescription = "Fetches an incremental backup from storage"

	UseNewUnwrapDescription = "Use the new implementation of catchup unwrap (beta)"
)

var useNewUnwrap bool

// catchupFetchCmd represents the catchup-fetch command

var catchupFetchCmd = &cobra.Command{
	Use: "catchup-fetch PGDATA backup_name",

	Short: CatchupFetchShortDescription, // TODO : improve description

	Args: cobra.ExactArgs(2),

	Run: func(cmd *cobra.Command, args []string) {
		internal.ConfigureLimiters()

		storage, err := internal.ConfigureStorage()

		tracelog.ErrorLogger.FatalOnError(err)

		postgres.HandleCatchupFetch(storage.RootFolder(), args[0], args[1], useNewUnwrap)
	},
}

func init() {
	catchupFetchCmd.Flags().BoolVar(&useNewUnwrap, "use-new-unwrap",

		false, UseNewUnwrapDescription)

	Cmd.AddCommand(catchupFetchCmd)
}
