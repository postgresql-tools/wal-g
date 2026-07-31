// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

package postgres

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lateos-ai/wal-g/internal"
	"github.com/lateos-ai/wal-g/pkg/storages/memory"
	"github.com/lateos-ai/wal-g/pkg/storages/storage"
	"github.com/lateos-ai/wal-g/utility"
)

const testBackupName = "base_000000010000000000000001"

func TestBackupVerify_SentinelMissing(t *testing.T) {
	root := memory.NewFolder("", memory.NewKVS())
	code := HandleBackupVerify(root, testBackupName, 0, 0, "", "", "json", io.Discard)
	if code != 1 {
		t.Errorf("expected exit code 1 for missing sentinel, got %d", code)
	}
}

func TestBackupVerify_SentinelCorruptJSON(t *testing.T) {
	root := memory.NewFolder("", memory.NewKVS())
	putBackupSentinel(root, testBackupName, []byte(`{invalid json`))
	code := HandleBackupVerify(root, testBackupName, 0, 0, "", "", "json", io.Discard)
	if code != 1 {
		t.Errorf("expected exit code 1 for corrupt sentinel, got %d", code)
	}
}

func TestBackupVerify_SentinelTruncatedJSON(t *testing.T) {
	root := memory.NewFolder("", memory.NewKVS())
	putBackupSentinel(root, testBackupName, []byte(`{"LSN":`))
	code := HandleBackupVerify(root, testBackupName, 0, 0, "", "", "json", io.Discard)
	if code != 1 {
		t.Errorf("expected exit code 1 for truncated sentinel, got %d", code)
	}
}

func TestBackupVerify_MissingTarPart(t *testing.T) {
	root := memory.NewFolder("", memory.NewKVS())
	sentinel := minimalSentinel()
	filesMeta := filesMetaWithParts("part_000.tar", "part_001.tar")

	putBackup(root, testBackupName, sentinel, filesMeta, map[string][]byte{
		"part_000.tar": []byte("content"),
	}, nil)

	buf := &bytes.Buffer{}
	code := HandleBackupVerify(root, testBackupName, 0, 0, "", "", "text", buf)
	if code != 1 {
		t.Errorf("expected exit code 1 for missing tar part, got %d", code)
	}
	if !bytes.Contains(buf.Bytes(), []byte("part_001.tar")) {
		t.Errorf("expected missing part report to mention part_001.tar, got:\n%s", buf.String())
	}
}

func TestBackupVerify_ChecksumCoverageMixed(t *testing.T) {
	root := memory.NewFolder("", memory.NewKVS())
	sentinel := minimalSentinel()
	filesMeta := FilesMetadataDto{
		Files: internal.BackupFileList{
			"file_a": {SHA256: "abc123"},
			"file_b": {},
			"file_c": {SHA256: "def456"},
		},
		TarFileSets: map[string][]string{
			"part_000.tar": {"file_a", "file_b", "file_c"},
		},
	}

	putBackup(root, testBackupName, sentinel, filesMeta, map[string][]byte{
		"part_000.tar": []byte("content"),
	}, nil)

	buf := &bytes.Buffer{}
	code := HandleBackupVerify(root, testBackupName, 0, 0, "", "", "text", buf)
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
	if !bytes.Contains(buf.Bytes(), []byte("2/3")) && !bytes.Contains(buf.Bytes(), []byte("2 / 3")) {
		t.Errorf("expected checksum coverage 2/3, got:\n%s", buf.String())
	}
}

func TestBackupVerify_ChecksumCoverageAllPresent(t *testing.T) {
	root := memory.NewFolder("", memory.NewKVS())
	sentinel := minimalSentinel()
	filesMeta := FilesMetadataDto{
		Files: internal.BackupFileList{
			"file_a": {SHA256: "abc"},
			"file_b": {SHA256: "def"},
		},
		TarFileSets: map[string][]string{
			"part_000.tar": {"file_a", "file_b"},
		},
	}

	putBackup(root, testBackupName, sentinel, filesMeta, map[string][]byte{
		"part_000.tar": []byte("content"),
	}, nil)

	code := HandleBackupVerify(root, testBackupName, 0, 0, "", "", "json", io.Discard)
	if code != 0 {
		t.Errorf("expected exit code 0, got %d", code)
	}
}

