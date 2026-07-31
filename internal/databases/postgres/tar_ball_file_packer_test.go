// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

package postgres

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/lateos-ai/wal-g/internal"
	"github.com/lateos-ai/wal-g/internal/crypto"
)

type testTarBall struct {
	partSize   atomic.Int64
	underlying *bytes.Buffer
	tarWriter  *tar.Writer
}

func (tb *testTarBall) Name() string                        { return "test.tar" }
func (tb *testTarBall) SetUp(_ crypto.Crypter, _ ...string) { tb.tarWriter = tar.NewWriter(tb.underlying) }
func (tb *testTarBall) CloseTar() error                     { return tb.tarWriter.Close() }
func (tb *testTarBall) Size() int64                         { return tb.partSize.Load() }
func (tb *testTarBall) AddSize(i int64)                     { tb.partSize.Add(i) }
func (tb *testTarBall) TarWriter() *tar.Writer              { return tb.tarWriter }
func (tb *testTarBall) AwaitUploads()                       {}

func TestPGPackerComputesSHA256(t *testing.T) {
	content := []byte("test data for pg packer sha256 computation")
	h := sha256.New()
	h.Write(content)
	expectedHex := hex.EncodeToString(h.Sum(nil))

	dir := t.TempDir()
	filePath := filepath.Join(dir, "testfile.txt")
	err := os.WriteFile(filePath, content, 0644)
	if err != nil {
		t.Fatal(err)
	}

	fileInfo, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}

	files := &internal.RegularBundleFiles{}
	packer := NewTarBallFilePacker(
		nil, nil, files,
		NewTarBallFilePackerOptions(false, false),
	)

	tarBall := &testTarBall{underlying: &bytes.Buffer{}}
	tarBall.SetUp(nil)

	cfi := &internal.ComposeFileInfo{
		Path:     filePath,
		FileInfo: fileInfo,
		Header: &tar.Header{
			Name:     "testfile.txt",
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
			Mode:     0644,
		},
		IsIncremented: false,
	}

	err = packer.PackFileIntoTar(cfi, tarBall)
	if err != nil {
		t.Fatal(err)
	}

	rawFiles := files.GetUnderlyingMap()
	val, ok := rawFiles.Load("testfile.txt")
	if !ok {
		t.Fatal("file description not found in BundleFiles")
	}
	desc := val.(internal.BackupFileDescription)

	if desc.SHA256 != expectedHex {
		t.Errorf("SHA256 mismatch: expected %s, got %s", expectedHex, desc.SHA256)
	}
	if desc.IsIncremented {
		t.Error("expected IsIncremented to be false")
	}
	if desc.MTime.IsZero() {
		t.Error("expected MTime to be set")
	}
}

func TestPGPackerSHA256WithTarRoundTrip(t *testing.T) {
	content := []byte("data for pg packer tar round trip")
	h := sha256.New()
	h.Write(content)
	expectedHex := hex.EncodeToString(h.Sum(nil))

	dir := t.TempDir()
	filePath := filepath.Join(dir, "data.bin")
	err := os.WriteFile(filePath, content, 0644)
	if err != nil {
		t.Fatal(err)
	}

	fileInfo, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}

	files := &internal.RegularBundleFiles{}
	packer := NewTarBallFilePacker(
		nil, nil, files,
		NewTarBallFilePackerOptions(false, false),
	)

	var buf bytes.Buffer
	tarBall := &testTarBall{underlying: &buf}
	tarBall.SetUp(nil)

	err = packer.PackFileIntoTar(&internal.ComposeFileInfo{
		Path:     filePath,
		FileInfo: fileInfo,
		Header: &tar.Header{
			Name:     "data.bin",
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
			Mode:     0644,
		},
		IsIncremented: false,
	}, tarBall)
	if err != nil {
		t.Fatal(err)
	}

	err = tarBall.CloseTar()
	if err != nil {
		t.Fatal(err)
	}

	tr := tar.NewReader(&buf)
	header, err := tr.Next()
	if err != nil {
		t.Fatal(err)
	}
	if header.Name != "data.bin" {
		t.Errorf("expected name 'data.bin', got '%s'", header.Name)
	}
	readBack, err := io.ReadAll(tr)
	if err != nil {
		t.Fatal(err)
	}
	if string(readBack) != string(content) {
		t.Errorf("content mismatch: expected %s, got %s", content, readBack)
	}

	rawFiles := files.GetUnderlyingMap()
	val, ok := rawFiles.Load("data.bin")
	if !ok {
		t.Fatal("file description not found")
	}
	desc := val.(internal.BackupFileDescription)
	if desc.SHA256 != expectedHex {
		t.Errorf("SHA256 mismatch: expected %s, got %s", expectedHex, desc.SHA256)
	}
}

func TestPGPackerFileNotExistError(t *testing.T) {
	content := []byte("will be deleted")
	dir := t.TempDir()
	filePath := filepath.Join(dir, "willdelete.txt")
	err := os.WriteFile(filePath, content, 0644)
	if err != nil {
		t.Fatal(err)
	}

	fileInfo, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}

	err = os.Remove(filePath)
	if err != nil {
		t.Fatal(err)
	}

	packer := NewTarBallFilePacker(
		nil, nil,
		&internal.RegularBundleFiles{},
		NewTarBallFilePackerOptions(false, false),
	)

	tarBall := &testTarBall{underlying: &bytes.Buffer{}}
	tarBall.SetUp(nil)

	cfi := &internal.ComposeFileInfo{
		Path:     filePath,
		FileInfo: fileInfo,
		Header: &tar.Header{
			Name: "deleted.txt",
			Size: int64(len(content)),
		},
		IsIncremented: false,
	}

	err = packer.PackFileIntoTar(cfi, tarBall)
	if err != nil {
		t.Fatal("expected nil error for deleted file, got:", err)
	}
}

func TestOldFormatSentinelParsing(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/backup_verify/sentinel_v0141.json")
	if err != nil {
		t.Fatal(err)
	}

	var meta FilesMetadataDto
	err = json.Unmarshal(data, &meta)
	if err != nil {
		t.Fatal(err)
	}

	if meta.Files == nil {
		t.Fatal("expected Files to be non-nil")
	}

	for name, desc := range meta.Files {
		if desc.SHA256 != "" {
			t.Errorf("expected empty SHA256 for file '%s', got '%s'", name, desc.SHA256)
		}
	}
}

func TestOldFormatSentinelSHA256FieldOmitEmpty(t *testing.T) {
	data, err := os.ReadFile("../../../testdata/backup_verify/sentinel_v0141.json")
	if err != nil {
		t.Fatal(err)
	}

	var meta FilesMetadataDto
	err = json.Unmarshal(data, &meta)
	if err != nil {
		t.Fatal(err)
	}

	out, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}

	var roundTripped FilesMetadataDto
	err = json.Unmarshal(out, &roundTripped)
	if err != nil {
		t.Fatal(err)
	}

	for name, desc := range roundTripped.Files {
		if desc.SHA256 != "" {
			t.Errorf("expected empty SHA256 after round trip for file '%s', got '%s'", name, desc.SHA256)
		}
	}
}
