// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package lzma

import (
	"io"

	"github.com/ulikunitz/xz/lzma"

	"github.com/lateos-ai/wal-g/internal/ioextensions"
)

const (
	AlgorithmName = "lzma"

	FileExtension = "lzma"
)

type Compressor struct{}

func (compressor Compressor) NewWriter(writer io.Writer) ioextensions.WriteFlushCloser {
	lzmaWriter, err := lzma.NewWriter(writer)
	if err != nil {
		panic(err)
	}
	return Writer{lzmaWriter}
}

type Writer struct {
	*lzma.Writer
}

func (l Writer) Flush() error {
	// Maybe in LZMA2
	panic("Flush not implemented for LZMA.")
}

func (compressor Compressor) FileExtension() string {
	return FileExtension
}
