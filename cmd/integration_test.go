package cmd

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/opencode-ai/opencode/internal/config"
	"github.com/opencode-ai/opencode/internal/llm/provider"
	"github.com/opencode-ai/opencode/internal/llm/tools"
	"github.com/opencode-ai/opencode/internal/message"
	"github.com/stretchr/testify/assert"
)

func TestIntegration(t *testing.T) {
	// Load the config
	_, err := config.Load(".", false)
	assert.NoError(t, err)

	// Set the mock provider
	provider.MockProvider = &provider.MockClient{
		SendMessagesFunc: func(ctx context.Context, messages []message.Message, tools []tools.BaseTool) (*provider.ProviderResponse, error) {
			return &provider.ProviderResponse{
				Content: "hello",
			}, nil
		},
	}

	// Redirect stdout to a buffer
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Run the command
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "run", "main.go", "-p", "hello")
	var out bytes.Buffer
	cmd.Stdout = &out
	err = cmd.Run()
	assert.NoError(t, err)

	// Restore stdout
	w.Close()
	os.Stdout = old

	// Read the output from the buffer
	var buf bytes.Buffer
	io.Copy(&buf, r)

	// Assert the output
	assert.True(t, strings.Contains(out.String(), "hello"), "Output should contain 'hello'")
}
