// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package fs

import (
	"fmt"
	"strings"

	"github.com/lateos-ai/wal-g/pkg/storages/storage"
)

const waleFileURL = "file://localhost"

// TODO: Unit tests
func ConfigureStorage(prefix string, _ map[string]string, rootWraps ...storage.WrapRootFolder) (storage.HashableStorage, error) {
	prefix = strings.TrimPrefix(prefix, waleFileURL) // WAL-E backward compatibility

	config := &Config{
		RootPath: prefix,
	}

	st, err := NewStorage(config, rootWraps...)
	if err != nil {
		return nil, fmt.Errorf("create FS storage: %w", err)
	}
	return st, nil
}
