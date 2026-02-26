package tests

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/relaydev/relay/internal/state"
)

func TestNewState(t *testing.T) {
	s := state.NewState("test-thread-1")
	if s.ThreadID != "test-thread-1" {
		t.Errorf("expected thread_id test-thread-1, got %s", s.ThreadID)
	}
	if s.Version != 1 {
		t.Errorf("expected version 1, got %d", s.Version)
	}
	if s.Schema != state.SchemaVersion {
		t.Errorf("expected schema %s, got %s", state.SchemaVersion, s.Schema)
	}
}

func TestStateHeader(t *testing.T) {
	s := state.NewState("test-thread-2")
	// Add more facts than the max
	for i := 0; i < 15; i++ {
		s.Facts = append(s.Facts, state.Fact{
			ID:    fmt.Sprintf("f%d", i),
			Key:   fmt.Sprintf("key%d", i),
			Value: i,
		})
	}
	for i := 0; i < 8; i++ {
		s.Constraints = append(s.Constraints, state.Constraint{
			ID:          fmt.Sprintf("c%d", i),
			Description: fmt.Sprintf("constraint %d", i),
		})
	}

	h := s.Header()

	if len(h.TopFacts) > state.MaxHeaderFacts {
		t.Errorf("header has %d facts, max is %d", len(h.TopFacts), state.MaxHeaderFacts)
	}
	if len(h.TopConstraints) > state.MaxHeaderConstraints {
		t.Errorf("header has %d constraints, max is %d", len(h.TopConstraints), state.MaxHeaderConstraints)
	}
	if h.ThreadID != "test-thread-2" {
		t.Errorf("expected thread_id test-thread-2, got %s", h.ThreadID)
	}
}

func TestValidatePatch(t *testing.T) {
	tests := []struct {
		name    string
		ops     []state.PatchOp
		wantErr bool
	}{
		{
			name: "valid add op",
			ops: []state.PatchOp{
				{Op: "add", Path: "/facts/-", Value: json.RawMessage(`{"id":"f1","key":"k","value":"v"}`)},
			},
			wantErr: false,
		},
		{
			name: "valid replace op",
			ops: []state.PatchOp{
				{Op: "replace", Path: "/metrics", Value: json.RawMessage(`{}`)},
			},
			wantErr: false,
		},
		{
			name: "invalid op",
			ops: []state.PatchOp{
				{Op: "invalid", Path: "/facts"},
			},
			wantErr: true,
		},
		{
			name: "missing path",
			ops: []state.PatchOp{
				{Op: "add", Path: ""},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := state.ValidatePatch(tt.ops)
			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestApplyPatch(t *testing.T) {
	s := state.NewState("test-thread-3")

	// Add a fact
	ops := []state.PatchOp{
		{
			Op:    "add",
			Path:  "/facts/-",
			Value: json.RawMessage(`{"id":"f1","key":"status","value":"active"}`),
		},
	}

	next, err := state.ApplyPatch(s, ops)
	if err != nil {
		t.Fatalf("apply patch: %v", err)
	}

	if len(next.Facts) != 1 {
		t.Errorf("expected 1 fact, got %d", len(next.Facts))
	}
	if next.Facts[0].Key != "status" {
		t.Errorf("expected key 'status', got %s", next.Facts[0].Key)
	}
	if next.Version != s.Version+1 {
		t.Errorf("expected version %d, got %d", s.Version+1, next.Version)
	}
}

func TestApplyPatchReplace(t *testing.T) {
	s := state.NewState("test-thread-4")
	s.Facts = []state.Fact{{ID: "f1", Key: "old", Value: "x"}}

	ops := []state.PatchOp{
		{
			Op:    "replace",
			Path:  "/facts",
			Value: json.RawMessage(`[{"id":"f1","key":"new","value":"y"}]`),
		},
	}

	next, err := state.ApplyPatch(s, ops)
	if err != nil {
		t.Fatalf("apply patch: %v", err)
	}

	if len(next.Facts) != 1 || next.Facts[0].Key != "new" {
		t.Errorf("expected replaced fact with key 'new', got %+v", next.Facts)
	}
}
