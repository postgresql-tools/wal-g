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

	return FilesystemID(fmt.Sprintf("dev:%d", uint64(stat.Dev))), nil
}