func TestBackupVerify_DeployMetadataNone(t *testing.T) {
	root, _, _ := setupBackupWithSentinelFixture(t, "testdata/backup_verify/sentinel_v0141.json")

	buf := &bytes.Buffer{}
	code := HandleBackupVerify(root, testBackupName, 0, 0, "", "", "text", buf)
	if code != 0 {
		t.Errorf("expected exit code 0 for pre-deploy-tagging backup, got %d (output: %s)", code, buf.String())
	}
	if !bytes.Contains(buf.Bytes(), []byte("none")) {
		t.Errorf("expected 'none' deploy metadata for old-format sentinel, got:\n%s", buf.String())
	}
}

func TestBackupVerify_DeployMetadataPresent(t *testing.T) {
	root, _, _ := setupBackupWithSentinelFixture(t, "testdata/backup_verify/sentinel_with_deploy.json")

	buf := &bytes.Buffer{}
	code := HandleBackupVerify(root, testBackupName, 0, 0, "", "", "text", buf)
	if code != 0 {
		t.Errorf("expected exit code 0 for deploy metadata test, got %d (output: %s)", code, buf.String())
	}
	if !bytes.Contains(buf.Bytes(), []byte("abc123def")) {
		t.Errorf("expected deploy metadata to include git_commit 'abc123def', got:\n%s", buf.String())
	}
}

func TestBackupVerify_WALChainOK(t *testing.T) {
	root := memory.NewFolder("", memory.NewKVS())
	sentinel := minimalSentinel()
	filesMeta := minimalFilesMeta()

	walFiles := map[string][]byte{
		"000000010000000000000001": {},
		"000000010000000000000002": {},
		"000000010000000000000003": {},
	}

	putBackupWithMeta(root, testBackupName, sentinel, filesMeta, emptyTarParts(filesMeta), walFiles, lsnPtr(0x1000000), lsnPtr(0x1FFFFFF))

	buf := &bytes.Buffer{}
	code := HandleBackupVerify(root, testBackupName, 0, 0, "", "", "text", buf)
	if code != 0 {
		t.Errorf("expected exit code 0 for clean WAL chain, got %d (output: %s)", code, buf.String())
	}
	if !bytes.Contains(buf.Bytes(), []byte("OK")) && !bytes.Contains(buf.Bytes(), []byte("no gaps")) {
		t.Errorf("expected WAL chain OK or no gaps, got:\n%s", buf.String())
	}
}

func TestBackupVerify_WALGapDetected(t *testing.T) {
	root := memory.NewFolder("", memory.NewKVS())
	sentinel := minimalSentinel()
	filesMeta := minimalFilesMeta()

	walFiles := map[string][]byte{
		"000000010000000000000001": {},
		"000000010000000000000003": {},
	}

	// segment 2 is missing, which should be detected as a gap
	putBackupWithMeta(root, testBackupName, sentinel, filesMeta, emptyTarParts(filesMeta), walFiles, lsnPtr(0x1000000), lsnPtr(0x1FFFFFF))

	buf := &bytes.Buffer{}
	code := HandleBackupVerify(root, testBackupName, 0, 0, "", "", "text", buf)
	if code != 0 {
		t.Errorf("expected exit code 0 for informational WAL gaps, got %d (output: %s)", code, buf.String())
	}
	if !bytes.Contains(buf.Bytes(), []byte("GAPS")) {
		t.Errorf("expected WAL chain to report gaps, got:\n%s", buf.String())
	}
}

