// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

package mongo

import (
	"context"

	"golang.org/x/sync/errgroup"

	"github.com/lateos-ai/wal-g/internal/databases/mongo/stages"
)

// HandleOplogPush starts oplog archiving process: fetch, validate, upload to storage.
func HandleOplogPush(ctx context.Context, fetcher stages.Fetcher, applier stages.Applier) error {
	errgrp, ctx := errgroup.WithContext(ctx)
	var errs []<-chan error

	oplogc, errc, err := fetcher.Fetch(ctx)
	if err != nil {
		return err
	}
	errs = append(errs, errc)

	errc, err = applier.Apply(ctx, oplogc)
	if err != nil {
		return err
	}
	errs = append(errs, errc)

	for _, errc := range errs {
		errgrp.Go(func() error {
			return <-errc
		})
	}

	return errgrp.Wait()
}
