package tools

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"littleclaw/pkg/memory"
	"littleclaw/pkg/providers"
	"littleclaw/pkg/workspace"
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
	memoryStore  *memory.Store      // Optional reference to memory store
	wsMgr        *workspace.Manager // Structured workspace manager
	tavilyAPIKey string             // Optional Tavily API key for web_search
	definitions  []providers.ToolDefinition
	handlers     map[string]Handler
}

// NewRegistry initializes a tool registry configured for the given workspace.
func NewRegistry(workspaceDir string, mem *memory.Store, wsMgr *workspace.Manager, tavilyAPIKey string) *Registry {
	r := &Registry{
		workspaceDir: workspaceDir,
		memoryStore:  mem,
		wsMgr:        wsMgr,
		tavilyAPIKey: tavilyAPIKey,
		definitions:  []providers.ToolDefinition{},
		handlers:     make(map[string]Handler),
	}

	// Register default sandbox tools
	r.registerCoreTools()

	// Register web tools (web_fetch always available; web_search needs Tavily key)
	r.registerWebTools()

	// Load dynamic skills
	r.LoadSkills()

	return r
}

func (r *Registry) LoadSkills() {
	skillsDir := filepath.Join(r.workspaceDir, "skills")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		fmt.Printf("Error creating skills directory: %v\n", err)
		return
	}

	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		fmt.Printf("Error reading skills directory: %v\n", err)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		// Only load .sh and .py files
		if !strings.HasSuffix(name, ".sh") && !strings.HasSuffix(name, ".py") {
			continue
		}

		toolName := strings.TrimSuffix(name, filepath.Ext(name))
		scriptPath := filepath.Join(skillsDir, name)

		// Pull description from tracker if available
		description := fmt.Sprintf("Dynamic skill: executes the %s script. Ensure to pass required arguments.", name)
		if r.wsMgr != nil {
			if t, err := r.wsMgr.ReadTracker("skills"); err == nil {
				if item, ok := t.Items[toolName]; ok && item.Description != "" {
					description = item.Description
				}
			}
		}

		// Define the tool
		def := providers.ToolDefinition{
			Type: "function",
		}
		def.Function.Name = toolName
		def.Function.Description = description
		def.Function.Parameters = map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"args": map[string]interface{}{
					"type":        "string",
					"description": "Arguments to pass to the script, separated by spaces.",
				},
			},
		}

		// Capture loop vars for closure
		capturedName := name
		capturedToolName := toolName
		capturedPath := scriptPath

		// Create handler
		handler := func(ctx context.Context, args map[string]interface{}) *ToolResult {
			cmdArgsStr, _ := args["args"].(string)

			// Simple split by space for args (a more robust parser might handle quotes)
			var cmdArgs []string
			if cmdArgsStr != "" {
				cmdArgs = strings.Fields(cmdArgsStr)
			}

			var cmd *exec.Cmd
			if strings.HasSuffix(capturedName, ".sh") {
				execArgs := append([]string{capturedPath}, cmdArgs...)
				cmd = exec.CommandContext(ctx, "sh", execArgs...)
			} else {
				execArgs := append([]string{capturedPath}, cmdArgs...)
				cmd = exec.CommandContext(ctx, "python3", execArgs...)
			}
			cmd.Dir = r.workspaceDir

			output, err := cmd.CombinedOutput()
			runOK := err == nil
			outStr := string(output)

			// Record run in tracker
			if r.wsMgr != nil {
				_ = r.wsMgr.RecordRun("skills", capturedToolName, cmdArgsStr, outStr, runOK)
			}

			if err != nil {
				return &ToolResult{ForLLM: fmt.Sprintf("Skill failed: %s\nOutput: %s", err, output)}
			}
			return &ToolResult{ForLLM: outStr}
		}

		r.RegisterTool(def, handler)
		fmt.Printf("Registered dynamic skill: %s\n", toolName)
	}
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
	// list_entities
	r.RegisterTool(providers.ToolDefinition{
		Type: "function",
		Function: struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
		}{
			Name:        "list_entities",
			Description: "Lists all currently known entity topics in the memory system. Use this to avoid creating duplicate entities.",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) *ToolResult {
		if r.memoryStore == nil {
			return &ToolResult{ForLLM: "Error: Memory store is not attached to this registry."}
		}

		entities, err := r.memoryStore.ListEntities()
		if err != nil {
			return &ToolResult{ForLLM: fmt.Sprintf("Error reading entities: %v", err)}
		}

		if len(entities) == 0 {
			return &ToolResult{ForLLM: "No entities found in memory."}
		}

		return &ToolResult{ForLLM: fmt.Sprintf("Known entities: %s", strings.Join(entities, ", "))}
	})

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
		if IsBannedCommand(cmdStr) {
			return &ToolResult{ForLLM: "Command blocked by safety guard (dangerous pattern detected)"}
		}

		cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
		cmd.Dir = r.workspaceDir

		output, err := cmd.CombinedOutput()
		if err != nil {
			return &ToolResult{ForLLM: fmt.Sprintf("Command failed: %s\nOutput: %s", err, output)}
		}

		return &ToolResult{
			ForLLM: string(output),
		}
	})

	// reload_skills
	r.RegisterTool(providers.ToolDefinition{
		Type: "function",
		Function: struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
		}{
			Name:        "reload_skills",
			Description: "Reloads dynamic executable skills from the skills/ directory. Use this after writing a new script to make it available as a tool.",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) *ToolResult {
		r.LoadSkills()
		return &ToolResult{
			ForLLM: "Dynamic skills reloaded successfully.",
		}
	})
}

