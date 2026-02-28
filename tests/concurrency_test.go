package tests

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/relaydev/relay/internal/artifacts"
	"github.com/relaydev/relay/internal/local"
	"github.com/relaydev/relay/internal/state"
)

func TestConcurrentLocalEngineAccess(t *testing.T) {
	baseDir := t.TempDir()

	const (
		workers    = 8
		iterations = 10
	)

	var wg sync.WaitGroup
	errCh := make(chan error, workers*iterations)

	for w := 0; w < workers; w++ {
		w := w
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				eng, err := local.Open(baseDir)
				if err != nil {
					errCh <- fmt.Errorf("open engine: %w", err)
					return
				}

				name := fmt.Sprintf("w%d-%d", w, i)
				thread, _, err := eng.CreateThread(name)
				if err != nil {
					_ = eng.Close()
					errCh <- fmt.Errorf("create thread: %w", err)
					return
				}

				content := []byte(fmt.Sprintf("payload %d/%d", w, i))
				prov := artifacts.Provenance{CreatedBy: "test", CreatedAt: time.Now().UTC()}
				art, err := eng.ArtifactPut(thread.ID, "note.txt", artifacts.TypeText, "text/plain", bytes.NewReader(content), prov)
				if err != nil {
					_ = eng.Close()
					errCh <- fmt.Errorf("artifact put: %w", err)
					return
				}

				ops := []state.PatchOp{
					{Op: "add", Path: "/artifacts/-", Value: mustJSON(map[string]any{"ref": art.Ref, "type": "text"})},
					{Op: "add", Path: "/last_actions/-", Value: mustJSON(map[string]any{"at": time.Now().UTC().Format(time.RFC3339), "description": "test action", "result_ref": art.Ref})},
				}
				if _, err := eng.PatchState(thread.ID, ops); err != nil {
					_ = eng.Close()
					errCh <- fmt.Errorf("patch state: %w", err)
					return
				}

				if err := eng.Close(); err != nil {
					errCh <- fmt.Errorf("close engine: %w", err)
					return
				}
			}
		}()
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}

	// Validate basic integrity on a sample of threads.
	eng, err := local.Open(baseDir)
	if err != nil {
		t.Fatalf("open engine for validation: %v", err)
	}
	defer eng.Close()

	threads, err := eng.ListThreads(0)
	if err != nil {
		t.Fatalf("list threads: %v", err)
	}
	if len(threads) != workers*iterations {
		t.Fatalf("expected %d threads, got %d", workers*iterations, len(threads))
	}

	sample := 5
	if len(threads) < sample {
		sample = len(threads)
	}
	for i := 0; i < sample; i++ {
		st, err := eng.State(threads[i].ID)
		if err != nil {
			t.Fatalf("get state: %v", err)
		}
		if len(st.Artifacts) == 0 || len(st.LastActions) == 0 {
			t.Fatalf("state missing artifacts/actions for thread %s", threads[i].ID)
		}
		refs := map[string]struct{}{}
		for _, a := range st.Artifacts {
			refs[a.Ref] = struct{}{}
		}
		for _, a := range st.LastActions {
			if a.ResultRef == "" {
				continue
			}
			if _, ok := refs[a.ResultRef]; !ok {
				t.Fatalf("referential closure broken for thread %s: %s", threads[i].ID, a.ResultRef)
			}
		}
	}
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
