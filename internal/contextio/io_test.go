// Modified in the lateos-ai/wal-g fork. Derived from wal-g/wal-g (Apache-2.0). See NOTICE.

// Source: https://github.com/dolmen-go/contextio

package contextio_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/lateos-ai/wal-g/internal/contextio"
)

func TestWriter(t *testing.T) {
	var buf bytes.Buffer
	w := contextio.NewWriter(t.Context(), &buf)
	n, err := w.Write([]byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Fatal("5 bytes written expected")
	}
	if buf.String() != "hello" {
		t.Fatal("Bad content")
	}

	buf.Reset()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	w = contextio.NewWriter(ctx, &buf)
	n, err = w.Write([]byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 5 {
		t.Fatal("5 bytes written expected")
	}
	if buf.String() != "hello" {
		t.Fatal("Bad content")
	}

	cancel()

	n, err = w.Write([]byte(", world"))
	if err != context.Canceled {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatal("0 bytes written expected")
	}
	if buf.String() != "hello" {
		t.Fatal("Bad content")
	}
}
