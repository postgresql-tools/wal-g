// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package etcd_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/lateos-ai/wal-g/internal"
	"github.com/lateos-ai/wal-g/internal/config"
	"github.com/lateos-ai/wal-g/internal/databases/etcd"
	"github.com/lateos-ai/wal-g/testtools"
)

func init() {
	internal.ConfigureSettings("")

	config.InitConfig()

	config.Configure()
}

func TestFetch(t *testing.T) {
	folder := testtools.CreateMockStorageFolder()

	backupName := "test"

	data := "Data"

	date := time.Date(2002, 3, 21, 0, 0, 0, 0, time.UTC)

	testObject := etcd.StreamSentinelDto{
		StartLocalTime: date,

		IsPermanent: false,

		UserData: data,
	}

	var expectedResult = internal.GenericMetadata{
		BackupName: backupName,

		StartTime: date,

		IsPermanent: false,

		UserData: data,
	}

	_ = internal.UploadDto(folder, testObject, internal.SentinelNameFromBackup(backupName))

	actualResult, err := etcd.NewGenericMetaFetcher().Fetch(backupName, folder)

	assert.NoError(t, err)

	isEqualTimeStart := expectedResult.StartTime.Equal(actualResult.StartTime)

	assert.True(t, isEqualTimeStart)

	expectedResult.StartTime = actualResult.StartTime

	expectedResult.FinishTime = actualResult.FinishTime

	assert.NoError(t, err)

	assert.Equal(t, expectedResult, actualResult)
}
