package a2a

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/opencode-ai/opencode/internal/llm/provider"
	"github.com/opencode-ai/opencode/internal/message"
)

// Client is a client for the A2A protocol.
type Client struct {
	Endpoint string
	HTTP     *http.Client
}

// NewClient creates a new A2A client.
func NewClient(endpoint string) *Client {
	return &Client{
		Endpoint: endpoint,
		HTTP:     http.DefaultClient,
	}
}

// JSONRPCRequest represents a JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	Method  string        `json:"method"`
	Params  interface{}   `json:"params"`
	ID      int           `json:"id"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	Result  interface{} `json:"result,omitempty"`
	Error   *Error      `json:"error,omitempty"`
	ID      int         `json:"id"`
}

// Error represents a JSON-RPC 2.0 error.
type Error struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Error returns the error message.
func (e *Error) Error() string {
	return fmt.Sprintf("jsonrpc: code %d, message: %s", e.Code, e.Message)
}

// Call sends a JSON-RPC 2.0 request to the A2A endpoint.
func (c *Client) Call(ctx context.Context, method string, params interface{}) (*JSONRPCResponse, error) {
	req := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      1,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal jsonrpc request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.Endpoint, bytes.NewBuffer(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to send http request: %w", err)
	}
	defer resp.Body.Close()

	var jsonRPCResp JSONRPCResponse
	if err := json.NewDecoder(resp.Body).Decode(&jsonRPCResp); err != nil {
		return nil, fmt.Errorf("failed to decode jsonrpc response: %w", err)
	}

	if jsonRPCResp.Error != nil {
		return nil, jsonRPCResp.Error
	}

	return &jsonRPCResp, nil
}

// Generate generates a response from the A2A agent.
func (c *Client) Generate(ctx context.Context, messages []message.Message) (*provider.ProviderResponse, error) {
	// For now, we'll just send the last message to the agent.
	// In the future, we may want to send the entire conversation.
	if len(messages) == 0 {
		return nil, fmt.Errorf("no messages to generate from")
	}
	lastMessage := messages[len(messages)-1]

	var textContent string
	for _, part := range lastMessage.Parts {
		if tc, ok := part.(message.TextContent); ok {
			textContent = tc.Text
			break
		}
	}

	// The A2A protocol uses a "task" object to manage the lifecycle of a request.
	// We'll create a simple task here.
	task := map[string]interface{}{
		"title":       "Generate response",
		"description": textContent,
	}

	resp, err := c.Call(ctx, "task_create", task)
	if err != nil {
		return nil, fmt.Errorf("failed to create task: %w", err)
	}

	// The result of the task_create method should be the task ID.
	taskID, ok := resp.Result.(string)
	if !ok {
		return nil, fmt.Errorf("unexpected result from task_create: %v", resp.Result)
	}

	// Now we'll poll the task for completion.
	// In a real implementation, we would use SSE for real-time updates.
	for {
		taskResult, err := c.Call(ctx, "task_get", map[string]interface{}{"task_id": taskID})
		if err != nil {
			return nil, fmt.Errorf("failed to get task: %w", err)
		}

		// The result of the task_get method should be the task object.
		task, ok := taskResult.Result.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("unexpected result from task_get: %v", taskResult.Result)
		}

		status, ok := task["status"].(string)
		if !ok {
			return nil, fmt.Errorf("unexpected status in task: %v", task)
		}

		if status == "completed" {
			// The output of the task is an "artifact".
			artifacts, ok := task["artifacts"].([]interface{})
			if !ok || len(artifacts) == 0 {
				return nil, fmt.Errorf("no artifacts in completed task: %v", task)
			}
			firstArtifact, ok := artifacts[0].(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("unexpected artifact format: %v", artifacts[0])
			}
			content, ok := firstArtifact["content"].(string)
			if !ok {
				return nil, fmt.Errorf("unexpected content format in artifact: %v", firstArtifact)
			}

			return &provider.ProviderResponse{
				Content: content,
			}, nil
		}

		if status == "failed" {
			return nil, fmt.Errorf("task failed: %v", task)
		}

		// Wait a bit before polling again.
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}
}
