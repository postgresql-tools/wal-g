// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package multistorage

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lateos-ai/wal-g/pkg/storages/memory"
)

func TestNamedFolder_GetSubFolder(t *testing.T) {
	t.Run("change path of subfolder", func(t *testing.T) {
		src := NamedFolder{
			Folder: memory.NewFolder("test", memory.NewKVS()),
		}

		got := src.GetSubFolder("subfolder")

		assert.Equal(t, "test/subfolder/", got.GetPath())
	})

	t.Run("do not get subfolder if path is empty", func(t *testing.T) {
		src := NamedFolder{
			Folder: memory.NewFolder("test", memory.NewKVS()),

			StorageName: "abc",
		}

		got := src.GetSubFolder("")

		assert.Equal(t, src, got)
	})

	t.Run("preserve storage name", func(t *testing.T) {
		src := NamedFolder{
			Folder: memory.NewFolder("test", memory.NewKVS()),

			StorageName: "abc",
		}

		got := src.GetSubFolder("subfolder")

		assert.Equal(t, src.StorageName, got.StorageName)
	})
}
