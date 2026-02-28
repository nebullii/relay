package state

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	MaxActionsKeep   = 50
	MaxArtifactsKeep = 200
)

// Compact reduces unbounded growth in state while keeping it useful.
// - Keeps last MaxActionsKeep actions, collapsing repeated actions into counts.
// - Keeps last MaxArtifactsKeep artifact refs plus any referenced by remaining actions.
// - Maintains a lightweight session summary when compaction happens.
// Ensures referential closure: actions never reference pruned artifacts.
func Compact(st *State) error {
	if st == nil {
		return nil
	}

	originalActions := len(st.LastActions)
	originalArtifacts := len(st.Artifacts)

	if len(st.LastActions) > 0 {
		st.LastActions = collapseActions(st.LastActions)
		if len(st.LastActions) > MaxActionsKeep {
			st.LastActions = st.LastActions[len(st.LastActions)-MaxActionsKeep:]
		}
	}

	refSet := referencedArtifacts(st.LastActions)
	if len(st.Artifacts) > MaxArtifactsKeep || len(refSet) > 0 {
		st.Artifacts = compactArtifacts(st.Artifacts, refSet)
	}

	if len(st.LastActions) != originalActions || len(st.Artifacts) != originalArtifacts {
		st.SessionSummary = fmt.Sprintf("Compacted: actions %d→%d, artifacts %d→%d",
			originalActions, len(st.LastActions), originalArtifacts, len(st.Artifacts))
	}

	if err := ensureReferentialClosure(st.LastActions, st.Artifacts); err != nil {
		return err
	}
	return nil
}

func collapseActions(actions []Action) []Action {
	if len(actions) == 0 {
		return actions
	}
	out := make([]Action, 0, len(actions))
	cur := actions[0]
	count := 1

	flush := func(a Action, n int) {
		if n <= 1 {
			out = append(out, a)
			return
		}
		a.Description = fmt.Sprintf("%s (x%d)", strings.TrimSpace(a.Description), n)
		out = append(out, a)
	}

	for i := 1; i < len(actions); i++ {
		a := actions[i]
		if a.Description == cur.Description && a.ResultRef == cur.ResultRef {
			count++
			continue
		}
		flush(cur, count)
		cur = a
		count = 1
	}
	flush(cur, count)
	return out
}

// sortActionsDeterministic sorts actions by timestamp then result_ref then description.
func sortActionsDeterministic(actions []Action) {
	sort.Slice(actions, func(i, j int) bool {
		ti := parseTime(actions[i].At)
		tj := parseTime(actions[j].At)
		if !ti.Equal(tj) {
			return ti.Before(tj)
		}
		if actions[i].ResultRef != actions[j].ResultRef {
			return actions[i].ResultRef < actions[j].ResultRef
		}
		return actions[i].Description < actions[j].Description
	})
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
}

func referencedArtifacts(actions []Action) map[string]struct{} {
	out := make(map[string]struct{})
	for _, a := range actions {
		if a.ResultRef != "" {
			out[a.ResultRef] = struct{}{}
		}
	}
	return out
}

func compactArtifacts(artifacts []ArtifactRef, keepRefs map[string]struct{}) []ArtifactRef {
	if len(artifacts) == 0 {
		return artifacts
	}

	// Keep newest N artifacts (tail)
	var base []ArtifactRef
	if len(artifacts) > MaxArtifactsKeep {
		base = artifacts[len(artifacts)-MaxArtifactsKeep:]
	} else {
		base = artifacts
	}

	seen := make(map[string]struct{}, len(base))
	for _, a := range base {
		seen[a.Ref] = struct{}{}
	}

	// Add referenced artifacts missing from base
	var missing []ArtifactRef
	if len(keepRefs) > 0 {
		for _, a := range artifacts {
			if _, ok := keepRefs[a.Ref]; !ok {
				continue
			}
			if _, ok := seen[a.Ref]; ok {
				continue
			}
			missing = append(missing, a)
			seen[a.Ref] = struct{}{}
		}
	}

	// Deterministic order for missing refs
	sort.Slice(missing, func(i, j int) bool {
		if missing[i].Ref != missing[j].Ref {
			return missing[i].Ref < missing[j].Ref
		}
		return missing[i].Name < missing[j].Name
	})

	out := append([]ArtifactRef{}, base...)
	out = append(out, missing...)
	return out
}

func ensureReferentialClosure(actions []Action, artifacts []ArtifactRef) error {
	if len(actions) == 0 {
		return nil
	}
	artSet := make(map[string]struct{}, len(artifacts))
	for _, a := range artifacts {
		artSet[a.Ref] = struct{}{}
	}
	for _, a := range actions {
		if a.ResultRef == "" {
			continue
		}
		if _, ok := artSet[a.ResultRef]; !ok {
			return fmt.Errorf("referential closure violated: action references missing artifact %s", a.ResultRef)
		}
	}
	return nil
}
