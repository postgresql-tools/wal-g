// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package memory

import (
	"testing"

	"github.com/lateos-ai/wal-g/pkg/storages/storage"
)

func TestMemoryFolder(t *testing.T) {
	storage.RunFolderTest(NewFolder("in_memory/", NewKVS()), t)
}
