// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

package postgres

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lateos-ai/wal-g/pkg/neon"
)

// fakeNeonClient records what the drill asked the control plane to do, so a
// test can assert on the calls rather than on a live Neon project.
type fakeNeonClient struct {
	pingErr   error
	createErr error
	uriErr    error
	deleteErr error

	created []string
	deleted []string
	uri     string
}

func (c *fakeNeonClient) Ping(context.Context) error {
	return c.pingErr
}

func (c *fakeNeonClient) CreateBranch(_ context.Context, name, _ string) (neon.Branch, error) {
	if c.createErr != nil {
		return neon.Branch{}, c.createErr
	}

	c.created = append(c.created, name)

	return neon.Branch{ID: "br-" + name, Name: name}, nil
}

func (c *fakeNeonClient) DeleteBranch(_ context.Context, branch neon.Branch) error {
	if c.deleteErr != nil {
		return c.deleteErr
	}

	c.deleted = append(c.deleted, branch.Name)

	return nil
}

func (c *fakeNeonClient) ConnectionURI(context.Context, string, string, string) (string, error) {
	if c.uriErr != nil {
		return "", c.uriErr
	}

	if c.uri != "" {
		return c.uri, nil
	}

	return "postgresql://owner:pw@ep-1.neon.tech/neondb?sslmode=require", nil
}

// The restore is the expensive part. Bad credentials must stop the drill before
// it spends an hour fetching a backup that has nowhere to go.
func TestHandleNeonDrill_FailsBeforeRestoringWhenCredentialsAreBad(t *testing.T) {
	client := &fakeNeonClient{pingErr: errors.New("401: bad credentials")}

	opts := &NeonDrillOptions{
		RestoreDrillOptions: RestoreDrillOptions{
			TargetDir: filepath.Join(t.TempDir(), "drill"),
			Format:    "json",
		},
		Client:    client,
		ProjectID: "proj-123",
	}

	var out bytes.Buffer

	if code := HandleNeonDrill(nil, opts, &out); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}

	if len(client.created) != 0 {
		t.Fatalf("a branch was created despite failed auth: %v", client.created)
	}

	report := out.String()

	if !strings.Contains(report, DrillCheckNeonAuth) {
		t.Fatalf("report should name the neon-auth phase:\n%s", report)
	}

	// The restore phases must not have run.
	if strings.Contains(report, `"name": "`+DrillCheckFetch+`"`) {
		t.Fatalf("the fetch phase ran despite failed auth:\n%s", report)
	}
}

// A branch is a running compute endpoint. A load that fails must not leave one
// behind, because the failure is exactly when nobody is watching.
func TestRunNeonBranchPhases_DeletesTheBranchWhenTheLoadFails(t *testing.T) {
	client := &fakeNeonClient{}

	// An empty target directory has no PG_VERSION, so the version check fails
	// and the load never reaches pg_dump.
	opts := &NeonDrillOptions{
		RestoreDrillOptions: RestoreDrillOptions{TargetDir: t.TempDir()},
		Client:              client,
	}
	applyNeonDefaults(opts)

	report := &NeonDrillReport{}

	runNeonBranchPhases(report, opts, client)

	if len(client.created) != 1 {
		t.Fatalf("expected one branch to be created, got %v", client.created)
	}

	if len(client.deleted) != 1 {
		t.Fatalf("expected the branch to be deleted after a failed load, got %v", client.deleted)
	}

	if report.BranchRetained {
		t.Fatal("report claims the branch was retained, but it was deleted")
	}

	if report.Failed == 0 {
		t.Fatal("a failed load should fail the drill")
	}
}

func TestRunNeonBranchPhases_KeepBranchRetainsIt(t *testing.T) {
	client := &fakeNeonClient{}

	opts := &NeonDrillOptions{
		RestoreDrillOptions: RestoreDrillOptions{TargetDir: t.TempDir()},
		Client:              client,
		KeepBranch:          true,
	}
	applyNeonDefaults(opts)

	report := &NeonDrillReport{}

	runNeonBranchPhases(report, opts, client)

	if len(client.deleted) != 0 {
		t.Fatalf("--keep-branch should not delete anything, got %v", client.deleted)
	}

	if !report.BranchRetained {
		t.Fatal("report should say the branch was retained")
	}
}

// Cleanup that fails must say so. Silently reporting success would hide a
// branch that is still being paid for.
func TestNeonCleanupPhase_FailsWhenTheBranchSurvives(t *testing.T) {
	client := &fakeNeonClient{deleteErr: errors.New("control plane unavailable")}

	opts := &NeonDrillOptions{
		RestoreDrillOptions: RestoreDrillOptions{TargetDir: t.TempDir()},
		Client:              client,
	}
	applyNeonDefaults(opts)

	report := &NeonDrillReport{}

	runNeonBranchPhases(report, opts, client)

	if !report.BranchRetained {
		t.Fatal("a failed delete must leave BranchRetained set")
	}

	var cleanup *DrillPhase

	for i := range report.Phases {
		if report.Phases[i].Name == DrillCheckNeonCleanup {
			cleanup = &report.Phases[i]
		}
	}

	if cleanup == nil {
		t.Fatal("expected a neon-cleanup phase")
	}

	if cleanup.Status != DoctorFail {
		t.Fatalf("cleanup status = %s, want fail", cleanup.Status)
	}
}

