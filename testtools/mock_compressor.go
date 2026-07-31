// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package testtools

import (
	"io"

	"github.com/lateos-ai/wal-g/internal/ioextensions"
)

type MockCompressor struct{}

func (compressor *MockCompressor) NewWriter(writer io.Writer) ioextensions.WriteFlushCloser {
	return &NopCloserWriter{
		writer,
	}
}

func (compressor *MockCompressor) FileExtension() string {
	return "mock"
}
