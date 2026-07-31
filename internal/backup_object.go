// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package internal

import (
	"time"

	"github.com/lateos-ai/wal-g/internal/multistorage/consts"
	"github.com/lateos-ai/wal-g/pkg/storages/storage"
	"github.com/lateos-ai/wal-g/utility"
)

func NewDefaultBackupObject(object storage.Object) BackupObject {
	return DefaultBackupObject{object}
}

var _ BackupObject = DefaultBackupObject{}

type DefaultBackupObject struct {
	storage.Object
}

func (o DefaultBackupObject) GetBackupName() string {
	return utility.StripRightmostBackupName(o.GetName())
}

func (o DefaultBackupObject) GetBaseBackupName() string {
	return o.GetBackupName()
}

func (o DefaultBackupObject) GetIncrementFromName() string {
	return o.GetBackupName()
}

func (o DefaultBackupObject) IsFullBackup() bool {
	return true
}

func (o DefaultBackupObject) GetBackupTime() time.Time {
	return o.GetLastModified()
}

func (o DefaultBackupObject) GetStorage() string {
	return consts.DefaultStorage
}
