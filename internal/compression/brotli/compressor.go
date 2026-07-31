// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

//go:build brotli
// +build brotli

package brotli

import (
	"io"

	"github.com/google/brotli/go/cbrotli"

	"github.com/lateos-ai/wal-g/internal/ioextensions"
)

const (
	AlgorithmName = "brotli"
	FileExtension = "br"
)

type Compressor struct{}

func (compressor Compressor) NewWriter(writer io.Writer) ioextensions.WriteFlushCloser {
	return cbrotli.NewWriter(writer, cbrotli.WriterOptions{Quality: 3})
}

func (compressor Compressor) FileExtension() string {
	return FileExtension
}
