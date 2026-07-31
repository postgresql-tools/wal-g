// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

//go:build lzo && !windows
// +build lzo,!windows

package compression

import "github.com/lateos-ai/wal-g/internal/compression/lzo"

func init() {
	Decompressors = append(Decompressors, lzo.Decompressor{})
}
