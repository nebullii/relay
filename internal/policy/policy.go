package policy

import (
	"fmt"
	"strings"
)

const (
	DefaultMaxPayloadBytes  = 16 * 1024 // 16KB
	DefaultMaxNoteLen       = 280
	DefaultMaxHops          = 50
	DefaultTenant           = "local"
	DefaultAPITokenLength   = 32
)

// Config holds policy configuration.
type Config struct {
	MaxPayloadBytes int
	MaxNoteLen      int
	MaxHops         int
	Tenant          string
	AllowedOrigins  []string
}

func DefaultConfig() *Config {
	return &Config{
		MaxPayloadBytes: DefaultMaxPayloadBytes,
		MaxNoteLen:      DefaultMaxNoteLen,
		MaxHops:         DefaultMaxHops,
		Tenant:          DefaultTenant,
	}
}

// ValidateEnvelope validates an A2A message envelope.
func ValidateEnvelope(env map[string]any, cfg *Config) error {
	if cfg == nil {
		cfg = DefaultConfig()
	}

	// Required fields
	required := []string{"msg_id", "thread_id", "from", "type", "schema", "payload"}
	for _, field := range required {
		if _, ok := env[field]; !ok {
			return fmt.Errorf("missing required field: %s", field)
		}
	}

	// Check payload size
	if payload, ok := env["payload"].(string); ok {
		if len(payload) > cfg.MaxPayloadBytes {
			return fmt.Errorf("payload exceeds max size of %d bytes", cfg.MaxPayloadBytes)
		}
	}

	// Check note length
	if note, ok := env["note"].(string); ok {
		if len(note) > cfg.MaxNoteLen {
			return fmt.Errorf("note exceeds max length of %d chars", cfg.MaxNoteLen)
		}
	}

	// Validate type
	validTypes := map[string]bool{
		"request": true, "response": true, "event": true, "command": true, "error": true,
	}
	if msgType, ok := env["type"].(string); ok {
		if !validTypes[msgType] {
			return fmt.Errorf("invalid message type: %s", msgType)
		}
	}

	return nil
}

// CheckHopLimit checks if a thread has exceeded its hop limit.
func CheckHopLimit(hopCount, maxHops int) error {
	if hopCount >= maxHops {
		return fmt.Errorf("hop limit exceeded: %d/%d", hopCount, maxHops)
	}
	return nil
}

// ValidateAPIToken validates an API token format.
func ValidateAPIToken(token string) error {
	if len(token) < 16 {
		return fmt.Errorf("API token too short (min 16 chars)")
	}
	if strings.ContainsAny(token, " \t\n\r") {
		return fmt.Errorf("API token must not contain whitespace")
	}
	return nil
}

// ACL represents an access control list.
type ACL struct {
	Tenant  string
	Allowed map[string]bool // capability -> allowed
}

func NewACL(tenant string) *ACL {
	return &ACL{
		Tenant:  tenant,
		Allowed: map[string]bool{},
	}
}

func (a *ACL) Allow(capability string) {
	a.Allowed[capability] = true
}

func (a *ACL) Deny(capability string) {
	a.Allowed[capability] = false
}

func (a *ACL) CanInvoke(capability string) bool {
	if allowed, ok := a.Allowed[capability]; ok {
		return allowed
	}
	return true // default allow in v1
}
