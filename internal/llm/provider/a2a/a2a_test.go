package a2a

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/opencode-ai/opencode/internal/message"
	"github.com/stretchr/testify/assert"
)

func TestGenerate(t *testing.T) {
	// Create a mock A2A agent.
	mockAgent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req JSONRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var resp JSONRPCResponse
		switch req.Method {
		case "task_create":
			resp = JSONRPCResponse{
				JSONRPC: "2.0",
				Result:  "test-task-id",
				ID:      req.ID,
			}
		case "task_get":
			resp = JSONRPCResponse{
				JSONRPC: "2.0",
				Result: map[string]interface{}{
					"status": "completed",
					"artifacts": []interface{}{
						map[string]interface{}{
							"content": "Hello, world!",
						},
					},
				},
				ID: req.ID,
			}
		default:
			http.Error(w, fmt.Sprintf("unknown method: %s", req.Method), http.StatusBadRequest)
			return
		}

		if err := json.NewEncoder(w).Encode(resp); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}))
	defer mockAgent.Close()

	// Create a new A2A client that points to the mock agent.
	client := NewClient(mockAgent.URL)

	// Call the Generate method.
	messages := []message.Message{
		{
			Parts: []message.ContentPart{
				message.TextContent{
					Text: "Hello",
				},
			},
		},
	}
	resp, err := client.Generate(context.Background(), messages)

	// Assert that the response is correct.
	assert.NoError(t, err)
	assert.Equal(t, "Hello, world!", resp.Content)
}
