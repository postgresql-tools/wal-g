// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package compression

import (
	"github.com/lateos-ai/wal-g/internal/compression/zstd"
)

func init() {
	Decompressors = append(Decompressors, zstd.Decompressor{})
	Compressors[zstd.AlgorithmName] = zstd.Compressor{}
	CompressingAlgorithms = append(CompressingAlgorithms, zstd.AlgorithmName)
}
