// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package asm

import "github.com/lateos-ai/wal-g/internal/fsutil"

type DataFolderASM struct {
	folder fsutil.DataFolder
}

func NewDataFolderASM(folder fsutil.DataFolder) DataFolderASM {
	return DataFolderASM{
		folder: folder,
	}
}

func (asm DataFolderASM) IsWalAlreadyUploaded(walFilePath string) bool {
	walFilePath = GetOnlyWalName(walFilePath)
	return asm.folder.FileExists(walFilePath)
}

func (asm DataFolderASM) MarkWalUploaded(walFilePath string) error {
	walFilePath = GetOnlyWalName(walFilePath)
	return asm.folder.CreateFile(walFilePath)
}

func (asm DataFolderASM) UnmarkWalFile(walFilePath string) error {
	walFilePath = GetOnlyWalName(walFilePath)
	return asm.folder.DeleteFile(walFilePath)
}

func (asm DataFolderASM) RenameReady(walFileName string) error {
	return asm.folder.RenameFile(walFileName+".ready", walFileName+".done")
}
