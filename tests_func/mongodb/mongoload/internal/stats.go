// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package internal

import (
	"encoding/json"
	"io"

	"github.com/lateos-ai/wal-g/tests_func/mongodb/mongoload/models"
)

func updateStatWithMongoOpLog(stat *models.OpsStat, log OpInfo) {
	if log.status {
		stat.Success[log.opName]++
	} else {
		stat.Fail[log.opName]++
		stat.Errors[log.opName] = append(stat.Errors[log.opName], log.err)
		return
	}
	var docs int
	if log.res != nil && log.res["n"] != nil {
		docs = int(log.res["n"].(int32))
	}
	stat.Docs[log.opName] += docs
}

func CollectStat(opInfoCh <-chan OpInfo) models.LoadStat {
	stat := models.NewLoadStat()
	for opInfo := range opInfoCh {
		updateStatWithMongoOpLog(&stat.CmdStat, opInfo)
		if opInfo.opName == "transaction" {
			for key := range opInfo.subcmds {
				updateStatWithMongoOpLog(&stat.TxnStat, opInfo.subcmds[key])
			}
		}
	}
	return *stat
}

func PrintStat(stat models.LoadStat, writer io.Writer) error {
	bstat, err := json.MarshalIndent(stat, "", "    ")
	if err != nil {
		return err
	}
	_, err = writer.Write(bstat)
	return err
}
