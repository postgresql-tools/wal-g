// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

//go:build brotli
// +build brotli

package brotli

import (
	"io"

	"github.com/google/brotli/go/cbrotli"

	"github.com/lateos-ai/wal-g/internal/compression/computils"
)

type Decompressor struct{}

func (decompressor Decompressor) Decompress(src io.Reader) (io.ReadCloser, error) {
	return cbrotli.NewReader(computils.NewUntilEOFReader(src)), nil
}

func (decompressor Decompressor) FileExtension() string {
	return FileExtension
}
