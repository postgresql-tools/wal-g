// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package postgres

import (
	"os"

	"github.com/wal-g/tracelog"

	"github.com/lateos-ai/wal-g/internal"
	"github.com/lateos-ai/wal-g/internal/printlist"
	"github.com/lateos-ai/wal-g/pkg/storages/storage"
)

func HandleDetailedBackupList(folder storage.Folder, pretty bool, json bool) {
	backups, err := internal.GetBackups(folder)

	err = internal.FilterOutNoBackupFoundError(err, json)

	tracelog.ErrorLogger.FatalfOnError("Get backups from folder: %v", err)

	backupDetails, err := GetBackupsDetails(folder, backups)

	tracelog.ErrorLogger.FatalOnError(err)

	SortBackupDetails(backupDetails)

	printableEntities := make([]printlist.Entity, len(backupDetails))

	for i := range backupDetails {
		printableEntities[i] = &backupDetails[i]
	}

	err = printlist.List(printableEntities, os.Stdout, pretty, json)

	tracelog.ErrorLogger.FatalfOnError("Print backups: %v", err)
}
