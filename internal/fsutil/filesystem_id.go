// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

package fsutil

// FilesystemID identifies the filesystem a path lives on. Values are only
// meaningful for comparison against other IDs obtained on the same host: two
// paths share a filesystem if and only if their IDs are equal.
//
// This exists so callers can tell "these paths draw on the same free space" from
// "these paths draw on different free space" - the difference between a space
// estimate that is definitive and one that is a guess.
type FilesystemID string

// GetFilesystemID reports which filesystem holds path. The path must exist.
func GetFilesystemID(path string) (FilesystemID, error) {
	return getFilesystemID(path)
}
