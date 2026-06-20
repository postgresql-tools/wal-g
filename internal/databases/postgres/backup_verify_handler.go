package postgres

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"time"

	"github.com/wal-g/tracelog"

	"github.com/lateos-ai/wal-g/internal"
	"github.com/lateos-ai/wal-g/internal/crypto"
	"github.com/lateos-ai/wal-g/pkg/storages/storage"
	"github.com/lateos-ai/wal-g/utility"
)

type BackupVerifyTier int

const (
	Tier1 BackupVerifyTier = 1
	Tier2 BackupVerifyTier = 2
)

func (t BackupVerifyTier) MarshalText() ([]byte, error) {
	if t == Tier1 {
		return []byte("1"), nil
	}
	return []byte("2"), nil
}

type VerifyFileResult struct {
	Name             string `json:"name"`
	StoredSHA256     string `json:"stored_sha256,omitempty"`
	ComputedSHA256   string `json:"computed_sha256,omitempty"`
	Match            bool   `json:"match"`
	NoStoredChecksum bool   `json:"no_stored_checksum,omitempty"`
	ReadError        string `json:"read_error,omitempty"`
}

type ChecksumCoverage struct {
	HasChecksum int `json:"has_checksum"`
	NoChecksum  int `json:"no_checksum"`
	Total       int `json:"total"`
}

type BackupVerifyResult struct {
	BackupName string           `json:"backup_name"`
	Tier       BackupVerifyTier `json:"tier"`
	Pass       bool             `json:"pass"`

	SentinelExists bool   `json:"sentinel_exists"`
	SentinelError  string `json:"sentinel_error,omitempty"`

	FilesMetadataExists bool `json:"files_metadata_exists"`

	MissingParts []string `json:"missing_parts,omitempty"`

	DeployMetadata interface{} `json:"deploy_metadata"`

	ChecksumCoverage ChecksumCoverage `json:"checksum_coverage"`

	WALCheckAvailable bool     `json:"wal_check_available"`
	WALCheckDetails   string   `json:"wal_check_details,omitempty"`
	WALGaps           []string `json:"wal_gaps,omitempty"`

	SamplePercent   int                `json:"sample_percent,omitempty"`
	ActualSamplePct float64            `json:"actual_sample_pct,omitempty"`
	SampledParts    int                `json:"sampled_parts,omitempty"`
	TotalParts      int                `json:"total_parts,omitempty"`
	FileResults     []VerifyFileResult `json:"file_results,omitempty"`
	Tier2Pass       *bool              `json:"tier2_pass,omitempty"`

	Elapsed string `json:"elapsed"`
	Error   string `json:"error,omitempty"`
}

type verifyTarInterpreter struct {
	filesMeta FilesMetadataDto
	results   []VerifyFileResult
}

func (v *verifyTarInterpreter) Interpret(reader io.Reader, header *tar.Header) error {
	if header.Typeflag != tar.TypeReg {
		return nil
	}

	h := sha256.New()
	n, err := io.Copy(h, reader)
	if err != nil {
		v.results = append(v.results, VerifyFileResult{
			Name:      header.Name,
			ReadError: err.Error(),
		})
		return nil
	}

	_ = n
	computed := hex.EncodeToString(h.Sum(nil))

	fd, ok := v.filesMeta.Files[header.Name]
	if !ok || fd.SHA256 == "" {
		v.results = append(v.results, VerifyFileResult{
			Name:             header.Name,
			ComputedSHA256:   computed,
			NoStoredChecksum: true,
		})
		return nil
	}

	match := fd.SHA256 == computed
	v.results = append(v.results, VerifyFileResult{
		Name:           header.Name,
		StoredSHA256:   fd.SHA256,
		ComputedSHA256: computed,
		Match:          match,
	})
	return nil
}

