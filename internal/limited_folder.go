// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package internal

import (
	"context"
	"io"

	"golang.org/x/time/rate"

	"github.com/lateos-ai/wal-g/internal/ioextensions"
	"github.com/lateos-ai/wal-g/internal/limiters"
	"github.com/lateos-ai/wal-g/pkg/storages/storage"
)

type LimitedFolder struct {
	storage.Folder
	limiter *rate.Limiter
}

func NewLimitedFolder(folder storage.Folder, limiter *rate.Limiter) *LimitedFolder {
	return &LimitedFolder{Folder: folder, limiter: limiter}
}

func (lf *LimitedFolder) GetSubFolder(subFolderRelativePath string) storage.Folder {
	folder := lf.Folder.GetSubFolder(subFolderRelativePath)
	return NewLimitedFolder(folder, lf.limiter)
}

func (lf *LimitedFolder) ReadObject(objectRelativePath string) (io.ReadCloser, error) {
	readCloser, err := lf.Folder.ReadObject(objectRelativePath)
	if err != nil {
		return nil, err
	}
	return ioextensions.ReadCascadeCloser{
		Reader: limiters.NewReader(context.Background(), readCloser, lf.limiter),
		Closer: readCloser,
	}, nil
}

func (lf *LimitedFolder) PutObject(name string, content io.Reader) error {
	return lf.PutObjectWithContext(context.Background(), name, content)
}

func (lf *LimitedFolder) PutObjectWithContext(ctx context.Context, name string, content io.Reader) error {
	limitedReader := limiters.NewReader(ctx, content, lf.limiter)
	return lf.Folder.PutObjectWithContext(ctx, name, limitedReader)
}

// SetShowAllVersions delegates the "show all versions" toggle to the underlying folder (if supported).
// This is used by storage tools (e.g. `wal-g st ls --all-versions`) and must work even when the folder is wrapped.
func (lf *LimitedFolder) SetShowAllVersions(show bool) {
	storage.SetShowAllVersions(lf.Folder, show)
}
