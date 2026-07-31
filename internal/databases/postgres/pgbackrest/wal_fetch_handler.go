// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package pgbackrest

import (
	"errors"
	"path/filepath"
	"strings"

	"github.com/lateos-ai/wal-g/internal"
	"github.com/lateos-ai/wal-g/pkg/storages/storage"
)

func HandleWalFetch(folder storage.Folder, stanza string, walFileName string, location string) error {
	archiveName, err := GetArchiveName(folder, stanza)
	if err != nil {
		return err
	}

	archiveFolder := folder.GetSubFolder(WalArchivePath).GetSubFolder(stanza).GetSubFolder(*archiveName)
	if strings.HasSuffix(walFileName, ".history") {
		return internal.DownloadFileTo(internal.NewFolderReader(archiveFolder), walFileName, location)
	}

	subdirectoryName := walFileName[0:16]
	walFolder := archiveFolder.GetSubFolder(subdirectoryName)
	if strings.HasSuffix(walFileName, ".backup") {
		return internal.DownloadFileTo(internal.NewFolderReader(walFolder), walFileName, location)
	}
	fileList, _, err := walFolder.ListFolder()
	if err != nil {
		return err
	}

	for _, file := range fileList {
		fileName := file.GetName()
		if strings.HasPrefix(fileName, walFileName) {
			return internal.DownloadFileTo(internal.NewFolderReader(walFolder), strings.TrimSuffix(fileName, filepath.Ext(fileName)), location)
		}
	}

	return errors.New("File " + walFileName + " not found in storage")
}