func HandleBackupVerify(
	rootFolder storage.Folder,
	backupName string,
	samplePct int,
	seed int64,
	targetLSN string,
	targetTime string,
	format string,
	output io.Writer,
) int {
	startTime := time.Now()
	result := &BackupVerifyResult{
		Pass:                true,
		BackupName:          backupName,
		Tier:                Tier1,
		SentinelExists:      true,
		FilesMetadataExists: true,
		DeployMetadata:      "none",
		WALCheckAvailable:   true,
	}

	backup, err := resolveBackup(rootFolder, backupName)
	if err != nil {
		result.Pass = false
		result.SentinelExists = false
		result.SentinelError = err.Error()
		writeBackupVerifyOutput(result, format, output, startTime)
		return 1
	}
	result.BackupName = backup.Name

	sentinel, filesMeta, err := backup.GetSentinelAndFilesMetadata()
	if err != nil {
		result.Pass = false
		result.SentinelExists = false
		result.SentinelError = err.Error()
		result.FilesMetadataExists = false
		writeBackupVerifyOutput(result, format, output, startTime)
		return 1
	}

	if len(filesMeta.Files) > 0 {
		result.FilesMetadataExists = true
	}

	err = checkManifestCompleteness(&backup, filesMeta, result)
	if err != nil {
		result.Pass = false
		tracelog.WarningLogger.Printf("manifest completeness check error: %v", err)
	}

	computeChecksumCoverage(filesMeta, result)

	extractDeployMetadata(sentinel, result)

	checkWALChain(rootFolder, backup.Name, result)

	if samplePct > 0 {
		result.Tier = Tier2
		result.SamplePercent = samplePct
		err = runTier2(&backup, filesMeta, samplePct, seed, result)
		if err != nil {
			tracelog.WarningLogger.Printf("Tier 2 error: %v", err)
		}
	}

	result.Pass = result.Pass && len(result.MissingParts) == 0 && result.SentinelExists && result.SentinelError == ""
	if result.Tier2Pass != nil && !*result.Tier2Pass {
		result.Pass = false
	}

	writeBackupVerifyOutput(result, format, output, startTime)
	return determineExitCode(result)
}

func resolveBackup(rootFolder storage.Folder, name string) (Backup, error) {
	baseBackupFolder := rootFolder.GetSubFolder(utility.BaseBackupPath)

	if name != "" {
		return NewBackup(baseBackupFolder, name)
	}

	backups, err := internal.GetBackups(baseBackupFolder)
	if err != nil {
		return Backup{}, fmt.Errorf("no backups found: %w", err)
	}

	latest := backups[len(backups)-1]
	return NewBackup(baseBackupFolder, latest.BackupName)
}

func checkManifestCompleteness(backup *Backup, filesMeta FilesMetadataDto, result *BackupVerifyResult) error {
	if len(filesMeta.TarFileSets) == 0 {
		return nil
	}

	tarPartitionFolder := backup.GetTarPartitionFolder()
	objects, _, err := tarPartitionFolder.ListFolder()
	if err != nil {
		return fmt.Errorf("failed to list tar partition folder: %w", err)
	}

	existing := make(map[string]bool, len(objects))
	for _, obj := range objects {
		existing[obj.GetName()] = true
	}

	for tarPart := range filesMeta.TarFileSets {
		if !existing[tarPart] {
			result.MissingParts = append(result.MissingParts, tarPart)
		}
	}

	return nil
}

func computeChecksumCoverage(filesMeta FilesMetadataDto, result *BackupVerifyResult) {
	c := ChecksumCoverage{Total: len(filesMeta.Files)}
	for _, fd := range filesMeta.Files {
		if fd.SHA256 != "" {
			c.HasChecksum++
		} else {
			c.NoChecksum++
		}
	}
	result.ChecksumCoverage = c
}

func extractDeployMetadata(sentinel BackupSentinelDto, result *BackupVerifyResult) {
	meta, ok := ExtractDeployMetadata(sentinel.UserData)
	if ok {
		result.DeployMetadata = meta
	}
}

func checkWALChain(rootFolder storage.Folder, backupName string, result *BackupVerifyResult) {
	walFolder := rootFolder.GetSubFolder(utility.WalPath)
	walFilenames, err := getFolderFilenames(walFolder)
	if err != nil {
		result.WALCheckAvailable = false
		result.WALCheckDetails = fmt.Sprintf("failed to list WAL folder: %v", err)
		return
	}

	if len(walFilenames) == 0 {
		result.WALCheckAvailable = false
		result.WALCheckDetails = "WAL folder is empty"
		return
	}

	latestSeg := findLatestWalSegment(walFilenames)
	if latestSeg == nil {
		result.WALCheckAvailable = false
		result.WALCheckDetails = "no valid WAL segments found"
		return
	}

	searchParams := BackupSearchParams{
		FindEarliestBackup:  false,
		SpecifiedBackupName: &backupName,
	}

	runner, err := NewIntegrityCheckRunner(rootFolder, walFilenames, *latestSeg, searchParams)
	if err != nil {
		result.WALCheckAvailable = false
		result.WALCheckDetails = fmt.Sprintf("WAL check unavailable: %v", err)
		return
	}

	checkResult, err := runner.Run()
	if err != nil {
		result.WALCheckAvailable = false
		result.WALCheckDetails = fmt.Sprintf("WAL check error: %v", err)
		return
	}

	if checkResult.Status == StatusOk {
		result.WALCheckDetails = "no gaps found"
		return
	}

	gaps := extractGapDetails(checkResult)
	result.WALGaps = gaps
	if len(gaps) > 0 {
		result.WALCheckDetails = fmt.Sprintf("%d gap(s) found", len(gaps))
	} else {
		result.WALCheckDetails = "unexpected status"
	}
}

