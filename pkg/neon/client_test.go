// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

package neon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testAPIKey = "neon_api_key_do_not_leak_2f8c1e"

// newTestClient points a client at a test server, with the waiting knobs turned
// down so operation polling does not make the suite slow.
func newTestClient(t *testing.T, handler http.Handler) (*Client, *httptest.Server) {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := NewClient(Config{
		APIKey:           testAPIKey,
		ProjectID:        "proj-123",
		Endpoint:         server.URL,
		OperationTimeout: 2 * time.Second,
		PollInterval:     time.Millisecond,
		HTTPClient:       server.Client(),
	})
	if err != nil {
		t.Fatalf("could not build the client: %v", err)
	}

	return client, server
}

func writeJSON(t *testing.T, w http.ResponseWriter, status int, body any) {
	t.Helper()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(body); err != nil {
		t.Errorf("could not encode the test response: %v", err)
	}
}

func TestNewClient_RequiresCredentials(t *testing.T) {
	if _, err := NewClient(Config{ProjectID: "proj-123"}); err == nil {
		t.Fatal("expected a missing API key to be refused")
	}

	if _, err := NewClient(Config{APIKey: testAPIKey}); err == nil {
		t.Fatal("expected a missing project ID to be refused")
	}
}

func TestNewClient_DefaultsTheEndpoint(t *testing.T) {
	client, err := NewClient(Config{APIKey: testAPIKey, ProjectID: "proj-123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if client.endpoint != DefaultEndpoint {
		t.Fatalf("endpoint = %q, want %q", client.endpoint, DefaultEndpoint)
	}
}

// Creating a branch returns before the endpoint is provisioned. The client must
// poll the operations out, or it hands back a branch that refuses connections.
func TestCreateBranch_WaitsForOperationsToFinish(t *testing.T) {
	var operationPolls int

	mux := http.NewServeMux()

	mux.HandleFunc("/projects/proj-123/branches", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}

		var request createBranchRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("could not decode the request: %v", err)
		}

		if request.Branch.Name != DrillBranchPrefix+"1" {
			t.Errorf("branch name = %q", request.Branch.Name)
		}

		if len(request.Endpoints) != 1 || request.Endpoints[0].Type != EndpointTypeReadWrite {
			t.Errorf("expected a single read_write endpoint, got %+v", request.Endpoints)
		}

		writeJSON(t, w, http.StatusCreated, branchResponse{
			Branch:     Branch{ID: "br-1", Name: DrillBranchPrefix + "1"},
			Operations: []Operation{{ID: "op-1", Action: "start_compute", Status: OperationRunning}},
		})
	})

	mux.HandleFunc("/projects/proj-123/operations/op-1", func(w http.ResponseWriter, _ *http.Request) {
		operationPolls++

		status := OperationRunning
		if operationPolls >= 2 {
			status = OperationFinished
		}

		writeJSON(t, w, http.StatusOK, operationResponse{
			Operation: Operation{ID: "op-1", Action: "start_compute", Status: status},
		})
	})

	client, _ := newTestClient(t, mux)

	branch, err := client.CreateBranch(context.Background(), DrillBranchPrefix+"1", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if branch.ID != "br-1" {
		t.Fatalf("branch ID = %q, want br-1", branch.ID)
	}

	if operationPolls < 2 {
		t.Fatalf("polled operations %d time(s); expected it to wait for the running one", operationPolls)
	}
}

func TestCreateBranch_ReportsAFailedOperation(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/projects/proj-123/branches", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusCreated, branchResponse{
			Branch: Branch{ID: "br-1", Name: DrillBranchPrefix + "1"},
			Operations: []Operation{{
				ID: "op-1", Action: "start_compute", Status: OperationFailed,
				Error: "compute quota exceeded",
			}},
		})
	})

	client, _ := newTestClient(t, mux)

	_, err := client.CreateBranch(context.Background(), DrillBranchPrefix+"1", "")
	if err == nil {
		t.Fatal("expected a failed operation to be an error")
	}

	if !strings.Contains(err.Error(), "compute quota exceeded") {
		t.Fatalf("error should carry the control plane's reason, got: %v", err)
	}
}

func TestListDrillBranches_IgnoresBranchesWalgDidNotCreate(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/projects/proj-123/branches", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusOK, listBranchesResponse{Branches: []Branch{
			{ID: "br-main", Name: "main", Default: true},
			{ID: "br-dev", Name: "someone-elses-work"},
			{ID: "br-drill", Name: DrillBranchPrefix + "20260814T100000Z"},
			// A branch merely mentioning the prefix later in its name is not ours.
			{ID: "br-decoy", Name: "not-a-" + DrillBranchPrefix + "branch"},
		}})
	})

	client, _ := newTestClient(t, mux)

	branches, err := client.ListDrillBranches(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(branches) != 1 {
		t.Fatalf("got %d drill branches, want 1: %+v", len(branches), branches)
	}

	if branches[0].ID != "br-drill" {
		t.Fatalf("selected the wrong branch: %+v", branches[0])
	}
}

