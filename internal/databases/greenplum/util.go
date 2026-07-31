// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package greenplum

import (
	"fmt"
	"path"

	"github.com/spf13/viper"

	conf "github.com/lateos-ai/wal-g/internal/config"
	"github.com/lateos-ai/wal-g/utility"
)

const SegmentsFolderPath = "segments_" + utility.VersionStr + "/"

func FormatSegmentStoragePrefix(contentID int) string {
	segmentFolderName := fmt.Sprintf("seg%d", contentID)

	return path.Join(SegmentsFolderPath, segmentFolderName)
}

func formatSegmentLogPath(contentID int) string {
	logsDir := viper.GetString(conf.GPLogsDirectory)

	return fmt.Sprintf("%s/%s-seg%d.log", logsDir, SegBackupLogPrefix, contentID)
}

func FormatSegmentBackupPath(contentID int) string {
	return path.Join(FormatSegmentStoragePrefix(contentID), utility.BaseBackupPath)
}

func FormatSegmentWalPath(contentID int) string {
	return path.Join(FormatSegmentStoragePrefix(contentID), utility.WalPath)
}
