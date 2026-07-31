// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package sqlserver

import (
	"github.com/spf13/cobra"
	"github.com/wal-g/tracelog"

	"github.com/lateos-ai/wal-g/internal"
	"github.com/lateos-ai/wal-g/internal/databases/sqlserver"
)

const proxyShortDescription = "Run local azure blob emulator"

// proxyCmd represents the streamFetch command

var proxyCmd = &cobra.Command{
	Use: "proxy",

	Short: proxyShortDescription,

	Args: cobra.ExactArgs(0),

	Run: func(cmd *cobra.Command, args []string) {
		storage, err := internal.ConfigureStorage()

		tracelog.ErrorLogger.FatalOnError(err)

		sqlserver.RunProxy(storage.RootFolder())
	},
}

func init() {
	cmd.AddCommand(proxyCmd)
}