func findLatestWalSegment(filenames []string) *WalSegmentDescription {
	var latest *WalSegmentDescription
	for _, filename := range filenames {
		baseName := utility.TrimFileExtension(filename)
		seg, err := NewWalSegmentDescription(baseName)
		if err != nil {
			continue
		}
		if latest == nil || seg.Timeline > latest.Timeline ||
			(seg.Timeline == latest.Timeline && seg.Number > latest.Number) {
			s := seg
			latest = &s
		}
	}
	return latest
}

func extractGapDetails(result WalVerifyCheckResult) []string {
	reader, err := result.Details.NewPlainTextReader()
	if err != nil {
		return []string{fmt.Sprintf("failed to read details: %v", err)}
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return []string{fmt.Sprintf("failed to read details: %v", err)}
	}
	if len(data) == 0 {
		return nil
	}
	return []string{string(data)}
}

func runTier2(backup *Backup, filesMeta FilesMetadataDto, samplePct int, seed int64, result *BackupVerifyResult) error {
	tarParts := make([]string, 0, len(filesMeta.TarFileSets))
	for part := range filesMeta.TarFileSets {
		tarParts = append(tarParts, part)
	}

	if len(tarParts) == 0 {
		// legacy backup without TarFileSets - list from folder
		var err error
		tarParts, err = backup.GetTarNames()
		if err != nil {
			return fmt.Errorf("failed to list tar parts: %w", err)
		}
	}

	result.TotalParts = len(tarParts)

	var rng *rand.Rand
	if seed != 0 {
		rng = rand.New(rand.NewSource(seed))
	} else {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}

	sampleCount := (len(tarParts)*samplePct + 99) / 100
	if sampleCount <= 0 {
		sampleCount = 1
	}
	if sampleCount > len(tarParts) {
		sampleCount = len(tarParts)
	}

	rng.Shuffle(len(tarParts), func(i, j int) {
		tarParts[i], tarParts[j] = tarParts[j], tarParts[i]
	})
	sampled := tarParts[:sampleCount]
	result.SampledParts = len(sampled)
	result.ActualSamplePct = float64(len(sampled)) / float64(len(tarParts)) * 100

	crypter := internal.ConfigureCrypter()
	tarPartitionFolder := backup.GetTarPartitionFolder()
	interp := &verifyTarInterpreter{filesMeta: filesMeta}

	for _, partName := range sampled {
		err := verifyTarPart(tarPartitionFolder, partName, crypter, interp)
		if err != nil {
			interp.results = append(interp.results, VerifyFileResult{
				Name:      partName,
				ReadError: err.Error(),
			})
		}
	}

	result.FileResults = interp.results

	mismatches := 0
	for _, r := range interp.results {
		if !r.NoStoredChecksum && !r.Match && r.ReadError == "" {
			mismatches++
		}
	}
	pass := mismatches == 0
	result.Tier2Pass = &pass

	return nil
}

func verifyTarPart(folder storage.Folder, partName string, crypter crypto.Crypter, interp *verifyTarInterpreter) error {
	reader, err := folder.ReadObject(partName)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", partName, err)
	}
	defer reader.Close()

	decompressed, err := internal.DecryptAndDecompressTar(reader, partName, crypter)
	if err != nil {
		return fmt.Errorf("failed to decrypt/decompress %s: %w", partName, err)
	}
	defer decompressed.Close()

	tarReader := tar.NewReader(decompressed)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar error in %s: %w", partName, err)
		}
		if err := interp.Interpret(tarReader, header); err != nil {
			return fmt.Errorf("verify error in %s entry %s: %w", partName, header.Name, err)
		}
	}

	return nil
}

func writeBackupVerifyOutput(result *BackupVerifyResult, format string, output io.Writer, startTime time.Time) {
	result.Elapsed = time.Since(startTime).Round(time.Millisecond).String()

	switch format {
	case "json":
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			fmt.Fprintf(output, `{"error":%q}`, err.Error())
			return
		}
		if _, err := output.Write(data); err != nil {
			tracelog.ErrorLogger.Printf("failed to write JSON output: %v", err)
		}
		if _, err := output.Write([]byte("\n")); err != nil {
			tracelog.ErrorLogger.Printf("failed to write JSON output: %v", err)
		}
	default:
		writeTextOutput(result, output)
	}
}

