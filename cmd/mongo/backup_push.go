// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package mongo

import (
	"os"

	"github.com/spf13/cobra"
	"github.com/wal-g/tracelog"

	"github.com/lateos-ai/wal-g/internal"
	conf "github.com/lateos-ai/wal-g/internal/config"
	"github.com/lateos-ai/wal-g/internal/databases/mongo"
	"github.com/lateos-ai/wal-g/internal/databases/mongo/archive"
	"github.com/lateos-ai/wal-g/internal/databases/mongo/client"
	"github.com/lateos-ai/wal-g/utility"
)

const (
	backupPushShortDescription = "Pushes backup to storage"

	PermanentFlag = "permanent"

	PermanentShorthand = "p"
)

var (
	permanent = false
)

// backupPushCmd represents the backupPush command

var backupPushCmd = &cobra.Command{
	Use: "backup-push",

	Short: backupPushShortDescription,

	Args: cobra.NoArgs,

	Run: func(cmd *cobra.Command, args []string) {
		internal.ConfigureLimiters()

		ctx := cmd.Context()

		mongodbURL, err := conf.GetRequiredSetting(conf.MongoDBUriSetting)

		tracelog.ErrorLogger.FatalOnError(err)

		// set up mongodb client and oplog fetcher

		mongoClient, err := client.NewMongoClient(ctx, mongodbURL)

		tracelog.ErrorLogger.FatalOnError(err)

		uplProvider, err := internal.ConfigureSplitUploader()

		tracelog.ErrorLogger.FatalOnError(err)

		uplProvider.ChangeDirectory(utility.BaseBackupPath)

		backupCmd, err := internal.GetCommandSettingContext(ctx, conf.NameStreamCreateCmd)

		tracelog.ErrorLogger.FatalOnError(err)

		backupCmd.Stderr = os.Stderr

		uploader := archive.NewStorageUploader(uplProvider)

		metaConstructor := archive.NewBackupMongoMetaConstructor(ctx, mongoClient, uplProvider.Folder(), permanent)

		err = mongo.HandleBackupPush(uploader, metaConstructor, backupCmd)

		tracelog.ErrorLogger.FatalfOnError("Backup creation failed: %v", err)
	},

	PreRun: func(cmd *cobra.Command, args []string) {
		conf.RequiredSettings[conf.NameStreamCreateCmd] = true

		err := internal.AssertRequiredSettingsSet()

		tracelog.ErrorLogger.FatalOnError(err)
	},
}

func init() {
	backupPushCmd.Flags().BoolVarP(&permanent, PermanentFlag, PermanentShorthand, false, "Pushes permanent backup")

	cmd.AddCommand(backupPushCmd)
}
