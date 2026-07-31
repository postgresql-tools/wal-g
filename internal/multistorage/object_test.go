// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package multistorage

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lateos-ai/wal-g/internal/multistorage/consts"
	"github.com/lateos-ai/wal-g/pkg/storages/storage"
)

func TestGetStorage(t *testing.T) {
	t.Run("provides storage name", func(t *testing.T) {
		obj := multiObject{
			Object: storage.LocalObject{},

			storageName: "some_name",
		}

		name := GetStorage(obj)

		assert.Equal(t, "some_name", name)
	})

	t.Run("provides default name if object is not multiobject", func(t *testing.T) {
		obj := storage.LocalObject{}

		name := GetStorage(obj)

		assert.Equal(t, consts.DefaultStorage, name)
	})
}
