package rpb

import "fmt"

const (
	MaxHeaderBytes  = 2048
	MaxPreviewBytes = 2048
	MaxPreviews     = 10
)

type Preview struct {
	ArtifactID string `json:"artifact_id"`
	Excerpt    string `json:"excerpt"`
}

// Bundle is the Relay Prompt Bundle (RPB) — the only internal prompt contract.
type Bundle struct {
	ModelID      string    `json:"model_id"`
	System       string    `json:"system"`
	User         string    `json:"user"`
	StateHeader  string    `json:"state_header"`
	ArtifactRefs []string  `json:"artifact_refs"`
	Previews     []Preview `json:"previews"`
}

func (b *Bundle) Validate() error {
	if len(b.StateHeader) > MaxHeaderBytes {
		return fmt.Errorf("state_header exceeds %d bytes (got %d). suggested action: reduce state size or increase cap", MaxHeaderBytes, len(b.StateHeader))
	}
	if len(b.Previews) > MaxPreviews {
		return fmt.Errorf("previews count exceeds %d (got %d). suggested action: reduce artifacts or increase cap", MaxPreviews, len(b.Previews))
	}
	for _, p := range b.Previews {
		if len(p.Excerpt) > MaxPreviewBytes {
			return fmt.Errorf("preview for artifact %s exceeds %d bytes (got %d). suggested action: store a smaller preview or increase cap", p.ArtifactID, MaxPreviewBytes, len(p.Excerpt))
		}
	}
	return nil
}
