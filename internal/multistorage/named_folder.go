// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package multistorage

import (
	"github.com/lateos-ai/wal-g/pkg/storages/storage"
)

type NamedFolder struct {
	storage.Folder
	StorageName string
}

func (nf NamedFolder) GetSubFolder(path string) NamedFolder {
	if path == "" {
		return nf
	}
	cpy := nf
	cpy.Folder = cpy.Folder.GetSubFolder(path)
	return cpy
}
