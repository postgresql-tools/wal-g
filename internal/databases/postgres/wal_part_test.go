// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package postgres_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lateos-ai/wal-g/internal/databases/postgres"
)

func TestSaveLoadWalPart(t *testing.T) {
	walPart := postgres.NewWalPart(postgres.WalTailType, 5, []byte{1, 2, 3, 4, 5})

	var walPartData bytes.Buffer

	err := walPart.Save(&walPartData)

	assert.NoError(t, err)

	loadedWalPart, err := postgres.LoadWalPart(&walPartData)

	assert.NoError(t, err)

	assert.Equal(t, walPart, loadedWalPart)
}
