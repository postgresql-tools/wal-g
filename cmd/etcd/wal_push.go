// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package etcd

import (
	"github.com/spf13/cobra"
	"github.com/wal-g/tracelog"

	"github.com/lateos-ai/wal-g/internal"
	conf "github.com/lateos-ai/wal-g/internal/config"
	"github.com/lateos-ai/wal-g/internal/databases/etcd"
)

const (
	walPushShortDescribtion = "Fetches wals and pushes to storage"
)

var walPushCmd = &cobra.Command{
	Use: "wal-push",

	Short: walPushShortDescribtion,

	Args: cobra.NoArgs,

	PreRun: func(cmd *cobra.Command, args []string) {
		conf.RequiredSettings[conf.ETCDMemberDataDirectory] = true

		err := internal.AssertRequiredSettingsSet()

		tracelog.ErrorLogger.FatalOnError(err)
	},

	Run: func(cmd *cobra.Command, args []string) {
		uploader, err := internal.ConfigureUploader()

		tracelog.ErrorLogger.FatalOnError(err)

		dataDir, err := conf.GetRequiredSetting(conf.ETCDMemberDataDirectory)

		tracelog.ErrorLogger.FatalOnError(err)

		err = etcd.HandleWALPush(cmd.Context(), uploader, dataDir)

		tracelog.ErrorLogger.FatalOnError(err)
	},
}

func init() {
	cmd.AddCommand(walPushCmd)
}
