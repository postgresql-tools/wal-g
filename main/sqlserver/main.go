// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package main

import (
	_ "github.com/microsoft/go-mssqldb"

	"github.com/lateos-ai/wal-g/cmd/sqlserver"
)

func main() {
	sqlserver.Execute()
}
