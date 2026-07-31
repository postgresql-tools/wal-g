// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package internal

import (
	"slices"
	"time"
)

const MaxCorruptBlocksInFileDesc int = 10

type BackupFileDescription struct {
	IsIncremented bool // should never be both incremented and Skipped
	IsSkipped     bool
	MTime         time.Time
	CorruptBlocks *CorruptBlocksInfo `json:",omitempty"`
	UpdatesCount  uint64
	SHA256        string `json:"sha256,omitempty"`
}

func NewBackupFileDescription(isIncremented, isSkipped bool, modTime time.Time) *BackupFileDescription {
	return &BackupFileDescription{IsIncremented: isIncremented, IsSkipped: isSkipped, MTime: modTime}
}

type CorruptBlocksInfo struct {
	CorruptBlocksCount int
	SomeCorruptBlocks  []uint32
}

func (desc *BackupFileDescription) SetCorruptBlocks(corruptBlockNumbers []uint32, storeAllBlocks bool) {
	if len(corruptBlockNumbers) == 0 {
		return
	}
	slices.Sort(corruptBlockNumbers)

	corruptBlocksCount := len(corruptBlockNumbers)
	// write no more than MaxCorruptBlocksInFileDesc
	someCorruptBlocks := make([]uint32, 0)
	for idx, blockNo := range corruptBlockNumbers {
		if !storeAllBlocks && idx >= MaxCorruptBlocksInFileDesc {
			break
		}
		someCorruptBlocks = append(someCorruptBlocks, blockNo)
	}
	desc.CorruptBlocks = &CorruptBlocksInfo{
		CorruptBlocksCount: corruptBlocksCount,
		SomeCorruptBlocks:  someCorruptBlocks,
	}
}

type BackupFileList map[string]BackupFileDescription
