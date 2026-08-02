// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

package fsutil

// DiskSpace describes the capacity of the filesystem holding a given path.
type DiskSpace struct {
	// FreeBytes is the space available to the calling user, which on filesystems
	// that reserve blocks for root is less than the raw free space.
	FreeBytes uint64
	// TotalBytes is the filesystem's total capacity.
	TotalBytes uint64
}

// UsedRatio returns the fraction of the filesystem currently in use (0-1),
// or 0 when the total capacity is unknown.
func (d DiskSpace) UsedRatio() float64 {
	if d.TotalBytes == 0 {
		return 0
	}
	return float64(d.TotalBytes-d.FreeBytes) / float64(d.TotalBytes)
}

// GetDiskSpace reports the capacity of the filesystem containing path.
// The path must exist.
func GetDiskSpace(path string) (DiskSpace, error) {
	return getDiskSpace(path)
}
