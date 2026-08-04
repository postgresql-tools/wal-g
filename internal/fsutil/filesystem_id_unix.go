// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

//go:build !windows

package fsutil

import (
	"fmt"
	"os"
	"syscall"
)

func getFilesystemID(path string) (FilesystemID, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("failed to stat %s: %w", path, err)
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", fmt.Errorf("failed to read device ID for %s", path)
	}

	// Dev is not the same integer type across unix platforms - uint64 on Linux,
	// int32 on darwin - so it is formatted rather than converted. The ID is only
	// ever compared for equality, so any stable per-device rendering will do.
	return FilesystemID(fmt.Sprintf("dev:%d", stat.Dev)), nil
}
