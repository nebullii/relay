package client

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/relaydev/relay/internal/artifacts"
	"github.com/relaydev/relay/internal/local"
	"github.com/relaydev/relay/internal/openai"
	"github.com/relaydev/relay/internal/rpb"
	"github.com/relaydev/relay/internal/state"
)

type Client struct {
	Model   string
	System  string
	BaseDir string
}

type Response struct {
	Output        string
	ThreadID      string
	ArtifactRef   string
	RPBBytes      int
	HeaderBytes   int
	PreviewBytes  int
	PreviewCount  int
	PromptBytes   int
	NaiveTokens   int
	ActualTokens  int
	AvoidedTokens int
	StateHeader   string
}

type RPBInfo struct {
	Bundle       rpb.Bundle
	ThreadID     string
	RPBBytes     int
	HeaderBytes  int
	PreviewBytes int
	PreviewCount int
	PromptBytes  int
	StateHeader  string
}

func (c *Client) Chat(ctx context.Context, threadID string, user string) (Response, error) {
	info, err := c.BuildRPB(ctx, threadID, user)
	if err != nil {
		return Response{}, err
	}
	return c.ChatWithRPB(ctx, info, user)
}

func (c *Client) ChatWithRPB(ctx context.Context, info RPBInfo, user string) (Response, error) {
	res := Response{
		ThreadID:     info.ThreadID,
		RPBBytes:     info.RPBBytes,
		HeaderBytes:  info.HeaderBytes,
		PreviewBytes: info.PreviewBytes,
		PreviewCount: info.PreviewCount,
		PromptBytes:  info.PromptBytes,
		StateHeader:  info.StateHeader,
	}

	// Render RPB -> OpenAI messages
	bundle := info.Bundle
	var userParts []string
	if bundle.User != "" {
		userParts = append(userParts, bundle.User)
	}
	if len(bundle.ArtifactRefs) > 0 {
		userParts = append(userParts, "\n## Artifacts\n")
		for _, p := range bundle.Previews {
			userParts = append(userParts, fmt.Sprintf("[artifact:%s]\n%s", p.ArtifactID, p.Excerpt))
		}
	}

	sys := bundle.StateHeader
	if bundle.System != "" {
		sys = bundle.System + "\n\n" + bundle.StateHeader
	}

	messages := []openai.Message{{Role: "system", Content: sys}}
	if len(userParts) > 0 {
		messages = append(messages, openai.Message{Role: "user", Content: strings.Join(userParts, "\n\n")})
	}

	res.PromptBytes = len(sys) + len(strings.Join(userParts, "\n\n"))

	// Call OpenAI streaming
	oc := openai.New(os.Getenv("OPENAI_API_KEY"))
	printer := &streamPrinter{}
	full, err := oc.StreamChat(bundle.ModelID, messages, printer)
	if err != nil {
		return Response{}, err
	}
	res.Output = full

	// Persist response in local store
	eng, err := c.openEngine()
	if err != nil {
		return Response{}, err
	}
	defer eng.Close()

	prov := artifacts.Provenance{
		CreatedBy: "relay",
		CreatedAt: time.Now().UTC(),
	}
	art, err := eng.ArtifactPut(res.ThreadID, fmt.Sprintf("assistant-%d", time.Now().Unix()), artifacts.TypeText, "text/plain", strings.NewReader(full), prov)
	if err != nil {
		return Response{}, err
	}
	res.ArtifactRef = art.Ref

	if art.Ref != "" {
		ops := []map[string]any{
			{"op": "add", "path": "/artifacts/-", "value": map[string]any{"ref": art.Ref, "type": "text"}},
			{"op": "add", "path": "/last_actions/-", "value": map[string]any{"at": time.Now().UTC().Format(time.RFC3339), "description": "assistant response", "result_ref": art.Ref}},
		}
		_, _ = eng.PatchState(res.ThreadID, toPatchOps(ops))
	}

	naive, actual, avoided, err := computeSavings(eng, res.ThreadID)
	if err == nil {
		res.NaiveTokens = naive
		res.ActualTokens = actual
		res.AvoidedTokens = avoided
	}

	return res, nil
}

