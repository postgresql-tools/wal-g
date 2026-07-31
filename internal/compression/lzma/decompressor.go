// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package lzma

import (
	"io"

	"github.com/ulikunitz/xz/lzma"

	"github.com/lateos-ai/wal-g/internal/compression/computils"
)

type Decompressor struct{}

func (decompressor Decompressor) Decompress(src io.Reader) (io.ReadCloser, error) {
	lzReader, err := lzma.NewReader(computils.NewUntilEOFReader(src))
	if err != nil {
		return nil, err
	}
	return io.NopCloser(lzReader), nil
}

func (decompressor Decompressor) FileExtension() string {
	return FileExtension
}
