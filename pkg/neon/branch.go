// Copyright (c) 2026 Lateos. Licensed under the MIT License. See LICENSE-MIT.

// Package neon talks to the Neon control plane.
//
// It is deliberately not a wal-g storage backend. Neon stores pages in its own
// pageserver and exposes only a SQL connection, so it can neither receive a
// physical backup nor serve one. This package manages branches - the cheap,
// disposable databases a restore drill loads a recovered cluster into - and
// nothing else.
package neon

import "time"

// DrillBranchPrefix marks the branches wal-g creates.
//
// Everything that lists or deletes branches filters on it. A Neon project
// belongs to its owner, and a drill that deleted a branch somebody was using
// would be far worse than one that leaked.
const DrillBranchPrefix = "walg-drill-"

// Branch is a Neon branch.
type Branch struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id,omitempty"`
	ParentID  string    `json:"parent_id,omitempty"`
	Name      string    `json:"name"`
	Default   bool      `json:"default,omitempty"`
	Protected bool      `json:"protected,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
}

// IsDrillBranch reports whether wal-g created this branch.
func (b Branch) IsDrillBranch() bool {
	return len(b.Name) > len(DrillBranchPrefix) && b.Name[:len(DrillBranchPrefix)] == DrillBranchPrefix
}

// Operation is an asynchronous control-plane action.
//
// Creating a branch returns before the branch is usable: the endpoint has to be
// provisioned first. Connecting to a branch whose operations have not finished
// fails in ways that look like a network fault, so callers wait.
type Operation struct {
	ID     string `json:"id"`
	Action string `json:"action"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// Operation statuses the control plane reports.
//
// The two cancel spellings are Neon's, not ours: these are wire values compared
// against what the API sends, so "correcting" them to the US spelling would
// stop them matching anything.
const (
	OperationRunning    = "running"
	OperationScheduling = "scheduling"
	OperationFinished   = "finished"
	OperationFailed     = "failed"
	OperationError      = "error"
	OperationCancelling = "cancelling" //nolint:misspell // Neon's wire value
	OperationCancelled  = "cancelled"  //nolint:misspell // Neon's wire value
	OperationSkipped    = "skipped"
)

// Done reports whether the operation has stopped changing.
func (o Operation) Done() bool {
	switch o.Status {
	case OperationFinished, OperationFailed, OperationError,
		OperationCancelled, OperationSkipped:
		return true
	default:
		return false
	}
}

// Succeeded reports whether the operation finished and did what it was asked.
func (o Operation) Succeeded() bool {
	return o.Status == OperationFinished || o.Status == OperationSkipped
}

// Endpoint is a compute endpoint attached to a branch. A branch without one
// cannot be connected to.
type Endpoint struct {
	ID       string `json:"id"`
	BranchID string `json:"branch_id,omitempty"`
	Host     string `json:"host,omitempty"`
	Type     string `json:"type,omitempty"`
}

// EndpointTypeReadWrite is the endpoint type a drill needs: the load writes.
const EndpointTypeReadWrite = "read_write"

// createBranchRequest is the POST /branches body.
type createBranchRequest struct {
	Branch    createBranchSpec     `json:"branch"`
	Endpoints []createEndpointSpec `json:"endpoints,omitempty"`
}

type createBranchSpec struct {
	Name     string `json:"name"`
	ParentID string `json:"parent_id,omitempty"`
}

type createEndpointSpec struct {
	Type string `json:"type"`
}

// branchResponse is the shape returned by create and delete alike.
type branchResponse struct {
	Branch     Branch      `json:"branch"`
	Endpoints  []Endpoint  `json:"endpoints,omitempty"`
	Operations []Operation `json:"operations,omitempty"`
}

type listBranchesResponse struct {
	Branches []Branch `json:"branches"`
}

type operationResponse struct {
	Operation Operation `json:"operation"`
}

type connectionURIResponse struct {
	URI string `json:"uri"`
}
