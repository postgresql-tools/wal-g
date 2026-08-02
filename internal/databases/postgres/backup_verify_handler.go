// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

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

	"github.com/spf13/viper"
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

func (t *BackupVerifyTier) UnmarshalText(data []byte) error {
	switch string(data) {
	case "1":
		*t = Tier1
	case "2":
		*t = Tier2
	default:
		return fmt.Errorf("unknown backup verify tier %q", data)
	}
	return nil
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

// DecryptCanaryResult reports whether the crypter and compression codec configured
// on *this* host can actually open the backup's data. The rest of Tier 1 is
// metadata-only, so without this check a green Tier 1 says nothing about whether
// the key available here can decrypt the bytes - the exact failure that only
// surfaces during a restore, when it is too late to do anything about it.
//
// One partition (the smallest) is fetched and the first tar header is read. Nothing
// else is downloaded, so the cost stays in the "near-zero egress" budget Tier 1 promises.
type DecryptCanaryResult struct {
	Attempted  bool   `json:"attempted"`
	Pass       bool   `json:"pass"`
	SkipReason string `json:"skip_reason,omitempty"`
	Part       string `json:"part,omitempty"`
	PartSize   int64  `json:"part_size,omitempty"`
	Crypter    string `json:"crypter,omitempty"`
	TarEntries int    `json:"tar_entries"`
	Error      string `json:"error,omitempty"`
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

	DecryptCanary DecryptCanaryResult `json:"decrypt_canary"`

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

// BackupVerifyOptions carries the tunables for a single backup-verify run.
type BackupVerifyOptions struct {
	// BackupName is the backup to verify; empty means LATEST.
	BackupName string
	// SamplePct > 0 promotes the run to Tier 2 and sets the partition sample size.
	SamplePct int
	// Seed makes Tier 2 sampling reproducible; 0 means time-based.
	Seed int64
	// TargetLSN and TargetTime bound the WAL chain verification scope.
	TargetLSN  string
	TargetTime string
	// Format is "text" or "json".
	Format string
	// SkipCanary disables the Tier 1 decrypt canary, making the run purely
	// metadata-based (no object data is fetched at all).
	SkipCanary bool
}

func HandleBackupVerify(
	rootFolder storage.Folder,
	opts BackupVerifyOptions,
	output io.Writer,
) int {
	startTime := time.Now()
	result := &BackupVerifyResult{
		Pass:                true,
		BackupName:          opts.BackupName,
		Tier:                Tier1,
		SentinelExists:      true,
		FilesMetadataExists: true,
		DeployMetadata:      "none",
		WALCheckAvailable:   true,
	}

	backup, err := resolveBackup(rootFolder, opts.BackupName)
	if err != nil {
		result.Pass = false
		result.SentinelExists = false
		result.SentinelError = err.Error()
		writeBackupVerifyOutput(result, opts.Format, output, startTime)
		return 1
	}
	result.BackupName = backup.Name

	sentinel, filesMeta, err := backup.GetSentinelAndFilesMetadata()
	if err != nil {
		result.Pass = false
		result.SentinelExists = false
		result.SentinelError = err.Error()
		result.FilesMetadataExists = false
		writeBackupVerifyOutput(result, opts.Format, output, startTime)
		return 1
	}

	if len(filesMeta.Files) > 0 {
		result.FilesMetadataExists = true
	}

	partObjects, err := checkManifestCompleteness(&backup, filesMeta, result)
	if err != nil {
		result.Pass = false
		tracelog.WarningLogger.Printf("manifest completeness check error: %v", err)
	}

	computeChecksumCoverage(filesMeta, result)

	extractDeployMetadata(sentinel, result)

	runDecryptCanary(&backup, filesMeta, partObjects, opts, result)

	checkWALChain(rootFolder, backup.Name, result)

	if opts.SamplePct > 0 {
		result.Tier = Tier2
		result.SamplePercent = opts.SamplePct
		err = runTier2(&backup, filesMeta, opts.SamplePct, opts.Seed, result)
		if err != nil {
			tracelog.WarningLogger.Printf("Tier 2 error: %v", err)
		}
	}

	result.Pass = result.Pass && len(result.MissingParts) == 0 && result.SentinelExists && result.SentinelError == ""
	if result.DecryptCanary.Attempted && !result.DecryptCanary.Pass {
		result.Pass = false
	}
	if result.Tier2Pass != nil && !*result.Tier2Pass {
		result.Pass = false
	}

	writeBackupVerifyOutput(result, opts.Format, output, startTime)
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

// checkManifestCompleteness reports partitions the manifest references but storage
// does not have. It returns the partition listing so later checks can reuse it
// instead of paying for a second LIST.
func checkManifestCompleteness(
	backup *Backup,
	filesMeta FilesMetadataDto,
	result *BackupVerifyResult,
) ([]storage.Object, error) {
	if len(filesMeta.TarFileSets) == 0 {
		return nil, nil
	}

	tarPartitionFolder := backup.GetTarPartitionFolder()
	objects, _, err := tarPartitionFolder.ListFolder()
	if err != nil {
		return nil, fmt.Errorf("failed to list tar partition folder: %w", err)
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

	return objects, nil
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

// runDecryptCanary fetches the smallest tar partition and proves it can be
// decrypted, decompressed, and parsed as a tar stream with the configuration
// present on this host.
//
// It is skipped when Tier 2 is running, because Tier 2 decrypts a whole sample of
// partitions and would make this redundant.
func runDecryptCanary(
	backup *Backup,
	filesMeta FilesMetadataDto,
	partObjects []storage.Object,
	opts BackupVerifyOptions,
	result *BackupVerifyResult,
) {
	switch {
	case opts.SkipCanary:
		result.DecryptCanary.SkipReason = "disabled by --no-canary"
		return
	case opts.SamplePct > 0:
		result.DecryptCanary.SkipReason = "covered by Tier 2 sampling"
		return
	}

	smallest := smallestVerifiablePart(filesMeta, partObjects)
	if smallest == nil {
		result.DecryptCanary.SkipReason = "no tar partitions available to sample"
		return
	}

	result.DecryptCanary.Attempted = true
	result.DecryptCanary.Part = smallest.GetName()
	result.DecryptCanary.PartSize = smallest.GetSize()

	// Deliberately not internal.ConfigureCrypter: that variant calls Fatal on a
	// misconfigured crypter, and "the key is not usable here" is precisely the
	// finding this check exists to report rather than crash on.
	crypter, err := internal.ConfigureCrypterForSpecificConfig(viper.GetViper())
	if err != nil {
		result.DecryptCanary.Error = fmt.Sprintf("failed to configure crypter: %v", err)
		return
	}

	result.DecryptCanary.Crypter = "none"
	if crypter != nil {
		result.DecryptCanary.Crypter = crypter.Name()
	}

	entries, err := readCanaryPart(backup.GetTarPartitionFolder(), smallest.GetName(), crypter)
	if err != nil {
		result.DecryptCanary.Error = err.Error()
		return
	}

	result.DecryptCanary.TarEntries = entries
	result.DecryptCanary.Pass = true
}

// smallestVerifiablePart picks the cheapest partition to fetch. Only partitions the
// manifest actually references are considered, so unrelated objects that happen to
// share the folder are never chosen.
func smallestVerifiablePart(filesMeta FilesMetadataDto, partObjects []storage.Object) storage.Object {
	var smallest storage.Object
	for _, obj := range partObjects {
		if len(filesMeta.TarFileSets) > 0 {
			if _, referenced := filesMeta.TarFileSets[obj.GetName()]; !referenced {
				continue
			}
		}
		if smallest == nil || obj.GetSize() < smallest.GetSize() {
			smallest = obj
		}
	}
	return smallest
}

// readCanaryPart opens a partition and reads only its first tar header. Entry
// bodies are never read, so the transfer is bounded to the head of the object
// regardless of how large the partition is.
func readCanaryPart(folder storage.Folder, partName string, crypter crypto.Crypter) (int, error) {
	reader, err := folder.ReadObject(partName)
	if err != nil {
		return 0, fmt.Errorf("failed to read %s: %w", partName, err)
	}
	defer reader.Close()

	decompressed, err := internal.DecryptAndDecompressTar(reader, partName, crypter)
	if err != nil {
		return 0, fmt.Errorf("failed to decrypt/decompress %s: %w", partName, err)
	}
	defer decompressed.Close()

	_, err = tar.NewReader(decompressed).Next()
	if err == io.EOF {
		// A partition with no entries still decrypted and parsed cleanly, which is
		// all this check claims to establish.
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("failed to parse tar stream in %s: %w", partName, err)
	}

	return 1, nil
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

	fmt.Fprintf(output, "Decrypt canary: ")
	switch {
	case !result.DecryptCanary.Attempted:
		fmt.Fprintf(output, "SKIPPED (%s)\n", result.DecryptCanary.SkipReason)
	case result.DecryptCanary.Pass:
		fmt.Fprintf(output, "OK (part %s, %d bytes, crypter=%s)\n",
			result.DecryptCanary.Part, result.DecryptCanary.PartSize, result.DecryptCanary.Crypter)
	default:
		fmt.Fprintf(output, "FAIL (part %s: %s)\n",
			result.DecryptCanary.Part, result.DecryptCanary.Error)
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

	if result.DecryptCanary.Attempted && !result.DecryptCanary.Pass {
		return 1
	}

	if result.Tier2Pass != nil && !*result.Tier2Pass {
		return 1
	}

	return 0
}
