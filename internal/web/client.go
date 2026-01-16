package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"tunapanel/internal/models"
)

type AgentClient struct {
	socketPath string
	timeout    time.Duration
	client     *http.Client
}

func NewAgentClient(socketPath string, timeout time.Duration) *AgentClient {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			dialer := net.Dialer{Timeout: timeout}
			return dialer.DialContext(ctx, "unix", socketPath)
		},
	}

	return &AgentClient{
		socketPath: socketPath,
		timeout:    timeout,
		client: &http.Client{
			Transport: transport,
			Timeout:   timeout,
		},
	}
}

func (c *AgentClient) Status(ctx context.Context) (models.Response, error) {
	return c.Do(ctx, models.Request{Command: "status"})
}

func (c *AgentClient) ListServices(ctx context.Context, state string) (models.Response, error) {
	command := "service.list"
	switch state {
	case "", "enabled":
		command = "service.list"
	case "running":
		command = "service.running"
	default:
		return models.Response{}, fmt.Errorf("invalid service state")
	}

	return c.Do(ctx, models.Request{Command: command})
}

func (c *AgentClient) Do(ctx context.Context, req models.Request) (models.Response, error) {
	var out models.Response

	payload, err := json.Marshal(req)
	if err != nil {
		return out, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://unix/v1/command", bytes.NewReader(payload))
	if err != nil {
		return out, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return out, classifyError(err, c.timeout)
	}
	defer resp.Body.Close()

	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&out); err != nil {
		return out, err
	}

	if resp.StatusCode != http.StatusOK {
		if out.Error != "" {
			return out, errors.New(out.Error)
		}
		return out, fmt.Errorf("agent error: %s", resp.Status)
	}
	if !out.OK {
		if out.Error != "" {
			return out, errors.New(out.Error)
		}
		return out, errors.New("agent request failed")
	}

	return out, nil
}

func classifyError(err error, timeout time.Duration) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("agent timeout after %s", timeout)
	}
	if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
		return fmt.Errorf("agent timeout after %s", timeout)
	}
	return fmt.Errorf("agent unavailable: %w", err)
}
