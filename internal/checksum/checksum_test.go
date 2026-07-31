// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

package checksum

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"
)

func TestCalculator(t *testing.T) {
	data := "hello world"
	calc := CreateCalculator()
	calc.AddData([]byte(data))
	sum := calc.Checksum()

	expected := sha256.Sum256([]byte(data))
	expectedHex := hex.EncodeToString(expected[:])

	if sum != expectedHex {
		t.Errorf("expected %s, got %s", expectedHex, sum)
	}

	if calc.Algorithm() != "sha256" {
		t.Errorf("expected sha256, got %s", calc.Algorithm())
	}
}

func TestReaderWithChecksum(t *testing.T) {
	data := "hello world"
	calc := CreateCalculator()
	reader := CreateReaderWithChecksum(strings.NewReader(data), calc)

	_, err := io.Copy(io.Discard, reader)
	if err != nil {
		t.Fatal(err)
	}

	sum := calc.Checksum()
	expected := sha256.Sum256([]byte(data))
	expectedHex := hex.EncodeToString(expected[:])

	if sum != expectedHex {
		t.Errorf("expected %s, got %s", expectedHex, sum)
	}
}

func TestWriterWithChecksum(t *testing.T) {
	data := "hello world"
	calc := CreateCalculator()
	var buf bytes.Buffer
	writer := CreateWriterWithChecksum(&nopWriteCloser{Buffer: &buf}, calc)

	_, err := writer.Write([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	err = writer.Close()
	if err != nil {
		t.Fatal(err)
	}

	sum := calc.Checksum()
	expected := sha256.Sum256([]byte(data))
	expectedHex := hex.EncodeToString(expected[:])

	if sum != expectedHex {
		t.Errorf("expected %s, got %s", expectedHex, sum)
	}

	if buf.String() != data {
		t.Errorf("expected %s, got %s", data, buf.String())
	}
}

type nopWriteCloser struct {
	*bytes.Buffer
}

func (n *nopWriteCloser) Close() error {
	return nil
}

func BenchmarkSHA256Overhead(b *testing.B) {
	const size = 1 << 20 // 1 MB
	data := bytes.Repeat([]byte("a"), size)
	b.SetBytes(size)

	b.Run("copy-only", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			reader := bytes.NewReader(data)
			_, err := io.Copy(io.Discard, reader)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("copy-with-sha256", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			reader := bytes.NewReader(data)
			calc := CreateCalculator()
			csc := CreateReaderWithChecksum(reader, calc)
			_, err := io.Copy(io.Discard, csc)
			if err != nil {
				b.Fatal(err)
			}
			_ = calc.Checksum()
		}
	})
}