func (c *Client) BuildRPB(ctx context.Context, threadID string, user string) (RPBInfo, error) {
	if strings.TrimSpace(user) == "" {
		return RPBInfo{}, fmt.Errorf("user prompt required")
	}

	eng, err := c.openEngine()
	if err != nil {
		return RPBInfo{}, err
	}
	defer eng.Close()

	tid, err := ensureThread(eng, threadID)
	if err != nil {
		return RPBInfo{}, err
	}

	model := c.Model
	if model == "" {
		model = os.Getenv("RELAY_MODEL")
		if model == "" {
			model = "gpt-4o"
		}
	}

	// Fetch state header
	header, err := eng.StateHeader(tid)
	if err != nil {
		return RPBInfo{}, err
	}
	headJSON, _ := json.Marshal(header)
	stateHeader := string(headJSON)

	// Fetch artifacts (previews only)
	arts, err := eng.ArtifactList(tid)
	if err != nil {
		return RPBInfo{}, err
	}
	sort.Slice(arts, func(i, j int) bool {
		ti := arts[i].CreatedAt
		tj := arts[j].CreatedAt
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return arts[i].Ref < arts[j].Ref
	})

	bundle := rpb.Bundle{
		ModelID:     model,
		System:      c.System,
		User:        user,
		StateHeader: stateHeader,
	}
	for i, art := range arts {
		if i >= rpb.MaxPreviews {
			break
		}
		bundle.ArtifactRefs = append(bundle.ArtifactRefs, art.Ref)
		excerpt := art.Preview.Text
		if len(excerpt) > rpb.MaxPreviewBytes {
			excerpt = excerpt[:rpb.MaxPreviewBytes]
		}
		bundle.Previews = append(bundle.Previews, rpb.Preview{ArtifactID: art.Ref, Excerpt: excerpt})
	}

	if err := bundle.Validate(); err != nil {
		return RPBInfo{}, err
	}

	rpbBytes, _ := json.Marshal(bundle)
	previewBytes := 0
	for _, p := range bundle.Previews {
		previewBytes += len(p.Excerpt)
	}
	promptBytes := len(bundle.StateHeader) + len(user)

	return RPBInfo{
		Bundle:       bundle,
		ThreadID:     tid,
		RPBBytes:     len(rpbBytes),
		HeaderBytes:  len(bundle.StateHeader),
		PreviewBytes: previewBytes,
		PreviewCount: len(bundle.Previews),
		PromptBytes:  promptBytes,
		StateHeader:  bundle.StateHeader,
	}, nil
}

func (c *Client) openEngine() (*local.Engine, error) {
	base := c.BaseDir
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".relay")
	}
	return local.Open(base)
}

func ensureThread(eng *local.Engine, threadID string) (string, error) {
	if threadID != "" {
		if _, err := eng.GetThread(threadID); err == nil {
			return threadID, nil
		}
	}
	t, _, err := eng.CreateThread("default")
	if err != nil {
		return "", err
	}
	return t.ID, nil
}

func computeSavings(eng *local.Engine, threadID string) (naiveTokens, actualTokens, avoided int, err error) {
	arts, err := eng.ArtifactList(threadID)
	if err != nil {
		return 0, 0, 0, err
	}
	for _, a := range arts {
		naiveTokens += int(a.Size) / 4
		actualTokens += len(a.Preview.Text) / 4
	}
	avoided = naiveTokens - actualTokens
	return naiveTokens, actualTokens, avoided, nil
}

func toPatchOps(ops []map[string]any) []state.PatchOp {
	data, _ := json.Marshal(ops)
	var out []state.PatchOp
	_ = json.Unmarshal(data, &out)
	return out
}

type streamPrinter struct{}

func (s *streamPrinter) OnDelta(text string) {
	fmt.Fprint(os.Stdout, text)
}
func (s *streamPrinter) OnDone() {
	fmt.Fprintln(os.Stdout)
}
