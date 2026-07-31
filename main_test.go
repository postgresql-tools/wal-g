// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

package main

import (
"testing"
)

func TestVersion(t *testing.T) {
if Version == "" {
t.Fatal("Version not set")
}
}
