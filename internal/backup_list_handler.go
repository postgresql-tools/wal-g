// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package internal

import (
	"os"

	"github.com/wal-g/tracelog"

	"github.com/lateos-ai/wal-g/internal/printlist"
	"github.com/lateos-ai/wal-g/pkg/storages/storage"
)

func HandleDefaultBackupList(folder storage.Folder, pretty, json bool) {
	backupTimes, err := GetBackups(folder)

	err = FilterOutNoBackupFoundError(err, json)

	tracelog.ErrorLogger.FatalfOnError("Get backups from folder: %v", err)

	SortBackupTimeSlices(backupTimes)

	printableEntities := make([]printlist.Entity, len(backupTimes))

	for i := range backupTimes {
		printableEntities[i] = backupTimes[i]
	}

	err = printlist.List(printableEntities, os.Stdout, pretty, json)

	tracelog.ErrorLogger.FatalfOnError("Print backups: %v", err)
}
