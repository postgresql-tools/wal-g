// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package mysql

import (
	"github.com/wal-g/tracelog"

	"github.com/lateos-ai/wal-g/internal"
)

// MarkBackup marks a backup as permanent or impermanent

func MarkBackup(uploader internal.Uploader, backupName string, toPermanent bool) {
	tracelog.InfoLogger.Printf("Retrieving previous related backups to be marked: toPermanent=%t", toPermanent)

	internal.HandleBackupMark(uploader, backupName, toPermanent, NewGenericMetaInteractor())
}
