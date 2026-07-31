// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package pg

import (
	"github.com/spf13/cobra"

	"github.com/lateos-ai/wal-g/internal/databases/postgres"
)

const (
	catchupSendShortDescription = "Sends incremental backup to standby"
)

var (
	catchupSendCmd = &cobra.Command{
		Use: "catchup-send PGDATA host:port",

		Short: catchupSendShortDescription,

		Args: cobra.ExactArgs(2),

		Run: func(cmd *cobra.Command, args []string) {
			postgres.HandleCatchupSend(args[0], args[1])
		},

		Annotations: map[string]string{"NoStorage": ""},
	}
)

func init() {
	Cmd.AddCommand(catchupSendCmd)
}
