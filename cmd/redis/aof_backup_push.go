// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package redis

import (
	"github.com/spf13/cobra"
	"github.com/wal-g/tracelog"

	"github.com/lateos-ai/wal-g/internal"
	conf "github.com/lateos-ai/wal-g/internal/config"
	"github.com/lateos-ai/wal-g/internal/databases/redis"
	"github.com/lateos-ai/wal-g/internal/databases/redis/archive"
	client "github.com/lateos-ai/wal-g/internal/databases/redis/client"
	"github.com/lateos-ai/wal-g/utility"
)

var (
	sharded = false
)

const (
	aofBackupPushCommandName = "aof-backup-push"

	//nolint:unused
	shardedShortDescription = "Turns on collecting slots info (use for sharded restore of sharded cluster only)"

	shardedFlag = "sharded"

	shardedShorthand = "s"
)

var aofBackupPushCmd = &cobra.Command{
	Use: aofBackupPushCommandName,

	Short: "Creates redis aof backup and pushes it to storage without local disk",

	Args: cobra.NoArgs,

	Run: func(cmd *cobra.Command, args []string) {
		internal.ConfigureLimiters()

		ctx := cmd.Context()

		uploader, err := internal.ConfigureUploader()

		tracelog.ErrorLogger.FatalOnError(err)

		uploader.ChangeDirectory(utility.BaseBackupPath + "/")

		memoryDataGetter := client.NewServerDataGetter()

		processName, _ := conf.GetSetting(conf.RedisServerProcessName)

		versionParser := archive.NewVersionParser(processName)

		metaConstructor := archive.NewBackupRedisMetaConstructor(

			ctx,

			uploader.Folder(),

			permanent,

			archive.AOFBackupType,

			versionParser,

			memoryDataGetter,
		)

		pushArgs := redis.AOFBackupPushArgs{
			Uploader: uploader,

			MetaConstructor: metaConstructor,

			Sharded: sharded,
		}

		err = redis.HandleAOFBackupPush(ctx, pushArgs)

		tracelog.ErrorLogger.FatalOnError(err)
	},
}

func init() {
	aofBackupPushCmd.Flags().BoolVarP(&permanent, PermanentFlag, PermanentShorthand, false, "Pushes permanent backup")

	aofBackupPushCmd.Flags().BoolVarP(&sharded, shardedFlag, shardedShorthand, false, "Pushes sharded backup")

	cmd.AddCommand(aofBackupPushCmd)
}
