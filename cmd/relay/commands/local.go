package commands

import (
	"encoding/json"

	"github.com/relaydev/relay/internal/local"
	"github.com/relaydev/relay/internal/state"
)

func openEngine(cfg *Config) (*local.Engine, error) {
	return local.Open(cfg.BaseDir)
}

func toPatchOps(ops []map[string]any) []state.PatchOp {
	data, _ := json.Marshal(ops)
	var out []state.PatchOp
	_ = json.Unmarshal(data, &out)
	return out
}
