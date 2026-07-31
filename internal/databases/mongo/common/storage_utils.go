// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package common

import (
	"github.com/lateos-ai/wal-g/internal"
	"github.com/lateos-ai/wal-g/internal/databases/mongo/models"
	"github.com/lateos-ai/wal-g/pkg/storages/storage"
	"github.com/lateos-ai/wal-g/utility"
)

const LogicalBackupType = "logical"
const BinaryBackupType = "binary"

func DownloadMetadata(folder storage.Folder, backupName string) (*models.BackupRoutesInfo, error) {
	var metadata models.BackupRoutesInfo
	backup, err := internal.GetBackupByName(backupName, "", folder)
	if err != nil {
		return nil, err
	}
	if err := backup.FetchMetadata(&metadata); err != nil {
		return nil, err
	}

	return &metadata, nil
}

func DownloadSentinel(folder storage.Folder, backupName string) (*models.Backup, error) {
	var sentinel models.Backup
	backup, err := internal.GetBackupByName(backupName, "", folder)
	if err != nil {
		return nil, err
	}
	if err := backup.FetchSentinel(&sentinel); err != nil {
		return nil, err
	}
	if sentinel.BackupName == "" {
		sentinel.BackupName = backupName
	}
	if sentinel.BackupType == "" {
		sentinel.BackupType = LogicalBackupType
	}
	return &sentinel, nil
}

func GetBackupFolder() (backupFolder storage.Folder, err error) {
	st, err := internal.ConfigureStorage()
	if err != nil {
		return nil, err
	}
	return st.RootFolder().GetSubFolder(utility.BaseBackupPath), err
}
