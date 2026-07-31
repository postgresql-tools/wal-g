// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

//go:build brotli && !windows && !(darwin && arm64)
// +build brotli
// +build !windows
// +build !darwin !arm64

package compression

import "github.com/lateos-ai/wal-g/internal/compression/brotli"

func init() {
	Decompressors = append(Decompressors, brotli.Decompressor{})
	Compressors[brotli.AlgorithmName] = brotli.Compressor{}
	CompressingAlgorithms = append(CompressingAlgorithms, brotli.AlgorithmName)
}
