package internal

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/lateos-ai/wal-g/internal/crypto"
)

type testTarBall struct {
	partSize  atomic.Int64
	underlying *bytes.Buffer
	tarWriter *tar.Writer
}

func (tb *testTarBall) Name() string                                { return "test.tar" }
func (tb *testTarBall) SetUp(_ crypto.Crypter, _ ...string)         { tb.tarWriter = tar.NewWriter(tb.underlying) }
func (tb *testTarBall) CloseTar() error                             { return tb.tarWriter.Close() }
func (tb *testTarBall) Size() int64                                 { return tb.partSize.Load() }
func (tb *testTarBall) AddSize(i int64)                             { tb.partSize.Add(i) }
func (tb *testTarBall) TarWriter() *tar.Writer                      { return tb.tarWriter }
func (tb *testTarBall) AwaitUploads()                               {}

func TestRegularPackerComputesSHA256(t *testing.T) {
	content := []byte("test data for sha256 computation in regular packer")
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

	files := &RegularBundleFiles{}
	packer := NewRegularTarBallFilePacker(files, false)

	tarBall := &testTarBall{underlying: &bytes.Buffer{}}
	tarBall.SetUp(nil)

	cfi := &ComposeFileInfo{
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

	val, ok := files.Load("testfile.txt")
	if !ok {
		t.Fatal("file description not found in BundleFiles")
	}
	desc := val.(BackupFileDescription)

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

func TestRegularPackerSHA256StoredInTar(t *testing.T) {
	content := []byte("verify data round-trips through tar")
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

	files := &RegularBundleFiles{}
	packer := NewRegularTarBallFilePacker(files, false)

	var buf bytes.Buffer
	tarBall := &testTarBall{underlying: &buf}
	tarBall.SetUp(nil)

	cfi := &ComposeFileInfo{
		Path:     filePath,
		FileInfo: fileInfo,
		Header: &tar.Header{
			Name:     "data.bin",
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

	err = tarBall.CloseTar()
	if err != nil {
		t.Fatal(err)
	}

	// Read back from tar and verify content
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

	// Verify SHA256 in file description
	val, ok := files.Load("data.bin")
	if !ok {
		t.Fatal("file description not found")
	}
	desc := val.(BackupFileDescription)
	if desc.SHA256 != expectedHex {
		t.Errorf("SHA256 mismatch: expected %s, got %s", expectedHex, desc.SHA256)
	}
}

func TestRegularPackerSkippedFileNoSHA256(t *testing.T) {
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

	files := &RegularBundleFiles{}
	packer := NewRegularTarBallFilePacker(files, true)

	tarBall := &testTarBall{underlying: &bytes.Buffer{}}
	tarBall.SetUp(nil)

	cfi := &ComposeFileInfo{
		Path:     filePath,
		FileInfo: fileInfo,
		Header: &tar.Header{
			Name: "skipped_file.txt",
			Size: int64(len(content)),
		},
		IsIncremented: false,
	}

	err = packer.PackFileIntoTar(cfi, tarBall)
	if err != nil {
		t.Fatal("expected nil error for deleted file, got:", err)
	}
}
