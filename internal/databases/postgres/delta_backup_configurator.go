// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package postgres

import (
	"github.com/wal-g/tracelog"

	"github.com/lateos-ai/wal-g/internal"
	"github.com/lateos-ai/wal-g/pkg/storages/storage"
	"github.com/lateos-ai/wal-g/utility"
)

type DeltaBackupConfigurator interface {
	Configure(folder storage.Folder, isPermanent bool) (PrevBackupInfo, DeltaDecision, error)
}

type RegularDeltaBackupConfigurator struct {
	deltaBaseSelector internal.BackupSelector
}

func NewRegularDeltaBackupConfigurator(deltaBaseSelector internal.BackupSelector) RegularDeltaBackupConfigurator {
	return RegularDeltaBackupConfigurator{deltaBaseSelector}
}

func (c RegularDeltaBackupConfigurator) Configure(

	folder storage.Folder, isPermanent bool,

) (prevBackupInfo PrevBackupInfo, decision DeltaDecision, err error) {
	maxDeltas, fromFull := internal.GetDeltaConfig()

	if maxDeltas == 0 {
		return PrevBackupInfo{}, DeltaDecision{}, nil
	}

	baseBackupFolder := folder.GetSubFolder(utility.BaseBackupPath)

	previousBackup, err := c.deltaBaseSelector.Select(folder)

	if err != nil {
		if _, ok := err.(internal.NoBackupsFoundError); ok {
			tracelog.InfoLogger.Println("Couldn't find previous backup. Doing full backup.")

			return PrevBackupInfo{}, DeltaDecision{}, nil
		}

		return PrevBackupInfo{}, DeltaDecision{}, err
	}

	previousPgBackup := ToPgBackup(previousBackup)

	prevBackupSentinelDto, err := previousPgBackup.GetSentinel()

	tracelog.ErrorLogger.FatalOnError(err)

	// Walk the chain in storage rather than trusting the count written into the
	// base backup's sentinel, so a missing or stale count cannot let the chain
	// grow past its limit unnoticed.
	chain, err := ResolveDeltaChain(baseBackupFolder, previousPgBackup.Name)

	if err != nil {
		return PrevBackupInfo{}, DeltaDecision{}, err
	}

	if !chain.Broken {
		warnOnDepthDisagreement(previousPgBackup.Name, prevBackupSentinelDto.IncrementCount, chain.Depth)
	}

	previousBackupMeta, metaErr := previousPgBackup.FetchMeta()

	if metaErr != nil {
		tracelog.InfoLogger.Printf(

			"Failed to get previous backup metadata: %s. Doing full backup.\n", metaErr.Error())

		return PrevBackupInfo{}, DeltaDecision{}, nil
	}

	decision = DecideDelta(DeltaDecisionInput{
		Chain: chain,

		RecordedCount: prevBackupSentinelDto.IncrementCount,

		MaxDeltas: maxDeltas,

		FromFull: fromFull,

		BaseIsPermanent: previousBackupMeta.IsPermanent,

		BaseHasStartLSN: prevBackupSentinelDto.BackupStartLSN != nil,

		NextIsPermanent: isPermanent,
	})

	decision.LogDecision(previousPgBackup.Name)

	if !decision.UseDelta {
		return PrevBackupInfo{}, decision, nil
	}

	if fromFull {
		tracelog.InfoLogger.Println("Delta will be made from full backup.")

		prevName := previousPgBackup.Name

		if prevBackupSentinelDto.IncrementFullName != nil {
			prevName = *prevBackupSentinelDto.IncrementFullName
		}

		previousPgBackup, err = NewBackup(baseBackupFolder, prevName)

		if err != nil {
			return PrevBackupInfo{}, DeltaDecision{}, err
		}

		prevBackupSentinelDto, err = previousPgBackup.GetSentinel()

		if err != nil {
			return PrevBackupInfo{}, DeltaDecision{}, err
		}
	}

	tracelog.InfoLogger.Printf("Delta backup from %v with LSN %s.\n", previousPgBackup.Name,

		*prevBackupSentinelDto.BackupStartLSN)

	prevBackupInfo.name = previousPgBackup.Name

	prevBackupInfo.sentinelDto, prevBackupInfo.filesMetadataDto, err = previousPgBackup.GetSentinelAndFilesMetadata()

	return prevBackupInfo, decision, err
}

type CatchupDeltaBackupConfigurator struct {
	fakePrevSentinel BackupSentinelDto
}

func NewCatchupDeltaBackupConfigurator(fakePreviousBackupSentinelDto BackupSentinelDto) CatchupDeltaBackupConfigurator {
	return CatchupDeltaBackupConfigurator{
		fakePrevSentinel: fakePreviousBackupSentinelDto,
	}
}

func (c CatchupDeltaBackupConfigurator) Configure(storage.Folder, bool) (prevBackupInfo PrevBackupInfo, decision DeltaDecision, err error) {
	prevBackupInfo.sentinelDto = c.fakePrevSentinel

	prevBackupInfo.filesMetadataDto = FilesMetadataDto{}

	// Catchup always increments from the fake sentinel it was handed, so there is
	// no chain in storage to walk and no limit to apply.
	return prevBackupInfo, DeltaDecision{UseDelta: true, Depth: 1}, nil
}
