// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package lz4

import (
	"io"

	"github.com/pierrec/lz4/v4"

	"github.com/lateos-ai/wal-g/internal/ioextensions"
)

const (
	AlgorithmName = "lz4"

	FileExtension = "lz4"
)

type Compressor struct{}

func (compressor Compressor) NewWriter(writer io.Writer) ioextensions.WriteFlushCloser {
	return lz4.NewWriter(writer)
}

func (compressor Compressor) FileExtension() string {
	return FileExtension
}
