// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

//go:build windows

package fsutil

import (
	"fmt"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func getDiskSpace(path string) (DiskSpace, error) {
	pathPtr, err := windows.UTF16PtrFromString(filepath.Clean(path))
	if err != nil {
		return DiskSpace{}, fmt.Errorf("failed to convert path %s: %w", path, err)
	}

	var freeToCaller, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(pathPtr, &freeToCaller, &total, &totalFree); err != nil {
		return DiskSpace{}, fmt.Errorf("failed to query free space at %s: %w", path, err)
	}

	// freeToCaller respects per-user quotas, matching the Unix side's use of the
	// space actually available to the running user rather than raw free blocks.
	return DiskSpace{
		FreeBytes:  freeToCaller,
		TotalBytes: total,
	}, nil
}
