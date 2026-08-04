// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

//go:build windows

package fsutil

import (
	"fmt"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// volumePathBufferLen is generous enough for a mount point path including the
// extended-length prefix.
const volumePathBufferLen = 1024

func getFilesystemID(path string) (FilesystemID, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("failed to resolve %s: %w", path, err)
	}

	pathPtr, err := windows.UTF16PtrFromString(absolute)
	if err != nil {
		return "", fmt.Errorf("failed to convert path %s: %w", path, err)
	}

	buffer := make([]uint16, volumePathBufferLen)
	if err := windows.GetVolumePathName(pathPtr, &buffer[0], volumePathBufferLen); err != nil {
		return "", fmt.Errorf("failed to resolve the volume holding %s: %w", path, err)
	}

	// The mount point is the volume's identity here: two paths under the same
	// mount point draw on the same free space.
	mountPoint := windows.UTF16ToString(buffer)

	return FilesystemID("volume:" + strings.ToLower(mountPoint)), nil
}
