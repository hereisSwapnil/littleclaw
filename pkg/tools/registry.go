package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"littleclaw/pkg/providers"
)

// ToolResult represents the output of a tool execution.
type ToolResult struct {
	ForLLM  string // Sent back to the language model
	ForUser string // (Optional) Sent directly to the user
}

// Handler handles the execution of a specific tool.
type Handler func(ctx context.Context, args map[string]interface{}) *ToolResult

// Registry holds the registered tools and their handlers.
type Registry struct {
	workspaceDir string
	definitions  []providers.ToolDefinition
	handlers     map[string]Handler
}

// NewRegistry initializes a tool registry configured for the given workspace.
func NewRegistry(workspaceDir string) *Registry {
	r := &Registry{
		workspaceDir: workspaceDir,
		definitions:  []providers.ToolDefinition{},
		handlers:     make(map[string]Handler),
	}

	// Register default sandbox tools
	r.registerCoreTools()
	return r
}

func (r *Registry) RegisterTool(def providers.ToolDefinition, handler Handler) {
	r.definitions = append(r.definitions, def)
	r.handlers[def.Function.Name] = handler
}

func (r *Registry) GetDefinitions() []providers.ToolDefinition {
	return r.definitions
}

func (r *Registry) Execute(ctx context.Context, name string, args map[string]interface{}) *ToolResult {
	handler, exists := r.handlers[name]
	if !exists {
		return &ToolResult{ForLLM: fmt.Sprintf("Error: Tool '%s' not found", name)}
	}
	return handler(ctx, args)
}

// Core execution sandbox tools
func (r *Registry) registerCoreTools() {
	// read_file
	r.RegisterTool(providers.ToolDefinition{
		Type: "function",
		Function: struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
		}{
			Name:        "read_file",
			Description: "Reads the content of a file within the sandbox workspace.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Relative path to the file within the workspace.",
					},
				},
				"required": []string{"path"},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) *ToolResult {
		p, ok := args["path"].(string)
		if !ok {
			return &ToolResult{ForLLM: "Error: path must be a string"}
		}
		
		safePath, err := r.resolveWorkspacePath(p)
		if err != nil {
			return &ToolResult{ForLLM: err.Error()}
		}

		data, err := os.ReadFile(safePath)
		if err != nil {
			return &ToolResult{ForLLM: fmt.Sprintf("Error reading file: %v", err)}
		}
		return &ToolResult{ForLLM: string(data)}
	})

	// exec (sandboxed shell)
	r.RegisterTool(providers.ToolDefinition{
		Type: "function",
		Function: struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
		}{
			Name:        "exec",
			Description: "Executes a shell command inside the workspace directory.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"command": map[string]interface{}{
						"type":        "string",
						"description": "The shell command to run.",
					},
				},
				"required": []string{"command"},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) *ToolResult {
		cmdStr, ok := args["command"].(string)
		if !ok {
			return &ToolResult{ForLLM: "Error: command must be a string"}
		}

		// Very basic security boundary. In a real system, you'd want closer inspection.
		if isBannedCommand(cmdStr) {
			return &ToolResult{ForLLM: "Command blocked by safety guard (dangerous pattern detected)"}
		}

		cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
		cmd.Dir = r.workspaceDir

		output, err := cmd.CombinedOutput()
		if err != nil {
			return &ToolResult{ForLLM: fmt.Sprintf("Command failed: %s\nOutput: %s", err, output)}
		}

		return &ToolResult{
			ForLLM:  string(output),
			ForUser: string(output), // Commands often produce useful user-facing output
		}
	})
}

func (r *Registry) resolveWorkspacePath(p string) (string, error) {
	cleanPath := filepath.Clean(filepath.Join(r.workspaceDir, p))
	if !strings.HasPrefix(cleanPath, r.workspaceDir) {
		return "", fmt.Errorf("Error: Path %s escapes workspace boundaries", p)
	}
	return cleanPath, nil
}

func isBannedCommand(cmd string) bool {
	bans := []string{"rm -rf", "mkfs", "dd if="}
	for _, b := range bans {
		if strings.Contains(cmd, b) {
			return true
		}
	}
	return false
}
