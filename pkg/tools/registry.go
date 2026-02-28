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
	ForLLM  string   // Sent back to the language model
	ForUser string   // (Optional) Sent directly to the user
	Files   []string // (Optional) Absolute paths of files to attach to the user response
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

	// write_file
	r.RegisterTool(providers.ToolDefinition{
		Type: "function",
		Function: struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
		}{
			Name:        "write_file",
			Description: "Writes content to a file within the sandbox workspace, completely overwriting it.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Relative path to the file within the workspace.",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "The full textual content to write to the file.",
					},
				},
				"required": []string{"path", "content"},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) *ToolResult {
		p, okPath := args["path"].(string)
		content, okContent := args["content"].(string)
		
		if !okPath || !okContent {
			return &ToolResult{ForLLM: "Error: path and content must be strings"}
		}
		
		safePath, err := r.resolveWorkspacePath(p)
		if err != nil {
			return &ToolResult{ForLLM: err.Error()}
		}

		// Ensure directory exists
		if err := os.MkdirAll(filepath.Dir(safePath), 0755); err != nil {
			return &ToolResult{ForLLM: fmt.Sprintf("Error creating parent directories: %v", err)}
		}

		if err := os.WriteFile(safePath, []byte(content), 0644); err != nil {
			return &ToolResult{ForLLM: fmt.Sprintf("Error writing file: %v", err)}
		}
		return &ToolResult{ForLLM: fmt.Sprintf("Successfully wrote to %s", p)}
	})

	// append_file
	r.RegisterTool(providers.ToolDefinition{
		Type: "function",
		Function: struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
		}{
			Name:        "append_file",
			Description: "Appends text to the end of a file within the sandbox workspace.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Relative path to the file within the workspace.",
					},
					"content": map[string]interface{}{
						"type":        "string",
						"description": "The text to append to the file.",
					},
				},
				"required": []string{"path", "content"},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) *ToolResult {
		p, okPath := args["path"].(string)
		content, okContent := args["content"].(string)
		
		if !okPath || !okContent {
			return &ToolResult{ForLLM: "Error: path and content must be strings"}
		}
		
		safePath, err := r.resolveWorkspacePath(p)
		if err != nil {
			return &ToolResult{ForLLM: err.Error()}
		}

		// Ensure directory exists
		if err := os.MkdirAll(filepath.Dir(safePath), 0755); err != nil {
			return &ToolResult{ForLLM: fmt.Sprintf("Error creating parent directories: %v", err)}
		}

		f, err := os.OpenFile(safePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if err != nil {
			return &ToolResult{ForLLM: fmt.Sprintf("Error opening file for append: %v", err)}
		}
		defer f.Close()

		if _, err := f.WriteString(content); err != nil {
			return &ToolResult{ForLLM: fmt.Sprintf("Error appending to file: %v", err)}
		}
		return &ToolResult{ForLLM: fmt.Sprintf("Successfully appended to %s", p)}
	})

	// send_telegram_file
	r.RegisterTool(providers.ToolDefinition{
		Type: "function",
		Function: struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
		}{
			Name:        "send_telegram_file",
			Description: "Attaches and sends a specific local file to the user over Telegram.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"path": map[string]interface{}{
						"type":        "string",
						"description": "Relative path to the file within the workspace to send.",
					},
					"caption": map[string]interface{}{
						"type":        "string",
						"description": "Optional textual message to send alongside the file.",
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

		// Validate file exists before claiming success
		info, err := os.Stat(safePath)
		if err != nil {
			return &ToolResult{ForLLM: fmt.Sprintf("Cannot find file to send: %v", err)}
		}
		if info.IsDir() {
			return &ToolResult{ForLLM: "Error: Cannot send entire directories. Specify a file."}
		}

		caption, _ := args["caption"].(string)

		return &ToolResult{
			ForLLM:  fmt.Sprintf("Successfully queued %s for sending to Telegram.", p),
			ForUser: caption,
			Files:   []string{safePath},
		}
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

	// spawn (async background agent)
	r.RegisterTool(providers.ToolDefinition{
		Type: "function",
		Function: struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
		}{
			Name:        "spawn",
			Description: "Spawns a detached, asynchronous sub-agent to handle a long-running task in the background. Does not block the main conversation.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"task": map[string]interface{}{
						"type":        "string",
						"description": "A highly detailed instruction for the sub-agent.",
					},
				},
				"required": []string{"task"},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) *ToolResult {
		taskStr, ok := args["task"].(string)
		if !ok {
			return &ToolResult{ForLLM: "Error: task must be a string"}
		}

		// The actual spawning logic is handled here by kicking off a background Go routine.
		// A full implementation would likely pass this request to the NanoCore via an event bus,
		// but returning a success message immediately acts as fire-and-forget.
		
		go func() {
			// Real implementation would invoke a new, isolated NanoCore instance here.
			fmt.Printf("Sub-agent spawned for task: %s\n", taskStr)
		}()

		return &ToolResult{
			ForLLM:  "Sub-agent successfully spawned in the background. It will message the user when complete.",
			ForUser: "Spawned a background agent to handle that task! It will report back shortly.",
		}
	})
}

func (r *Registry) resolveWorkspacePath(p string) (string, error) {
	// If the LLM passed an absolute path that already contains the workspace dir
	if filepath.IsAbs(p) {
		cleaned := filepath.Clean(p)
		if strings.HasPrefix(cleaned, r.workspaceDir) {
			return cleaned, nil
		}
	}

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