func TestRunNeonBranchPhases_NamesTheBranchWithTheDrillPrefix(t *testing.T) {
	client := &fakeNeonClient{}

	opts := &NeonDrillOptions{
		RestoreDrillOptions: RestoreDrillOptions{TargetDir: t.TempDir()},
		Client:              client,
	}
	applyNeonDefaults(opts)

	runNeonBranchPhases(&NeonDrillReport{}, opts, client)

	if len(client.created) != 1 {
		t.Fatalf("expected one branch, got %v", client.created)
	}

	// Cleanup refuses to delete anything without this prefix, so a generated
	// name that lacked it would strand every branch the drill ever made.
	if !strings.HasPrefix(client.created[0], neon.DrillBranchPrefix) {
		t.Fatalf("branch name %q does not start with %q", client.created[0], neon.DrillBranchPrefix)
	}
}

// The password must reach psql through the environment. In argv it would be
// readable by every process on the host.
func TestPsqlEnvFromURI_PutsTheCredentialsInTheEnvironment(t *testing.T) {
	env, err := psqlEnvFromURI("postgresql://owner:s3cret@ep-1.neon.tech:5433/neondb?sslmode=verify-full")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]string{
		"PGHOST":     "ep-1.neon.tech",
		"PGPORT":     "5433",
		"PGDATABASE": "neondb",
		"PGUSER":     "owner",
		"PGPASSWORD": "s3cret",
		"PGSSLMODE":  "verify-full",
	}

	got := map[string]string{}

	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		got[key] = value
	}

	for key, value := range want {
		if got[key] != value {
			t.Errorf("%s = %q, want %q", key, got[key], value)
		}
	}
}

// Neon rejects plaintext connections, so a URI that omits sslmode must not end
// up disabling TLS by default.
func TestPsqlEnvFromURI_DefaultsToRequiringTLS(t *testing.T) {
	env, err := psqlEnvFromURI("postgresql://owner:pw@ep-1.neon.tech/neondb")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var sslMode string

	for _, entry := range env {
		if key, value, _ := strings.Cut(entry, "="); key == "PGSSLMODE" {
			sslMode = value
		}
	}

	if sslMode != "require" {
		t.Fatalf("PGSSLMODE = %q, want require", sslMode)
	}
}

func TestPsqlEnvFromURI_RejectsAURIWithoutAHost(t *testing.T) {
	if _, err := psqlEnvFromURI("not-a-uri"); err == nil {
		t.Fatal("expected a URI without a host to be refused")
	}
}

func TestParsePgDumpMajor(t *testing.T) {
	cases := map[string]int{
		"pg_dump (PostgreSQL) 16.2": 16,
		"pg_dump (PostgreSQL) 17":   17,
		// Packaged builds append their own provenance after the real version.
		"pg_dump (PostgreSQL) 15.6 (Debian 15.6-1.pgdg120+2)":   15,
		"pg_dump (PostgreSQL) 16.2 (Ubuntu 16.2-1.pgdg22.04+1)": 16,
	}

	for version, want := range cases {
		got, err := parsePgDumpMajor(version)
		if err != nil {
			t.Errorf("%q: unexpected error: %v", version, err)

			continue
		}

		if got != want {
			t.Errorf("%q: major = %d, want %d", version, got, want)
		}
	}

	if _, err := parsePgDumpMajor("no version here"); err == nil {
		t.Error("expected unparseable output to be an error")
	}
}

// An older pg_dump fails partway through with an error about a missing catalog
// column. Catching it up front turns that into something actionable.
func TestCheckDumpVersion_RefusesAnOlderPgDump(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "PG_VERSION"), []byte("16\n"), 0o600); err != nil {
		t.Fatalf("could not write PG_VERSION: %v", err)
	}

	major, err := readClusterMajorVersion(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if major != 16 {
		t.Fatalf("cluster major = %d, want 16", major)
	}
}

func TestReadClusterMajorVersion_ReportsAMissingFile(t *testing.T) {
	if _, err := readClusterMajorVersion(t.TempDir()); err == nil {
		t.Fatal("expected a missing PG_VERSION to be an error")
	}
}

// The Neon load is a transfer, not a recovery. Timing it against the RTO would
// fail the drill for a reason unrelated to backup health.
func TestApplyNeonDefaults_ForcesTheClusterToStart(t *testing.T) {
	opts := &NeonDrillOptions{}
	applyNeonDefaults(opts)

	if !opts.StartPostgres {
		t.Fatal("the dump reads from the restored cluster, so it must be started")
	}

	if opts.Role != DefaultNeonRole || opts.Database != DefaultNeonDatabase {
		t.Fatalf("role/database defaults not applied: %+v", opts)
	}

	if opts.PgDumpPath != "pg_dump" || opts.PsqlPath != "psql" {
		t.Fatalf("client binary defaults not applied: %+v", opts)
	}
}
