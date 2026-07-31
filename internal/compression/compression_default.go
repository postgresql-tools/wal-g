// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

//go:build !windows
// +build !windows

package compression

import (
	"github.com/lateos-ai/wal-g/internal/compression/gzip"
	"github.com/lateos-ai/wal-g/internal/compression/lz4"
	"github.com/lateos-ai/wal-g/internal/compression/lzma"
	"github.com/lateos-ai/wal-g/internal/compression/none"
)

var CompressingAlgorithms = []string{lz4.AlgorithmName, lzma.AlgorithmName, none.AlgorithmName}

var Compressors = map[string]Compressor{
	lz4.AlgorithmName:  lz4.Compressor{},
	lzma.AlgorithmName: lzma.Compressor{},
	none.AlgorithmName: none.Compressor{},
}

var Decompressors = []Decompressor{
	lz4.Decompressor{},
	lzma.Decompressor{},
	gzip.Decompressor{},
	none.Decompressor{},
}
