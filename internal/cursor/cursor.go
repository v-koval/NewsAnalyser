package cursor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const DefaultBaseURL = "https://api.cursor.com/v0/agents"

type Client struct {
	APIKey     string
	BaseURL    string
	HTTP       *http.Client
	PollPeriod time.Duration
	MaxWait    time.Duration
}

func New(apiKey string) *Client {
	return &Client{
		APIKey:     apiKey,
		BaseURL:    DefaultBaseURL,
		HTTP:       &http.Client{Timeout: 90 * time.Second},
		PollPeriod: 15 * time.Second,
		MaxWait:    30 * time.Minute,
	}
}

type createReq struct {
	Prompt struct {
		Text string `json:"text"`
	} `json:"prompt"`
	Source *sourceField `json:"source,omitempty"`
	Model  string       `json:"model,omitempty"`
}

type sourceField struct {
	Repository string `json:"repository"`
}

type createResp struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type statusResp struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type convResp struct {
	Messages []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"messages"`
}

func (c *Client) do(ctx context.Context, method, url string, body any) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("cursor api %s %s: %d: %s", method, url, resp.StatusCode, string(b))
	}
	return b, nil
}

// RunPrompt creates an agent with the given prompt, polls for completion,
// fetches the conversation and returns the last assistant text message.
func (c *Client) RunPrompt(ctx context.Context, prompt, repository string) (string, error) {
	if c.APIKey == "" {
		return "", errors.New("cursor api key is empty")
	}
	var cr createReq
	cr.Prompt.Text = prompt
	if repository != "" {
		// Cursor API может ожидать формат "owner/repo" вместо полного URL.
		repo := repository
		repo = strings.TrimSuffix(repo, "/")
		repo = strings.TrimSuffix(repo, ".git")
		if strings.HasPrefix(repo, "https://github.com/") {
			repo = strings.TrimPrefix(repo, "https://github.com/")
		}
		cr.Source = &sourceField{Repository: repo}
	}
	b, err := c.do(ctx, "POST", c.BaseURL, cr)
	if err != nil {
		return "", err
	}
	var created createResp
	if err := json.Unmarshal(b, &created); err != nil {
		return "", fmt.Errorf("parse create: %w: %s", err, string(b))
	}
	if created.ID == "" {
		return "", fmt.Errorf("no agent id in response: %s", string(b))
	}
	deadline := time.Now().Add(c.MaxWait)
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
		sb, err := c.do(ctx, "GET", c.BaseURL+"/"+created.ID, nil)
		if err != nil {
			return "", err
		}
		var st statusResp
		if err := json.Unmarshal(sb, &st); err != nil {
			return "", err
		}
		switch strings.ToUpper(st.Status) {
		case "FINISHED", "COMPLETED", "DONE", "SUCCESS":
			cb, err := c.do(ctx, "GET", c.BaseURL+"/"+created.ID+"/conversation", nil)
			if err != nil {
				return "", err
			}
			var cv convResp
			if err := json.Unmarshal(cb, &cv); err != nil {
				return "", err
			}
			for i := len(cv.Messages) - 1; i >= 0; i-- {
				if cv.Messages[i].Type == "assistant_message" || cv.Messages[i].Type == "assistant" {
					return cv.Messages[i].Text, nil
				}
			}
			if len(cv.Messages) > 0 {
				return cv.Messages[len(cv.Messages)-1].Text, nil
			}
			return "", errors.New("empty conversation")
		case "FAILED", "ERROR", "CANCELLED", "EXPIRED":
			return "", fmt.Errorf("agent finished with status %s", st.Status)
		}
		if time.Now().After(deadline) {
			return "", errors.New("agent timeout")
		}
		time.Sleep(c.PollPeriod)
	}
}

var fenceRe = regexp.MustCompile("(?s)```(?:json)?\\s*(.*?)```")

// ExtractJSON pulls the first JSON object/array out of the agent response text.
func ExtractJSON(s string) string {
	if m := fenceRe.FindStringSubmatch(s); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}
	s = strings.TrimSpace(s)
	start := strings.IndexAny(s, "{[")
	if start < 0 {
		return ""
	}
	openCh := s[start]
	closeCh := byte('}')
	if openCh == '[' {
		closeCh = ']'
	}
	depth := 0
	inStr := false
	escaped := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if inStr {
			if escaped {
				escaped = false
				continue
			}
			switch c {
			case '\\':
				escaped = true
			case '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case openCh:
			depth++
		case closeCh:
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return s[start:]
}

// RepairJSON best-effort escapes stray unescaped double quotes inside JSON
// string values. LLMs often emit quoted titles like «из "Just Like Heaven"»
// without escaping the inner quotes; this walks the text and escapes any `"`
// whose next non-whitespace char is not a structural terminator (`,`, `}`,
// `]`, `:`) or end of input.
func RepairJSON(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 16)
	inStr := false
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !inStr {
			if c == '"' {
				inStr = true
			}
			b.WriteByte(c)
			continue
		}
		if escaped {
			b.WriteByte(c)
			escaped = false
			continue
		}
		if c == '\\' {
			b.WriteByte(c)
			escaped = true
			continue
		}
		if c == '"' {
			j := i + 1
			for j < len(s) {
				n := s[j]
				if n == ' ' || n == '\t' || n == '\n' || n == '\r' {
					j++
					continue
				}
				break
			}
			if j >= len(s) {
				b.WriteByte(c)
				inStr = false
				continue
			}
			n := s[j]
			if n == ',' || n == '}' || n == ']' || n == ':' {
				b.WriteByte(c)
				inStr = false
			} else {
				b.WriteByte('\\')
				b.WriteByte('"')
			}
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}