func TestBackupVerify_Tier2AllMatch(t *testing.T) {
	fileContent := []byte("hello world, this is a test file")
	h := sha256.Sum256(fileContent)
	expectedSHA256 := hex.EncodeToString(h[:])

	root := memory.NewFolder("", memory.NewKVS())
	sentinel := minimalSentinel()
	filesMeta := FilesMetadataDto{
		Files: internal.BackupFileList{
			"testfile.txt": {SHA256: expectedSHA256},
		},
		TarFileSets: map[string][]string{
			"part_000.tar": {"testfile.txt"},
		},
	}

	tarPart := buildTarPart(map[string][]byte{
		"testfile.txt": fileContent,
	})

	putBackup(root, testBackupName, sentinel, filesMeta, map[string][]byte{
		"part_000.tar": tarPart,
	}, nil)

	buf := &bytes.Buffer{}
	code := HandleBackupVerify(root, testBackupName, 100, 42, "", "", "text", buf)
	if code != 0 {
		t.Errorf("expected exit code 0 for matching checksums, got %d (output: %s)", code, buf.String())
	}
	if !bytes.Contains(buf.Bytes(), []byte("no issues detected")) {
		t.Errorf("expected 'no issues detected', got:\n%s", buf.String())
	}
}

func TestBackupVerify_Tier2Mismatch(t *testing.T) {
	originalContent := []byte("original content")
	h := sha256.Sum256(originalContent)
	expectedSHA256 := hex.EncodeToString(h[:])

	corruptedContent := []byte("corrupted content!!!!")

	root := memory.NewFolder("", memory.NewKVS())
	sentinel := minimalSentinel()
	filesMeta := FilesMetadataDto{
		Files: internal.BackupFileList{
			"testfile.txt": {SHA256: expectedSHA256},
		},
		TarFileSets: map[string][]string{
			"part_000.tar": {"testfile.txt"},
		},
	}

	tarPart := buildTarPart(map[string][]byte{
		"testfile.txt": corruptedContent,
	})

	putBackup(root, testBackupName, sentinel, filesMeta, map[string][]byte{
		"part_000.tar": tarPart,
	}, nil)

	buf := &bytes.Buffer{}
	code := HandleBackupVerify(root, testBackupName, 100, 42, "", "", "text", buf)
	if code != 1 {
		t.Errorf("expected exit code 1 for checksum mismatch, got %d (output: %s)", code, buf.String())
	}
	if !bytes.Contains(buf.Bytes(), []byte("MISMATCH")) {
		t.Errorf("expected 'MISMATCH' in output, got:\n%s", buf.String())
	}
}

func TestBackupVerify_Tier2PreChecksumBackup(t *testing.T) {
	fileContent := []byte("some data without stored checksum")

	root := memory.NewFolder("", memory.NewKVS())
	sentinel := minimalSentinel()
	filesMeta := FilesMetadataDto{
		Files: internal.BackupFileList{
			"testfile.txt": {},
		},
		TarFileSets: map[string][]string{
			"part_000.tar": {"testfile.txt"},
		},
	}

	tarPart := buildTarPart(map[string][]byte{
		"testfile.txt": fileContent,
	})

	putBackup(root, testBackupName, sentinel, filesMeta, map[string][]byte{
		"part_000.tar": tarPart,
	}, nil)

	buf := &bytes.Buffer{}
	code := HandleBackupVerify(root, testBackupName, 100, 42, "", "", "text", buf)
	if code != 0 {
		t.Errorf("expected exit code 0 for pre-checksum backup, got %d (output: %s)", code, buf.String())
	}
	if !bytes.Contains(buf.Bytes(), []byte("Readability")) {
		t.Errorf("expected 'Readability' label for pre-checksum backup, got:\n%s", buf.String())
	}
}

