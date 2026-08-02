// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

//go:build !windows

package fsutil

import (
	"fmt"
	"syscall"
)

func getDiskSpace(path string) (DiskSpace, error) {
	var fs syscall.Statfs_t
	if err := syscall.Statfs(path, &fs); err != nil {
		return DiskSpace{}, fmt.Errorf("failed to stat filesystem at %s: %w", path, err)
	}

	blockSize := uint64(fs.Bsize)

	// Bavail rather than Bfree: blocks reserved for root are not space a restore
	// running as postgres can actually use.
	return DiskSpace{
		FreeBytes:  fs.Bavail * blockSize,
		TotalBytes: fs.Blocks * blockSize,
	}, nil
}
