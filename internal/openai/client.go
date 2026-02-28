package openai

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type Client struct {
	apiKey  string
	baseURL string
	http    *http.Client
}

func New(apiKey string) *Client {
	return &Client{
		apiKey:  apiKey,
		baseURL: "https://api.openai.com/v1",
		http:    &http.Client{Timeout: 0},
	}
}

type StreamHandler interface {
	OnDelta(text string)
	OnDone()
}

type sseEvent struct {
	Delta string
	Done  bool
}

func (c *Client) StreamChat(model string, messages []Message, h StreamHandler) (string, error) {
	if c.apiKey == "" {
		return "", fmt.Errorf("OPENAI_API_KEY not set")
	}

	const maxAttempts = 5
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		full, err := c.streamOnce(model, messages, h)
		if err == nil {
			return full, nil
		}
		lastErr = err
		retryAfter, ok := retryAfter(err)
		if !ok || attempt == maxAttempts {
			break
		}
		if retryAfter <= 0 {
			retryAfter = time.Duration(attempt) * 2 * time.Second
		}
		time.Sleep(retryAfter)
	}
	return "", lastErr
}

func (c *Client) streamOnce(model string, messages []Message, h StreamHandler) (string, error) {
	body := map[string]any{
		"model":    model,
		"stream":   true,
		"messages": messages,
	}
	data, _ := json.Marshal(body)

	req, err := http.NewRequest("POST", c.baseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		body := strings.TrimSpace(string(b))
		if resp.StatusCode == 429 || resp.StatusCode == 503 {
			return "", retryableErr{
				err:        fmt.Errorf("openai error (status %d): %s", resp.StatusCode, body),
				retryAfter: parseRetryAfter(resp, body),
			}
		}
		return "", fmt.Errorf("openai error (status %d): %s", resp.StatusCode, body)
	}

	var full strings.Builder
	scanner := bufio.NewScanner(resp.Body)
	// Increase buffer for long lines
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if payload == "[DONE]" {
			h.OnDone()
			return full.String(), nil
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta.Content
		if delta == "" {
			continue
		}
		full.WriteString(delta)
		h.OnDelta(delta)
	}
	if err := scanner.Err(); err != nil {
		return full.String(), err
	}
	// If stream ended without [DONE], treat as done.
	h.OnDone()
	return full.String(), nil
}

type retryableErr struct {
	err        error
	retryAfter time.Duration
}

func (e retryableErr) Error() string { return e.err.Error() }

func retryAfter(err error) (time.Duration, bool) {
	if err == nil {
		return 0, false
	}
	if re, ok := err.(retryableErr); ok {
		return re.retryAfter, true
	}
	return 0, false
}

func parseRetryAfter(resp *http.Response, body string) time.Duration {
	if resp != nil {
		if ra := resp.Header.Get("Retry-After"); ra != "" {
			if secs, err := time.ParseDuration(ra + "s"); err == nil {
				return secs
			}
		}
	}
	// Try to parse "Please try again in Xms"
	if idx := strings.Index(body, "try again in "); idx >= 0 {
		rest := body[idx+13:]
		for i := 0; i < len(rest); i++ {
			if rest[i] < '0' || rest[i] > '9' {
				if i == 0 {
					break
				}
				msStr := rest[:i]
				if ms, err := time.ParseDuration(msStr + "ms"); err == nil {
					return ms
				}
				break
			}
		}
	}
	return 0
}

// Optional helper if we ever need non-streaming in v1.
func (c *Client) Chat(model string, messages []Message) (string, error) {
	if c.apiKey == "" {
		return "", fmt.Errorf("OPENAI_API_KEY not set")
	}
	client := &http.Client{Timeout: 120 * time.Second}
	body := map[string]any{
		"model":    model,
		"stream":   false,
		"messages": messages,
	}
	data, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", c.baseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("openai error: %s", strings.TrimSpace(string(b)))
	}
	var out struct {
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("openai: no choices")
	}
	return out.Choices[0].Message.Content, nil
}
