// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package binary

import (
	"strings"

	"github.com/lateos-ai/wal-g/internal"
)

const (
	CollectionPrefix = "collection"
	IndexPrefix      = "index"
)

type DirDatabaseTarBallComposerMaker struct {
	files       internal.BundleFiles
	tarFileSets internal.TarFileSets
}

func NewDirDatabaseTarBallComposerMaker() *DirDatabaseTarBallComposerMaker {
	return &DirDatabaseTarBallComposerMaker{
		files:       &internal.RegularBundleFiles{},
		tarFileSets: internal.NewRegularTarFileSets(),
	}
}

func (maker *DirDatabaseTarBallComposerMaker) Make(bundle *internal.Bundle) (internal.TarBallComposer, error) {
	packer := internal.NewRegularTarBallFilePacker(maker.files, false)
	return internal.NewDirDatabaseTarBallComposer(
		maker.files,
		bundle.TarBallQueue,
		packer,
		maker.tarFileSets,
		bundle.Crypter,
		mongoPathFilter,
	), nil
}

func mongoPathFilter(path string) bool {
	return strings.Contains(path, CollectionPrefix) || strings.Contains(path, IndexPrefix)
}
