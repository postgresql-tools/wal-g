// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package postgres_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lateos-ai/wal-g/internal/databases/postgres"
	"github.com/lateos-ai/wal-g/internal/walparser"
)

func TestSaveLoadDeltaFile(t *testing.T) {
	deltaFile := &postgres.DeltaFile{
		Locations: []walparser.BlockLocation{
			*walparser.NewBlockLocation(1, 2, 3, 4),

			*walparser.NewBlockLocation(5, 6, 7, 8),
		},

		WalParser: walparser.NewWalParser(),
	}

	var deltaFileData bytes.Buffer

	err := deltaFile.Save(&deltaFileData)

	assert.NoError(t, err)

	loadedDeltaFile, err := postgres.LoadDeltaFile(&deltaFileData)

	assert.NoError(t, err)

	assert.Equal(t, deltaFile, loadedDeltaFile)
}