func TestBackupVerify_SampleSeedReproducibility(t *testing.T) {
	fileContent := []byte("test content")
	h := sha256.Sum256(fileContent)
	sha := hex.EncodeToString(h[:])

	parts := make(map[string][]byte)
	filesMeta := FilesMetadataDto{
		Files:       make(internal.BackupFileList),
		TarFileSets: make(map[string][]string),
	}

	for i := 0; i < 10; i++ {
		partName := fmt.Sprintf("part_%03d.tar", i)
		fileName := fmt.Sprintf("file_%d.txt", i)
		parts[partName] = buildTarPart(map[string][]byte{fileName: fileContent})
		filesMeta.Files[fileName] = internal.BackupFileDescription{SHA256: sha}
		filesMeta.TarFileSets[partName] = []string{fileName}
	}

	root := memory.NewFolder("", memory.NewKVS())
	sentinel := minimalSentinel()
	putBackup(root, testBackupName, sentinel, filesMeta, parts, nil)

	buf1 := &bytes.Buffer{}
	buf2 := &bytes.Buffer{}
	code1 := HandleBackupVerify(root, testBackupName, 50, 12345, "", "", "json", buf1)
	code2 := HandleBackupVerify(root, testBackupName, 50, 12345, "", "", "json", buf2)

	if code1 != 0 || code2 != 0 {
		t.Errorf("expected exit code 0 for both runs, got %d and %d", code1, code2)
	}

	var res1, res2 BackupVerifyResult
	json.Unmarshal(buf1.Bytes(), &res1)
	json.Unmarshal(buf2.Bytes(), &res2)

	if res1.SampledParts != res2.SampledParts {
		t.Errorf("sampled parts differ: %d vs %d", res1.SampledParts, res2.SampledParts)
	}
	if (res1.Tier2Pass == nil) != (res2.Tier2Pass == nil) {
		t.Errorf("Tier2Pass existence differs")
	}
	if res1.Tier2Pass != nil && res2.Tier2Pass != nil && *res1.Tier2Pass != *res2.Tier2Pass {
		t.Errorf("Tier2Pass differs: %v vs %v", *res1.Tier2Pass, *res2.Tier2Pass)
	}
}

func TestBackupVerify_LATESTBackup(t *testing.T) {
	root := memory.NewFolder("", memory.NewKVS())
	sentinel := minimalSentinel()
	filesMeta := minimalFilesMeta()

	putBackup(root, testBackupName, sentinel, filesMeta, map[string][]byte{
		"part_000.tar": []byte("content"),
	}, nil)

	code := HandleBackupVerify(root, "", 0, 0, "", "", "json", io.Discard)
	if code != 0 {
		t.Errorf("expected exit code 0 for LATEST backup resolution, got %d", code)
	}
}

// --- helpers ---

func minimalSentinel() []byte {
	s := BackupSentinelDto{
		BackupStartLSN:  lsnPtr(0x1000000),
		BackupFinishLSN: lsnPtr(0x2000000),
		PgVersion:       15,
	}
	data, _ := json.Marshal(s)
	return data
}

func minimalFilesMeta() FilesMetadataDto {
	return FilesMetadataDto{
		Files: internal.BackupFileList{
			"global/pg_control": {},
		},
		TarFileSets: map[string][]string{
			"part_000.tar": {"global/pg_control"},
		},
	}
}

func filesMetaWithParts(partNames ...string) FilesMetadataDto {
	files := make(internal.BackupFileList)
	sets := make(map[string][]string)
	for _, p := range partNames {
		sets[p] = []string{"dummy"}
	}
	files["dummy"] = internal.BackupFileDescription{}
	return FilesMetadataDto{Files: files, TarFileSets: sets}
}

func lsnPtr(lsn LSN) *LSN {
	return &lsn
}

func putBackup(root storage.Folder, name string, sentinelJSON []byte, filesMeta interface{}, tarParts map[string][]byte, walFiles map[string][]byte) {
	putBackupSentinel(root, name, sentinelJSON)
	putFilesMeta(root, name, filesMeta)
	putTarParts(root, name, tarParts)
	putWALFiles(root, walFiles)
}

func putBackupWithMeta(root storage.Folder, name string, sentinelJSON []byte, filesMeta interface{}, tarParts map[string][]byte, walFiles map[string][]byte, startLSN, finishLSN *LSN) {
	putBackup(root, name, sentinelJSON, filesMeta, tarParts, walFiles)
	if startLSN != nil || finishLSN != nil {
		meta := ExtendedMetadataDto{
			StartLsn:  lsnValue(startLSN),
			FinishLsn: lsnValue(finishLSN),
		}
		putMetadata(root, name, meta)
	}
}

