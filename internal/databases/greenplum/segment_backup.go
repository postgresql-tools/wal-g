// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package greenplum

import (
	"github.com/lateos-ai/wal-g/internal"
	"github.com/lateos-ai/wal-g/internal/databases/greenplum/pax"
	"github.com/lateos-ai/wal-g/internal/databases/postgres"
	"github.com/lateos-ai/wal-g/pkg/storages/storage"
)

type SegBackup struct {
	postgres.Backup

	AoFilesMetadataDto  *AOFilesMetadataDTO
	PaxFilesMetadataDto *pax.FilesMetadataDTO
}

func NewSegBackup(baseBackupFolder storage.Folder, name, storage string) (SegBackup, error) {
	pgBackup, err := postgres.NewBackupInStorage(baseBackupFolder, name, storage)
	if err != nil {
		return SegBackup{}, err
	}
	return SegBackup{
		Backup: pgBackup,
	}, nil
}

func ToGpSegBackup(source postgres.Backup) (output SegBackup) {
	return SegBackup{
		Backup: source,
	}
}

func (backup *SegBackup) LoadAoFilesMetadata() (*AOFilesMetadataDTO, error) {
	if backup.AoFilesMetadataDto != nil {
		return backup.AoFilesMetadataDto, nil
	}

	var meta AOFilesMetadataDTO
	err := internal.FetchDto(backup.Folder, &meta, getAOFilesMetadataPath(backup.Name))
	if err != nil {
		return nil, err
	}

	backup.AoFilesMetadataDto = &meta
	return backup.AoFilesMetadataDto, nil
}

func (backup *SegBackup) LoadPaxFilesMetadata() (*pax.FilesMetadataDTO, error) {
	if backup.PaxFilesMetadataDto != nil {
		return backup.PaxFilesMetadataDto, nil
	}

	var meta pax.FilesMetadataDTO
	err := internal.FetchDto(backup.Folder, &meta, pax.GetFilesMetadataPath(backup.Name))
	if err != nil {
		return nil, err
	}

	backup.PaxFilesMetadataDto = &meta
	return backup.PaxFilesMetadataDto, nil
}
