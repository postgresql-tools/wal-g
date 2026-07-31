// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package greenplum

import (
	"github.com/lateos-ai/wal-g/internal"
	"github.com/lateos-ai/wal-g/internal/databases/postgres"
)

type ExtractProviderImpl struct {
	FilesToExtractProviderImpl
}

func (t ExtractProviderImpl) Get(
	backup postgres.Backup,
	filesToUnwrap map[string]bool,
	skipRedundantTars bool,
	dbDataDir string,
	createNewIncrementalFiles bool,
) (postgres.IncrementalTarInterpreter, []internal.ReaderMaker, []internal.ReaderMaker, error) {
	segBackup := ToGpSegBackup(backup)

	interpreter, err := t.getTarInterpreter(dbDataDir, segBackup, filesToUnwrap, createNewIncrementalFiles)
	if err != nil {
		return nil, nil, nil, err
	}

	concurrentTarsToExtract, sequentialTarsToExtract, err := t.FilesToExtractProviderImpl.Get(segBackup, filesToUnwrap, skipRedundantTars)
	return interpreter, concurrentTarsToExtract, sequentialTarsToExtract, err
}

func (t ExtractProviderImpl) getTarInterpreter(dbDataDir string, backup SegBackup,
	filesToUnwrap map[string]bool, createNewIncrementalFiles bool) (*IncrementalTarInterpreter, error) {
	_, err := backup.LoadAoFilesMetadata()
	if err != nil {
		return nil, err
	}

	_, _, err = backup.GetSentinelAndFilesMetadata()
	if err != nil {
		return nil, err
	}

	return NewIncrementalTarInterpreter(dbDataDir, *backup.SentinelDto, *backup.FilesMetadataDto, *backup.AoFilesMetadataDto,
		filesToUnwrap, createNewIncrementalFiles), nil
}
