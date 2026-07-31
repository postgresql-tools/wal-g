// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package postgres_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lateos-ai/wal-g/internal/databases/postgres"
	"github.com/lateos-ai/wal-g/internal/walparser"
)

const (
	WalgTestDataFolderPath = "../../../test/testdata"

	WalFilename = "00000001000000000000007C"

	LastWalFilename = "00000001000000000000007F"

	DeltaFilename = "000000010000000000000070_delta"

	DeltaFilename2 = "0000000300000000000000A0_delta"
)

var TestLocation = *walparser.NewBlockLocation(1, 2, 3, 4)

func TestGetDeltaFileNameFor(t *testing.T) {
	deltaFilename, err := postgres.GetDeltaFilenameFor(WalFilename)

	assert.NoError(t, err)

	assert.Equal(t, DeltaFilename, deltaFilename)
}

func TestGetPositionInDelta(t *testing.T) {
	assert.Equal(t, 12, postgres.GetPositionInDelta(WalFilename))
}