func (r *Registry) resolveWorkspacePath(p string) (string, error) {
	if r.wsMgr != nil {
		// If the LLM passed an absolute path that already contains the workspace dir, strip it
		if filepath.IsAbs(p) {
			cleaned := filepath.Clean(p)
			if strings.HasPrefix(cleaned, r.workspaceDir) {
				p = strings.TrimPrefix(cleaned, r.workspaceDir)
				p = strings.TrimPrefix(p, "/")
			}
		}
		safePath, err := r.wsMgr.ResolvePath(p)
		if err != nil {
			return "", fmt.Errorf("Error: %w", err)
		}
		// Safeguard: Prevent LLM from directly touching memory files
		base := filepath.Base(safePath)
		dir := filepath.Dir(safePath)
		if IsProtectedMemoryPath(base, dir) {
			return "", fmt.Errorf("Error: Direct file access to memory files is prohibited. Use memory tools instead.")
		}
		return safePath, nil
	}

	// Fallback (no workspace manager)
	if filepath.IsAbs(p) {
		cleaned := filepath.Clean(p)
		if strings.HasPrefix(cleaned, r.workspaceDir) {
			p = strings.TrimPrefix(cleaned, r.workspaceDir)
			p = strings.TrimPrefix(p, "/")
		}
	}

	cleanPath := filepath.Clean(filepath.Join(r.workspaceDir, p))
	if !strings.HasPrefix(cleanPath, r.workspaceDir) {
		return "", fmt.Errorf("Error: Path %s escapes workspace boundaries", p)
	}

	base := filepath.Base(cleanPath)
	dir := filepath.Dir(cleanPath)
	if IsProtectedMemoryPath(base, dir) {
		return "", fmt.Errorf("Error: Direct file access to memory files is prohibited. You MUST use memory tools (update_core_memory, append_core_memory, read_core_memory, write_entity, list_entities, read_entity, search_history) instead.")
	}

	return cleanPath, nil
}

func IsBannedCommand(cmd string) bool {
	bans := []string{"rm -rf", "mkfs", "dd if="}
	for _, b := range bans {
		if strings.Contains(cmd, b) {
			return true
		}
	}
	return false
}

// dailyLogPattern matches daily log files like "2026-03-11.md"
var dailyLogPattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\.md$`)

// isProtectedMemoryPath returns true if the given file path belongs to a protected memory file.
// This includes MEMORY.md, daily logs, INTERNAL.md, entity files, and summaries.
func IsProtectedMemoryPath(base, dir string) bool {
	// Core memory files
	if base == "MEMORY.md" || base == "HISTORY.md" || base == "INTERNAL.md" {
		return true
	}
	// Identity files
	if base == "SOUL.md" || base == "IDENTITY.md" || base == "USER.md" || base == "HEARTBEAT.md" {
		return true
	}
	// Entity directory
	if strings.Contains(dir, "ENTITIES") || base == "ENTITIES" {
		return true
	}
	// Daily log files (YYYY-MM-DD.md)
	if dailyLogPattern.MatchString(base) && strings.HasSuffix(dir, "memory") {
		return true
	}
	// Summary files
	if strings.Contains(dir, "summaries") {
		return true
	}
	// Memory backup files
	if strings.HasPrefix(base, "MEMORY_") && strings.HasSuffix(base, ".md") {
		return true
	}
	// History/internal archive files
	if (strings.HasPrefix(base, "HISTORY_ARCHIVE_") || strings.HasPrefix(base, "INTERNAL_ARCHIVE_")) && strings.HasSuffix(base, ".md") {
		return true
	}
	return false
}
