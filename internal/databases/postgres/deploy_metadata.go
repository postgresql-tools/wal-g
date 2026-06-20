package postgres

import (
	"encoding/json"
	"os/exec"
	"runtime"
	"strings"
)

const deployMetadataKey = "walg_deploy_metadata"

type DeployMetadata struct {
	GitCommit string `json:"git_commit,omitempty"`
	GitBranch string `json:"git_branch,omitempty"`
	DeployID  string `json:"deploy_id,omitempty"`
}

func CollectDeployMetadata(gitCommit, gitBranch, deployID string) DeployMetadata {
	meta := DeployMetadata{
		DeployID: deployID,
	}

	if gitCommit != "" {
		meta.GitCommit = gitCommit
	} else {
		meta.GitCommit = detectGitCommit()
	}

	if gitBranch != "" {
		meta.GitBranch = gitBranch
	} else {
		meta.GitBranch = detectGitBranch()
	}

	return meta
}

func ExtractDeployMetadata(userData interface{}) (DeployMetadata, bool) {
	if userData == nil {
		return DeployMetadata{}, false
	}
	m, ok := userData.(map[string]interface{})
	if !ok {
		return DeployMetadata{}, false
	}
	raw, ok := m[deployMetadataKey]
	if !ok {
		return DeployMetadata{}, false
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return DeployMetadata{}, false
	}
	var meta DeployMetadata
	err = json.Unmarshal(data, &meta)
	if err != nil {
		return DeployMetadata{}, false
	}
	return meta, true
}

func MergeDeployMetadataIntoUserData(meta DeployMetadata, userData interface{}) interface{} {
	metaMap := make(map[string]interface{})
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return userData
	}
	if err := json.Unmarshal(metaJSON, &metaMap); err != nil {
		return userData
	}

	if userData == nil {
		return map[string]interface{}{
			deployMetadataKey: metaMap,
		}
	}

	switch ud := userData.(type) {
	case map[string]interface{}:
		ud[deployMetadataKey] = metaMap
		return ud
	default:
		return map[string]interface{}{
			deployMetadataKey: metaMap,
			"payload":         userData,
		}
	}
}

func gitCommand(args ...string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		gitArgs := append([]string{"/c", "git"}, args...)
		gitArgs = append(gitArgs, "2>NUL")
		return exec.Command("cmd", gitArgs...)
	}
	return exec.Command("git", args...)
}

func detectGitCommit() string {
	out, err := gitCommand("rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func detectGitBranch() string {
	out, err := gitCommand("rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