func lsnValue(lsn *LSN) LSN {
	if lsn == nil {
		return 0
	}
	return *lsn
}

func putBackupSentinel(root storage.Folder, name string, data []byte) {
	path := utility.BaseBackupPath + internal.SentinelNameFromBackup(name)
	root.PutObject(path, bytes.NewReader(data))
}

func putFilesMeta(root storage.Folder, name string, meta interface{}) {
	data, err := json.Marshal(meta)
	if err != nil {
		panic(err)
	}
	path := utility.BaseBackupPath + name + "/" + FilesMetadataName
	root.PutObject(path, bytes.NewReader(data))
}

func putMetadata(root storage.Folder, name string, meta ExtendedMetadataDto) {
	data, err := json.Marshal(meta)
	if err != nil {
		panic(err)
	}
	path := utility.BaseBackupPath + name + "/" + utility.MetadataFileName
	root.PutObject(path, bytes.NewReader(data))
}

func putTarParts(root storage.Folder, name string, parts map[string][]byte) {
	if parts == nil {
		return
	}
	for partName, data := range parts {
		path := utility.BaseBackupPath + name + internal.TarPartitionFolderName + partName
		root.PutObject(path, bytes.NewReader(data))
	}
}

func putWALFiles(root storage.Folder, files map[string][]byte) {
	if files == nil {
		return
	}
	for name, data := range files {
		root.PutObject(utility.WalPath+name, bytes.NewReader(data))
	}
}

func buildTarPart(files map[string][]byte) []byte {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, content := range files {
		tw.WriteHeader(&tar.Header{
			Name:     name,
			Size:     int64(len(content)),
			Typeflag: tar.TypeReg,
			Mode:     0644,
		})
		tw.Write(content)
	}
	tw.Close()
	return buf.Bytes()
}

func setupBackupWithSentinelFixture(t *testing.T, fixturePath string) (storage.Folder, *BackupSentinelDto, FilesMetadataDto) {
	t.Helper()

	fixtureFullPath := filepath.Join("..", "..", "..", fixturePath)
	data, err := os.ReadFile(fixtureFullPath)
	if err != nil {
		t.Fatalf("failed to read fixture %s: %v", fixturePath, err)
	}

	var sentinel BackupSentinelDto
	if err := json.Unmarshal(data, &sentinel); err != nil {
		t.Fatalf("failed to unmarshal sentinel from %s: %v", fixturePath, err)
	}

	var filesMeta FilesMetadataDto
	json.Unmarshal(data, &filesMeta)

	if len(filesMeta.Files) == 0 {
		filesMeta.Files = make(internal.BackupFileList)
	}
	if len(filesMeta.TarFileSets) == 0 {
		filesMeta.TarFileSets = make(map[string][]string)
	}

	root := memory.NewFolder("", memory.NewKVS())

	backup := Backup{Backup: internal.Backup{Name: testBackupName, Folder: root.GetSubFolder(utility.BaseBackupPath)}}
	backup.SentinelDto = &sentinel
	backup.FilesMetadataDto = &filesMeta

	putBackupSentinel(root, testBackupName, data)

	// Create empty tar parts for every referenced part (even as empty placeholder)
	// so manifest completeness doesn't report them as missing.
	tarParts := make(map[string][]byte)
	for partName := range filesMeta.TarFileSets {
		tarParts[partName] = []byte{}
	}
	putTarParts(root, testBackupName, tarParts)

	return root, &sentinel, filesMeta
}

func emptyTarParts(filesMeta FilesMetadataDto) map[string][]byte {
	parts := make(map[string][]byte)
	for name := range filesMeta.TarFileSets {
		parts[name] = []byte{}
	}
	return parts
}

func init() {
	time.Local = time.UTC
}
