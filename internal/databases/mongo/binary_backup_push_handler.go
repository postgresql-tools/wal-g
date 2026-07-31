// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package mongo

import (
	"context"
	"time"

	"github.com/lateos-ai/wal-g/internal"
	conf "github.com/lateos-ai/wal-g/internal/config"
	"github.com/lateos-ai/wal-g/internal/databases/mongo/binary"
	"github.com/lateos-ai/wal-g/utility"
)

type HandleBinaryBackupPushArgs struct {
	AppName       string
	CountJournals bool
	Permanent     bool
	SkipMetadata  bool
	UserDataRaw   string
}

func HandleBinaryBackupPush(ctx context.Context, args HandleBinaryBackupPushArgs) error {
	mongodbURI, err := conf.GetRequiredSetting(conf.MongoDBUriSetting)
	if err != nil {
		return err
	}
	mongodService, err := binary.CreateMongodService(ctx, args.AppName, mongodbURI, 10*time.Minute)
	if err != nil {
		return err
	}

	uploader, err := internal.ConfigureUploader()
	if err != nil {
		return err
	}
	uploader.ChangeDirectory(utility.BaseBackupPath + "/")

	backupService, err := binary.CreateBackupService(ctx, mongodService, uploader)
	if err != nil {
		return err
	}

	var userData interface{}
	if args.UserDataRaw == "" {
		if userData, err = internal.GetSentinelUserData(); err != nil {
			return err
		}
	} else {
		if userData, err = internal.UnmarshalSentinelUserData(args.UserDataRaw); err != nil {
			return err
		}
	}

	doBackupArgs := binary.DoBackupArgs{
		BackupName:    binary.GenerateNewBackupName(),
		CountJournals: args.CountJournals,
		Permanent:     args.Permanent,
		SkipMetadata:  args.SkipMetadata,
		UserData:      userData,
	}
	return backupService.DoBackup(doBackupArgs)
}
