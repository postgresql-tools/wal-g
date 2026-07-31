// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package pgbackrest

import (
	"fmt"
	"os"

	"github.com/wal-g/tracelog"

	"github.com/lateos-ai/wal-g/internal"
	"github.com/lateos-ai/wal-g/internal/printlist"
	"github.com/lateos-ai/wal-g/pkg/storages/storage"
)

// TODO: unit tests

func HandleBackupList(folder storage.Folder, stanza string, detailed bool, pretty bool, json bool) error {
	backupTimes, err := GetBackupList(folder, stanza)

	if len(backupTimes) == 0 {
		tracelog.InfoLogger.Println("No backups found")

		return nil
	}

	if err != nil {
		return err
	}

	internal.SortBackupTimeSlices(backupTimes)

	printableEntities := make([]printlist.Entity, len(backupTimes))

	for i := range backupTimes {
		if detailed {
			details, err := GetBackupDetails(folder, stanza, backupTimes[i].BackupName)

			if err != nil {
				return fmt.Errorf("get backup details: %w", err)
			}

			printableEntities[i] = details
		} else {
			printableEntities[i] = backupTimes[i]
		}
	}

	err = printlist.List(printableEntities, os.Stdout, pretty, json)

	if err != nil {
		return fmt.Errorf("print backups: %w", err)
	}

	return nil
}
