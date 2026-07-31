// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package mongo

import (
	"encoding/json"
	"io"

	"github.com/lateos-ai/wal-g/internal/databases/mongo/common"
	"github.com/lateos-ai/wal-g/pkg/storages/storage"
)

// HandleBackupShow prints sentinel contents.
func HandleBackupShow(backupFolder storage.Folder, backupName string, output io.Writer, pretty bool) (err error) {
	sentinel, err := common.DownloadSentinel(backupFolder, backupName)
	if err != nil {
		return err
	}

	encoder := json.NewEncoder(output)
	if pretty {
		encoder.SetIndent("", "    ")
	}
	return encoder.Encode(sentinel)
}
