package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"littleclaw/pkg/providers"
	"littleclaw/pkg/tools"
	"littleclaw/pkg/workspace"
)

// registerWorkspaceTools adds tools for managing the structured workspace.
func (c *NanoCore) registerWorkspaceTools() {

	// --- list_workspace ---
	c.toolRegistry.RegisterTool(providers.ToolDefinition{
		Type: "function",
		Function: struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
		}{
			Name:        "list_workspace",
			Description: "Lists all folders registered in the workspace index (scripts, skills, tools, uploads, blogs, content, and any custom folders). Returns folder names, types, and descriptions.",
			Parameters: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
		folders := c.wsMgr.ListFolders()
		if len(folders) == 0 {
			return &tools.ToolResult{ForLLM: "Workspace has no registered folders yet."}
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Workspace root: %s\n\n", c.wsMgr.WorkspaceDir()))
		sb.WriteString(fmt.Sprintf("%d registered folder(s):\n", len(folders)))
		for _, f := range folders {
			sb.WriteString(fmt.Sprintf("- %s/ [%s] — %s (created: %s)\n",
				f.Name, f.Type, f.Description, f.CreatedAt.Format("2006-01-02")))
		}
		return &tools.ToolResult{ForLLM: sb.String()}
	})

	// --- create_workspace_folder ---
	c.toolRegistry.RegisterTool(providers.ToolDefinition{
		Type: "function",
		Function: struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
		}{
			Name:        "create_workspace_folder",
			Description: "Creates a new named folder in the workspace and registers it in the workspace index with a description. Use this to keep things organized (e.g. 'research', 'finance', 'projects/acme').",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Folder name (snake_case recommended, e.g. 'research_notes').",
					},
					"description": map[string]interface{}{
						"type":        "string",
						"description": "What this folder is for.",
					},
				},
				"required": []string{"name", "description"},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
		name, _ := args["name"].(string)
		desc, _ := args["description"].(string)
		if name == "" {
			return &tools.ToolResult{ForLLM: "Error: folder name is required."}
		}

		path, err := c.wsMgr.CreateFolder(name, desc)
		if err != nil {
			return &tools.ToolResult{ForLLM: fmt.Sprintf("Error creating folder: %v", err)}
		}
		return &tools.ToolResult{
			ForLLM: fmt.Sprintf("Folder '%s' created at %s with tracker.json initialised.", name, path),
		}
	})

	// --- track_item ---
	c.toolRegistry.RegisterTool(providers.ToolDefinition{
		Type: "function",
		Function: struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
		}{
			Name:        "track_item",
			Description: "Registers or updates a tracked item (script, skill, tool, or any file) in a folder's tracker.json. Use this immediately after creating a script or skill so it is catalogued with a description and tags.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"folder": map[string]interface{}{
						"type":        "string",
						"description": "The folder the item lives in (e.g. 'scripts', 'skills', 'tools', or any custom folder name).",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "Short unique name for the item (e.g. 'backup_db').",
					},
					"file": map[string]interface{}{
						"type":        "string",
						"description": "Relative filename within the folder (e.g. 'backup_db.sh').",
					},
					"description": map[string]interface{}{
						"type":        "string",
						"description": "What this item does.",
					},
					"tags": map[string]interface{}{
						"type":        "array",
						"description": "Optional tags to categorize the item.",
						"items":       map[string]interface{}{"type": "string"},
					},
					"notes": map[string]interface{}{
						"type":        "string",
						"description": "Any extra notes or usage examples.",
					},
				},
				"required": []string{"folder", "name"},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
		folder, _ := args["folder"].(string)
		name, _ := args["name"].(string)
		file, _ := args["file"].(string)
		description, _ := args["description"].(string)
		notes, _ := args["notes"].(string)

		var tags []string
		if rawTags, ok := args["tags"].([]interface{}); ok {
			for _, t := range rawTags {
				if s, ok := t.(string); ok {
					tags = append(tags, s)
				}
			}
		}

		if folder == "" || name == "" {
			return &tools.ToolResult{ForLLM: "Error: folder and name are required."}
		}

		item := workspace.TrackedItem{
			Name:        name,
			File:        file,
			Description: description,
			Notes:       notes,
			Tags:        tags,
		}

		if err := c.wsMgr.TrackItem(folder, item); err != nil {
			return &tools.ToolResult{ForLLM: fmt.Sprintf("Error tracking item: %v", err)}
		}

		return &tools.ToolResult{
			ForLLM: fmt.Sprintf("Item '%s' tracked in %s/tracker.json.", name, folder),
		}
	})

	// --- list_tracked ---
	c.toolRegistry.RegisterTool(providers.ToolDefinition{
		Type: "function",
		Function: struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
		}{
			Name:        "list_tracked",
			Description: "Lists all tracked items in a folder's tracker.json (scripts, skills, tools, etc.) with their run counts, last-run time, and status.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"folder": map[string]interface{}{
						"type":        "string",
						"description": "The folder to inspect (e.g. 'scripts', 'skills', 'tools').",
					},
				},
				"required": []string{"folder"},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
		folder, _ := args["folder"].(string)
		if folder == "" {
			return &tools.ToolResult{ForLLM: "Error: folder is required."}
		}

		t, err := c.wsMgr.ReadTracker(folder)
		if err != nil {
			return &tools.ToolResult{ForLLM: fmt.Sprintf("No tracker found for folder '%s'. It may not exist yet.", folder)}
		}

		if len(t.Items) == 0 {
			return &tools.ToolResult{ForLLM: fmt.Sprintf("No tracked items in %s/ yet.", folder)}
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("Tracked items in %s/ (%d total, last updated: %s):\n\n",
			folder, len(t.Items), t.UpdatedAt.Format("2006-01-02 15:04")))

		for _, item := range t.Items {
			statusIcon := "⬜"
			if item.RunCount > 0 {
				if item.LastRunOK {
					statusIcon = "✅"
				} else {
					statusIcon = "❌"
				}
			}

			lastRun := "never"
			if item.LastRunAt != nil {
				lastRun = item.LastRunAt.Format("2006-01-02 15:04")
			}

			sb.WriteString(fmt.Sprintf("%s %s", statusIcon, item.Name))
			if item.File != "" {
				sb.WriteString(fmt.Sprintf(" (%s)", item.File))
			}
			sb.WriteString("\n")
			if item.Description != "" {
				sb.WriteString(fmt.Sprintf("   %s\n", item.Description))
			}
			sb.WriteString(fmt.Sprintf("   runs: %d | last run: %s | updated: %s\n",
				item.RunCount, lastRun, item.UpdatedAt.Format("2006-01-02")))
			if len(item.Tags) > 0 {
				sb.WriteString(fmt.Sprintf("   tags: %s\n", strings.Join(item.Tags, ", ")))
			}
			if item.Notes != "" {
				sb.WriteString(fmt.Sprintf("   notes: %s\n", item.Notes))
			}
			sb.WriteString("\n")
		}

		return &tools.ToolResult{ForLLM: sb.String()}
	})

	// --- get_tracker_json ---
	c.toolRegistry.RegisterTool(providers.ToolDefinition{
		Type: "function",
		Function: struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
		}{
			Name:        "get_tracker_json",
			Description: "Returns the raw tracker.json for a folder. Useful for deep inspection or exporting tracking data.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"folder": map[string]interface{}{
						"type":        "string",
						"description": "The folder name (e.g. 'scripts', 'skills', 'tools').",
					},
				},
				"required": []string{"folder"},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
		folder, _ := args["folder"].(string)
		if folder == "" {
			return &tools.ToolResult{ForLLM: "Error: folder is required."}
		}

		t, err := c.wsMgr.ReadTracker(folder)
		if err != nil {
			return &tools.ToolResult{ForLLM: fmt.Sprintf("No tracker found for '%s'.", folder)}
		}

		data, err := json.MarshalIndent(t, "", "  ")
		if err != nil {
			return &tools.ToolResult{ForLLM: fmt.Sprintf("Error serialising tracker: %v", err)}
		}
		return &tools.ToolResult{ForLLM: string(data)}
	})

	// --- record_script_run ---
	c.toolRegistry.RegisterTool(providers.ToolDefinition{
		Type: "function",
		Function: struct {
			Name        string                 `json:"name"`
			Description string                 `json:"description"`
			Parameters  map[string]interface{} `json:"parameters"`
		}{
			Name:        "record_script_run",
			Description: "Manually records that a script was executed, updating its run stats in the tracker. Use this after running a script via `exec` so the history stays accurate.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"folder": map[string]interface{}{
						"type":        "string",
						"description": "The folder containing the script (usually 'scripts').",
					},
					"name": map[string]interface{}{
						"type":        "string",
						"description": "The tracked name of the script.",
					},
					"args": map[string]interface{}{
						"type":        "string",
						"description": "Arguments that were passed to the script.",
					},
					"output": map[string]interface{}{
						"type":        "string",
						"description": "Output of the script execution (truncated if too long).",
					},
					"success": map[string]interface{}{
						"type":        "boolean",
						"description": "Whether the script ran successfully.",
					},
				},
				"required": []string{"folder", "name"},
			},
		},
	}, func(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
		folder, _ := args["folder"].(string)
		name, _ := args["name"].(string)
		argsStr, _ := args["args"].(string)
		output, _ := args["output"].(string)
		success, _ := args["success"].(bool)

		if folder == "" || name == "" {
			return &tools.ToolResult{ForLLM: "Error: folder and name are required."}
		}

		if err := c.wsMgr.RecordRun(folder, name, argsStr, output, success); err != nil {
			return &tools.ToolResult{ForLLM: fmt.Sprintf("Error recording run: %v", err)}
		}

		ts := time.Now().Format("2006-01-02 15:04:05")
		status := "success"
		if !success {
			status = "failure"
		}
		return &tools.ToolResult{
			ForLLM: fmt.Sprintf("Recorded %s run of '%s' in %s/tracker.json at %s.", status, name, folder, ts),
		}
	})
}
