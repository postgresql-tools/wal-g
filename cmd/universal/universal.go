// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package universal

import (
	"github.com/spf13/cobra"

	"github.com/lateos-ai/wal-g/cmd/common"
	"github.com/lateos-ai/wal-g/cmd/etcd"
	"github.com/lateos-ai/wal-g/cmd/fdb"
	"github.com/lateos-ai/wal-g/cmd/mongo"
	"github.com/lateos-ai/wal-g/cmd/mysql"
	"github.com/lateos-ai/wal-g/cmd/pg"
	"github.com/lateos-ai/wal-g/cmd/redis"
	"github.com/lateos-ai/wal-g/cmd/sqlserver"
)

var (
	universalCmd = &cobra.Command{
		Use: "wal-g",

		Short: "Universal database backup tool",
	}
)

func Execute() {
	common.ExecuteContext(universalCmd)
}

func init() {
	etcdCmd := etcd.GetCmd()

	etcdCmd.Use = "etcd"

	universalCmd.AddCommand(etcdCmd)

	fdbCmd := fdb.GetCmd()

	fdbCmd.Use = "fdb"

	universalCmd.AddCommand(fdbCmd)

	mongoCmd := mongo.GetCmd()

	mongoCmd.Use = "mongo"

	universalCmd.AddCommand(mongoCmd)

	mysqlCmd := mysql.GetCmd()

	mysqlCmd.Use = "mysql"

	universalCmd.AddCommand(mysqlCmd)

	pgCmd := pg.GetCmd()

	pgCmd.Use = "pg"

	universalCmd.AddCommand(pgCmd)

	redisCmd := redis.GetCmd()

	redisCmd.Use = "redis"

	universalCmd.AddCommand(redisCmd)

	sqlserverCmd := sqlserver.GetCmd()

	sqlserverCmd.Use = "sqlserver"

	universalCmd.AddCommand(sqlserverCmd)
}
