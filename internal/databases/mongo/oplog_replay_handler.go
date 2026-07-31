// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package mongo

import (
	"context"

	"github.com/lateos-ai/wal-g/internal/databases/mongo/binary"
	"github.com/lateos-ai/wal-g/internal/databases/mongo/models"
	"github.com/lateos-ai/wal-g/internal/databases/mongo/stages"
)

func HandleOplogReplay(ctx context.Context,
	since,
	until models.Timestamp,
	fetcher stages.BetweenFetcher,
	applier stages.Applier) error {
	return binary.HandleOplogReplay(ctx, since, until, fetcher, applier)
}

func RunOplogReplay(ctx context.Context, mongodbURL string, replayArgs binary.ReplyOplogConfig) error {
	return binary.RunOplogReplay(ctx, mongodbURL, replayArgs)
}
