package state

import (
	"encoding/json"
	"fmt"
	"time"
)

const SchemaVersion = "com.relay.state.v1"

// State is the canonical memory for a thread.
type State struct {
	Schema        string         `json:"$schema"`
	Version       int            `json:"version"`
	ThreadID      string         `json:"thread_id"`
	UpdatedAt     time.Time      `json:"updated_at"`
	Facts         []Fact         `json:"facts"`
	Constraints   []Constraint   `json:"constraints"`
	OpenQuestions []Question     `json:"open_questions"`
	Decisions     []Decision     `json:"decisions"`
	Plan          []PlanStep     `json:"plan"`
	Artifacts     []ArtifactRef  `json:"artifacts"`
	LastActions   []Action       `json:"last_actions"`
	Metrics       Metrics        `json:"metrics"`
}

type Fact struct {
	ID    string `json:"id"`
	Key   string `json:"key"`
	Value any    `json:"value"`
	At    string `json:"at,omitempty"`
}

type Constraint struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Severity    string `json:"severity"` // hard | soft
}

type Question struct {
	ID       string `json:"id"`
	Question string `json:"question"`
	Status   string `json:"status"` // open | resolved
}

type Decision struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`
	ReasonCodes []string `json:"reason_codes"`
	EvidenceRef []string `json:"evidence_refs"`
	Confidence  float64  `json:"confidence"`
	At          string   `json:"at,omitempty"`
}

type PlanStep struct {
	ID     string `json:"id"`
	Step   string `json:"step"`
	Status string `json:"status"` // pending | done | skipped
}

type ArtifactRef struct {
	Ref  string `json:"ref"`
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

type Action struct {
	At          string `json:"at"`
	Description string `json:"description"`
	ResultRef   string `json:"result_ref,omitempty"`
}

type Metrics struct {
	CacheHits      int `json:"cache_hits"`
	CacheMisses    int `json:"cache_misses"`
	TokensEstimate int `json:"tokens_estimate"`
	TokensAvoided  int `json:"tokens_avoided"`
	HopCount       int `json:"hop_count"`
}

// Header is a bounded, token-efficient view of state.
type Header struct {
	Schema         string        `json:"$schema"`
	ThreadID       string        `json:"thread_id"`
	Version        int           `json:"version"`
	TopFacts       []Fact        `json:"top_facts"`
	TopConstraints []Constraint  `json:"top_constraints"`
	OpenQuestions  []Question    `json:"open_questions"`
	NextSteps      []PlanStep    `json:"next_steps"`
	ArtifactRefs   []ArtifactRef `json:"artifact_refs"`
	LastActions    []Action      `json:"last_actions"`
	Metrics        Metrics       `json:"metrics"`
	Truncated      bool          `json:"truncated,omitempty"` // true when facts were dropped to meet MaxHeaderBytes
}

const (
	MaxHeaderFacts       = 10
	MaxHeaderConstraints = 5
	MaxHeaderQuestions   = 5
	MaxHeaderPlanSteps   = 5
	MaxHeaderArtifacts   = 10
	MaxHeaderActions     = 5
)

func NewState(threadID string) *State {
	return &State{
		Schema:        SchemaVersion,
		Version:       1,
		ThreadID:      threadID,
		UpdatedAt:     time.Now().UTC(),
		Facts:         []Fact{},
		Constraints:   []Constraint{},
		OpenQuestions: []Question{},
		Decisions:     []Decision{},
		Plan:          []PlanStep{},
		Artifacts:     []ArtifactRef{},
		LastActions:   []Action{},
		Metrics:       Metrics{},
	}
}

// MaxHeaderBytes is the hard JSON size cap for a rendered state header.
const MaxHeaderBytes = 2048

// Header returns a bounded view of state for use in agent prompts.
// Field counts are capped first; then if the JSON still exceeds MaxHeaderBytes,
// oldest facts are dropped one by one until it fits.
func (s *State) Header() *Header {
	h := &Header{
		Schema:   SchemaVersion,
		ThreadID: s.ThreadID,
		Version:  s.Version,
		Metrics:  s.Metrics,
	}

	// Bounded facts — keep newest, drop oldest when over limit.
	facts := s.Facts
	if len(facts) > MaxHeaderFacts {
		facts = facts[len(facts)-MaxHeaderFacts:]
	}
	h.TopFacts = facts

	// Bounded constraints — keep oldest (highest priority rules).
	constraints := s.Constraints
	if len(constraints) > MaxHeaderConstraints {
		constraints = constraints[:MaxHeaderConstraints]
	}
	h.TopConstraints = constraints

	// Open questions only.
	for _, q := range s.OpenQuestions {
		if q.Status == "open" || q.Status == "" {
			h.OpenQuestions = append(h.OpenQuestions, q)
			if len(h.OpenQuestions) >= MaxHeaderQuestions {
				break
			}
		}
	}

	// Pending plan steps.
	for _, p := range s.Plan {
		if p.Status == "pending" || p.Status == "" {
			h.NextSteps = append(h.NextSteps, p)
			if len(h.NextSteps) >= MaxHeaderPlanSteps {
				break
			}
		}
	}

	// Recent artifacts.
	artifacts := s.Artifacts
	if len(artifacts) > MaxHeaderArtifacts {
		artifacts = artifacts[len(artifacts)-MaxHeaderArtifacts:]
	}
	h.ArtifactRefs = artifacts

	// Recent actions.
	actions := s.LastActions
	if len(actions) > MaxHeaderActions {
		actions = actions[len(actions)-MaxHeaderActions:]
	}
	h.LastActions = actions

	// Hard JSON size cap: drop oldest facts one by one until header fits.
	for len(h.TopFacts) > 0 {
		data, err := json.Marshal(h)
		if err != nil || len(data) <= MaxHeaderBytes {
			break
		}
		h.TopFacts = h.TopFacts[1:] // drop oldest
		h.Truncated = true
	}

	return h
}

// PatchOp represents a single JSON Patch operation (RFC 6902).
type PatchOp struct {
	Op    string          `json:"op"`
	Path  string          `json:"path"`
	Value json.RawMessage `json:"value,omitempty"`
	From  string          `json:"from,omitempty"`
}

// ValidatePatch validates patch operations before applying.
func ValidatePatch(ops []PatchOp) error {
	validOps := map[string]bool{"add": true, "remove": true, "replace": true, "move": true, "copy": true, "test": true}
	for i, op := range ops {
		if !validOps[op.Op] {
			return fmt.Errorf("patch[%d]: unknown op %q", i, op.Op)
		}
		if op.Path == "" {
			return fmt.Errorf("patch[%d]: path is required", i)
		}
	}
	return nil
}

// ApplyPatch applies JSON patch operations to a state.
// This is a simplified implementation supporting add/replace/remove on top-level list fields.
func ApplyPatch(s *State, ops []PatchOp) (*State, error) {
	// Round-trip through JSON for a clean apply
	data, err := json.Marshal(s)
	if err != nil {
		return nil, fmt.Errorf("marshal state: %w", err)
	}

	var doc map[string]json.RawMessage
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("unmarshal state: %w", err)
	}

	for i, op := range ops {
		if err := applyOp(doc, op); err != nil {
			return nil, fmt.Errorf("patch[%d] %s %s: %w", i, op.Op, op.Path, err)
		}
	}

	// Re-serialize
	result, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("re-marshal: %w", err)
	}

	var next State
	if err := json.Unmarshal(result, &next); err != nil {
		return nil, fmt.Errorf("unmarshal result: %w", err)
	}

	next.Version = s.Version + 1
	next.UpdatedAt = time.Now().UTC()
	return &next, nil
}

func applyOp(doc map[string]json.RawMessage, op PatchOp) error {
	// Parse path: /field or /field/index
	path := op.Path
	if len(path) == 0 || path[0] != '/' {
		return fmt.Errorf("path must start with /")
	}
	parts := splitPath(path[1:])
	if len(parts) == 0 {
		return fmt.Errorf("empty path")
	}

	field := parts[0]

	switch op.Op {
	case "add", "replace":
		if len(parts) == 1 {
			doc[field] = op.Value
		} else if len(parts) == 2 && parts[1] == "-" {
			// Append to array
			var arr []json.RawMessage
			if existing, ok := doc[field]; ok {
				_ = json.Unmarshal(existing, &arr)
			}
			arr = append(arr, op.Value)
			data, _ := json.Marshal(arr)
			doc[field] = data
		} else {
			return fmt.Errorf("deep patch not supported in v1: %s", op.Path)
		}
	case "remove":
		if len(parts) == 1 {
			delete(doc, field)
		} else {
			return fmt.Errorf("deep remove not supported in v1: %s", op.Path)
		}
	case "test":
		// No-op in v1
	default:
		return fmt.Errorf("op %q not fully implemented", op.Op)
	}
	return nil
}

func splitPath(path string) []string {
	if path == "" {
		return nil
	}
	var parts []string
	cur := ""
	for _, c := range path {
		if c == '/' {
			parts = append(parts, cur)
			cur = ""
		} else {
			cur += string(c)
		}
	}
	parts = append(parts, cur)
	return parts
}