// Cleanup runs from defers and signal handlers. A bug there must not be able to
// delete something a person is using.
func TestDeleteBranch_RefusesBranchesWalgDidNotCreate(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(_ http.ResponseWriter, r *http.Request) {
		t.Errorf("no request should have been sent, got %s %s", r.Method, r.URL.Path)
	})

	client, _ := newTestClient(t, mux)

	cases := []Branch{
		{ID: "br-main", Name: "main", Default: true},
		{ID: "br-dev", Name: "someone-elses-work"},
		{ID: "br-protected", Name: DrillBranchPrefix + "x", Protected: true},
		{ID: "br-default", Name: DrillBranchPrefix + "x", Default: true},
	}

	for _, branch := range cases {
		if err := client.DeleteBranch(context.Background(), branch); err == nil {
			t.Fatalf("expected deleting %+v to be refused", branch)
		}
	}
}

// Cleanup runs on paths where the branch may never have been created, so a
// branch that is already gone is the desired end state, not a failure.
func TestDeleteBranch_TreatsAMissingBranchAsDone(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/projects/proj-123/branches/br-1", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusNotFound, map[string]string{"message": "branch not found"})
	})

	client, _ := newTestClient(t, mux)

	err := client.DeleteBranch(context.Background(), Branch{ID: "br-1", Name: DrillBranchPrefix + "1"})
	if err != nil {
		t.Fatalf("a missing branch should not be an error, got: %v", err)
	}
}

func TestConnectionURI_PassesTheBranchAndRole(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/projects/proj-123/connection_uri", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()

		if query.Get("branch_id") != "br-1" {
			t.Errorf("branch_id = %q", query.Get("branch_id"))
		}

		if query.Get("database_name") != "neondb" {
			t.Errorf("database_name = %q", query.Get("database_name"))
		}

		if query.Get("role_name") != "neondb_owner" {
			t.Errorf("role_name = %q", query.Get("role_name"))
		}

		writeJSON(t, w, http.StatusOK, connectionURIResponse{
			URI: "postgresql://neondb_owner:secretpw@ep-1.neon.tech/neondb",
		})
	})

	client, _ := newTestClient(t, mux)

	uri, err := client.ConnectionURI(context.Background(), "br-1", "neondb", "neondb_owner")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.HasPrefix(uri, "postgresql://") {
		t.Fatalf("uri = %q", uri)
	}
}

func TestConnectionURI_RejectsAnEmptyURI(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/projects/proj-123/connection_uri", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusOK, connectionURIResponse{})
	})

	client, _ := newTestClient(t, mux)

	if _, err := client.ConnectionURI(context.Background(), "br-1", "neondb", "owner"); err == nil {
		t.Fatal("expected an empty connection URI to be refused")
	}
}

// The API key is a credential with full control of the project. Errors from a
// drill end up in CI logs and JSON reports, so the key must never reach one.
func TestErrors_NeverCarryTheAPIKey(t *testing.T) {
	mux := http.NewServeMux()

	// Echo the key back in the body, which a hostile or careless control plane
	// could do. It still must not survive into the error.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(t, w, http.StatusUnauthorized, map[string]string{
			"message": "bad credentials for " + r.Header.Get("Authorization"),
		})
	})

	client, _ := newTestClient(t, mux)

	_, err := client.ListBranches(context.Background())
	if err == nil {
		t.Fatal("expected a 401 to be an error")
	}

	if strings.Contains(err.Error(), testAPIKey) {
		t.Fatalf("the API key leaked into an error: %v", err)
	}

	// The operator still needs to be told what to fix.
	if !strings.Contains(err.Error(), "WALG_NEON_API_KEY") {
		t.Fatalf("a 401 should point at the setting to check, got: %v", err)
	}
}

func TestAPIError_SurfacesRateLimiting(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "30")
		writeJSON(t, w, http.StatusTooManyRequests, map[string]string{"message": "rate limited"})
	})

	client, _ := newTestClient(t, mux)

	_, err := client.ListBranches(context.Background())
	if err == nil {
		t.Fatal("expected a 429 to be an error")
	}

	if !strings.Contains(err.Error(), "retry after 30s") {
		t.Fatalf("expected the Retry-After hint in the error, got: %v", err)
	}
}

func TestWaitForOperations_GivesUpOnTheOperationTimeout(t *testing.T) {
	mux := http.NewServeMux()

	mux.HandleFunc("/projects/proj-123/operations/op-1", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(t, w, http.StatusOK, operationResponse{
			Operation: Operation{ID: "op-1", Action: "start_compute", Status: OperationRunning},
		})
	})

	client, _ := newTestClient(t, mux)
	client.operationTimeout = 50 * time.Millisecond

	err := client.WaitForOperations(context.Background(),
		[]Operation{{ID: "op-1", Action: "start_compute", Status: OperationRunning}})
	if err == nil {
		t.Fatal("expected an operation that never finishes to time out")
	}

	if !strings.Contains(err.Error(), "op-1") {
		t.Fatalf("the error should name the operation, got: %v", err)
	}
}

func TestIsNotFound(t *testing.T) {
	if !IsNotFound(&APIError{StatusCode: http.StatusNotFound}) {
		t.Fatal("a 404 should be reported as not found")
	}

	if IsNotFound(&APIError{StatusCode: http.StatusInternalServerError}) {
		t.Fatal("a 500 is not a not-found")
	}

	if IsNotFound(nil) {
		t.Fatal("nil is not a not-found")
	}
}
