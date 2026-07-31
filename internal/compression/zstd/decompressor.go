// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package zstd

import (
	"io"

	"github.com/klauspost/compress/zstd"

	"github.com/lateos-ai/wal-g/internal/compression/computils"
)

type Decompressor struct{}

func (decompressor Decompressor) Decompress(src io.Reader) (io.ReadCloser, error) {
	zstdReader, err := zstd.NewReader(computils.NewUntilEOFReader(src))
	if err != nil {
		return nil, err
	}
	return zstdReader.IOReadCloser(), nil
}

func (decompressor Decompressor) FileExtension() string {
	return FileExtension
}