func writeTextOutput(result *BackupVerifyResult, output io.Writer) {
	fmt.Fprintf(output, "Backup: %s\n", result.BackupName)
	fmt.Fprintf(output, "Tier: %d\n", result.Tier)

	if result.Error != "" {
		fmt.Fprintf(output, "Error: %s\n", result.Error)
		return
	}

	fmt.Fprintf(output, "\n--- Tier 1: Metadata Verification ---\n")
	fmt.Fprintf(output, "Sentinel: ")
	if result.SentinelError != "" {
		fmt.Fprintf(output, "FAIL (%s)\n", result.SentinelError)
	} else {
		fmt.Fprintf(output, "OK\n")
	}

	fmt.Fprintf(output, "Files metadata: ")
	if !result.FilesMetadataExists {
		fmt.Fprintf(output, "NOT FOUND\n")
	} else {
		fmt.Fprintf(output, "OK (%d file(s))\n", result.ChecksumCoverage.Total)
	}

	fmt.Fprintf(output, "Checksum coverage: %d/%d files have stored checksums\n",
		result.ChecksumCoverage.HasChecksum,
		result.ChecksumCoverage.Total)

	if len(result.MissingParts) > 0 {
		fmt.Fprintf(output, "Missing parts: %d\n", len(result.MissingParts))
		for _, p := range result.MissingParts {
			fmt.Fprintf(output, "  - %s\n", p)
		}
	} else {
		fmt.Fprintf(output, "Missing parts: none\n")
	}

	fmt.Fprintf(output, "Deploy metadata: ")
	switch d := result.DeployMetadata.(type) {
	case DeployMetadata:
		fmt.Fprintf(output, "commit=%s branch=%s deploy_id=%s\n", d.GitCommit, d.GitBranch, d.DeployID)
	case string:
		fmt.Fprintf(output, "%s\n", d)
	default:
		fmt.Fprintf(output, "%v\n", d)
	}

	fmt.Fprintf(output, "WAL chain: ")
	if !result.WALCheckAvailable {
		fmt.Fprintf(output, "UNAVAILABLE (%s)\n", result.WALCheckDetails)
	} else if len(result.WALGaps) > 0 {
		fmt.Fprintf(output, "GAPS FOUND (%s)\n", result.WALCheckDetails)
	} else {
		fmt.Fprintf(output, "OK\n")
	}

	if result.Tier == Tier2 {
		fmt.Fprintf(output, "\n--- Tier 2: Spot-Check Verification ---\n")
		fmt.Fprintf(output, "Sampled: %d/%d parts (%.1f%%)\n",
			result.SampledParts, result.TotalParts, result.ActualSamplePct)

		mismatches := 0
		readabilityOnly := 0
		matched := 0
		for _, r := range result.FileResults {
			if r.NoStoredChecksum {
				readabilityOnly++
			} else if r.Match {
				matched++
			} else if r.ReadError != "" {
				mismatches++
			} else {
				mismatches++
			}
		}

		fmt.Fprintf(output, "Files verified: %d\n", len(result.FileResults))
		fmt.Fprintf(output, "  Matched:      %d\n", matched)
		fmt.Fprintf(output, "  Mismatched:   %d\n", mismatches)
		fmt.Fprintf(output, "  Readability:  %d\n", readabilityOnly)

		for _, r := range result.FileResults {
			if r.ReadError != "" {
				fmt.Fprintf(output, "  ERROR: %s: %s\n", r.Name, r.ReadError)
			} else if !r.NoStoredChecksum && !r.Match {
				fmt.Fprintf(output, "  MISMATCH: %s\n", r.Name)
				fmt.Fprintf(output, "    stored:   %s\n", r.StoredSHA256)
				fmt.Fprintf(output, "    computed: %s\n", r.ComputedSHA256)
			}
		}

		if result.Tier2Pass != nil {
			if *result.Tier2Pass {
				fmt.Fprintf(output, "Status: no issues detected at this verification tier\n")
			} else {
				fmt.Fprintf(output, "Status: CORRUPTED - %d file(s) failed checksum verification\n", mismatches)
			}
		}
	} else {
		if result.Pass {
			fmt.Fprintf(output, "\nStatus: no issues detected at this verification tier\n")
		}
	}

	fmt.Fprintf(output, "Elapsed: %s\n", result.Elapsed)
}

func determineExitCode(result *BackupVerifyResult) int {
	if result.Error != "" {
		// storage unreachable or similar infrastructure error
		if result.SentinelError != "" && !result.SentinelExists {
			return 2
		}
		return 1
	}

	if !result.SentinelExists || len(result.MissingParts) > 0 {
		return 1
	}

	if result.Tier2Pass != nil && !*result.Tier2Pass {
		return 1
	}

	return 0
}
