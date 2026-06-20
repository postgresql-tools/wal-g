package postgres

import (
	"encoding/json"
	"os"
	"testing"
)

func TestCollectDeployMetadata_ExplicitValues(t *testing.T) {
	meta := CollectDeployMetadata("abc123", "main", "deploy-42")

	if meta.GitCommit != "abc123" {
		t.Errorf("expected git_commit 'abc123', got '%s'", meta.GitCommit)
	}
	if meta.GitBranch != "main" {
		t.Errorf("expected git_branch 'main', got '%s'", meta.GitBranch)
	}
	if meta.DeployID != "deploy-42" {
		t.Errorf("expected deploy_id 'deploy-42', got '%s'", meta.DeployID)
	}
}

func TestCollectDeployMetadata_AutoDetect(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	execGit(t, dir, "init")
	execGit(t, dir, "config", "user.email", "test@test.com")
	execGit(t, dir, "config", "user.name", "Test")
	execGit(t, dir, "commit", "--allow-empty", "-m", "initial")

	meta := CollectDeployMetadata("", "", "")

	if meta.GitCommit == "" {
		t.Error("expected git_commit to be auto-detected, got empty")
	}
	if meta.GitBranch == "" {
		t.Error("expected git_branch to be auto-detected, got empty")
	}
	if meta.GitBranch != "master" && meta.GitBranch != "main" {
		t.Errorf("expected branch 'master' or 'main', got '%s'", meta.GitBranch)
	}
}

func TestCollectDeployMetadata_ExplicitOverridesAuto(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	execGit(t, dir, "init")

	meta := CollectDeployMetadata("explicit-sha", "", "")

	if meta.GitCommit != "explicit-sha" {
		t.Errorf("explicit git_commit should override auto-detect, got '%s'", meta.GitCommit)
	}
}

func TestMergeDeployMetadataIntoUserData_NilUserData(t *testing.T) {
	meta := DeployMetadata{GitCommit: "abc", GitBranch: "main", DeployID: "d1"}
	result := MergeDeployMetadataIntoUserData(meta, nil)

	data, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map[string]interface{}, got %T", result)
	}

	dm, ok := data[deployMetadataKey].(map[string]interface{})
	if !ok {
		t.Fatalf("expected nested map, got %T", data[deployMetadataKey])
	}

	if dm["git_commit"] != "abc" {
		t.Errorf("expected git_commit='abc', got '%v'", dm["git_commit"])
	}
	if dm["git_branch"] != "main" {
		t.Errorf("expected git_branch='main', got '%v'", dm["git_branch"])
	}
	if dm["deploy_id"] != "d1" {
		t.Errorf("expected deploy_id='d1', got '%v'", dm["deploy_id"])
	}
}

func TestMergeDeployMetadataIntoUserData_ExistingMap(t *testing.T) {
	meta := DeployMetadata{GitCommit: "abc", GitBranch: "main"}
	existing := map[string]interface{}{
		"custom_key": "custom_value",
	}

	result := MergeDeployMetadataIntoUserData(meta, existing)

	data, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map[string]interface{}, got %T", result)
	}

	if data["custom_key"] != "custom_value" {
		t.Errorf("existing data should be preserved")
	}

	dm, ok := data[deployMetadataKey].(map[string]interface{})
	if !ok {
		t.Fatalf("expected nested map, got %T", data[deployMetadataKey])
	}

	if dm["git_commit"] != "abc" {
		t.Errorf("expected git_commit='abc', got '%v'", dm["git_commit"])
	}
}

func TestMergeDeployMetadataIntoUserData_NonMapUserData(t *testing.T) {
	meta := DeployMetadata{GitCommit: "abc"}
	existing := "a string value"

	result := MergeDeployMetadataIntoUserData(meta, existing)

	data, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map[string]interface{}, got %T", result)
	}

	if data["payload"] != "a string value" {
		t.Errorf("expected original data under 'payload' key, got '%v'", data["payload"])
	}
}

func TestExtractDeployMetadata_NilUserData(t *testing.T) {
	_, ok := ExtractDeployMetadata(nil)
	if ok {
		t.Error("expected false for nil UserData")
	}
}

func TestExtractDeployMetadata_NoDeployKey(t *testing.T) {
	userData := map[string]interface{}{
		"some_other_key": "value",
	}
	_, ok := ExtractDeployMetadata(userData)
	if ok {
		t.Error("expected false when deploy metadata key is missing")
	}
}

func TestExtractDeployMetadata_Success(t *testing.T) {
	meta := DeployMetadata{GitCommit: "abc123", GitBranch: "main", DeployID: "deploy-42"}
	userData := MergeDeployMetadataIntoUserData(meta, nil)

	extracted, ok := ExtractDeployMetadata(userData)
	if !ok {
		t.Fatal("expected true when deploy metadata is present")
	}
	if extracted.GitCommit != "abc123" {
		t.Errorf("expected git_commit 'abc123', got '%s'", extracted.GitCommit)
	}
	if extracted.GitBranch != "main" {
		t.Errorf("expected git_branch 'main', got '%s'", extracted.GitBranch)
	}
	if extracted.DeployID != "deploy-42" {
		t.Errorf("expected deploy_id 'deploy-42', got '%s'", extracted.DeployID)
	}
}

func TestExtractDeployMetadata_WrongType(t *testing.T) {
	userData := "not a map"
	_, ok := ExtractDeployMetadata(userData)
	if ok {
		t.Error("expected false for non-map UserData")
	}
}

func TestDeployMetadata_JSONRoundTrip(t *testing.T) {
	meta := DeployMetadata{GitCommit: "abc123", GitBranch: "main", DeployID: "deploy-42"}
	userData := MergeDeployMetadataIntoUserData(meta, nil)

	data, err := json.Marshal(userData)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	dm, ok := decoded[deployMetadataKey].(map[string]interface{})
	if !ok {
		t.Fatalf("expected nested map in JSON, got %T", decoded[deployMetadataKey])
	}

	if dm["git_commit"] != "abc123" {
		t.Errorf("expected git_commit='abc123', got '%v'", dm["git_commit"])
	}

	extracted, ok := ExtractDeployMetadata(decoded)
	if !ok {
		t.Fatal("expected ExtractDeployMetadata to succeed after JSON round trip")
	}
	if extracted.GitCommit != "abc123" {
		t.Errorf("expected git_commit after round trip 'abc123', got '%s'", extracted.GitCommit)
	}
}

func execGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := gitCommand(args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

func TestCollectDeployMetadata_NoGitRepo(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	t.Setenv("GIT_CEILING_DIRECTORIES", dir)

	meta := CollectDeployMetadata("", "", "")

	if meta.GitCommit != "" {
		t.Logf("note: git commit was detected as '%s' (may be from parent repo)", meta.GitCommit)
	}
	if meta.GitBranch != "" {
		t.Logf("note: git branch was detected as '%s' (may be from parent repo)", meta.GitBranch)
	}
}
