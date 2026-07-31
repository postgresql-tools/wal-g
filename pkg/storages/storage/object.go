// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package storage

import (
	"time"
)

//go:generate mockgen -destination=../../../test/mocks/mock_object.go -package mocks -build_flags -mod=readonly github.com/lateos-ai/wal-g/pkg/storages/storage Object

type Object interface {
	GetName() string
	GetLastModified() time.Time
	GetSize() int64
	GetVersionID() string
	GetAdditionalInfo() string
}
