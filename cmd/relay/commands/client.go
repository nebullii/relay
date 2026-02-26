package commands

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is a thin HTTP client for the relay daemon.
type Client struct {
	baseURL  string
	token    string
	http     *http.Client
}

func NewClient(cfg *Config) *Client {
	return &Client{
		baseURL: DaemonURL(cfg),
		token:   cfg.APIToken,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) do(method, path string, body any) ([]byte, int, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("marshal request: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, c.baseURL+path, reqBody)
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	return data, resp.StatusCode, nil
}

func (c *Client) Get(path string, out any) error {
	data, status, err := c.do("GET", path, nil)
	if err != nil {
		return err
	}
	if status >= 400 {
		return parseAPIError(data)
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

func (c *Client) Post(path string, body, out any) error {
	data, status, err := c.do("POST", path, body)
	if err != nil {
		return err
	}
	if status >= 400 {
		return parseAPIError(data)
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

func (c *Client) Health() error {
	var out map[string]string
	return c.Get("/health", &out)
}

func parseAPIError(data []byte) error {
	var errResp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(data, &errResp); err == nil && errResp.Error != "" {
		return fmt.Errorf("%s", errResp.Error)
	}
	return fmt.Errorf("server error: %s", string(data))
}
