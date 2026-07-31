// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package zstd

import (
	"io"

	"github.com/klauspost/compress/zstd"

	"github.com/lateos-ai/wal-g/internal/ioextensions"
)

const (
	AlgorithmName = "zstd"
	FileExtension = "zst"
)

type Compressor struct{}

func (compressor Compressor) NewWriter(writer io.Writer) ioextensions.WriteFlushCloser {
	zw, err := zstd.NewWriter(writer, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		panic(err)
	}

	return zw
}

func (compressor Compressor) FileExtension() string {
	return FileExtension
}
