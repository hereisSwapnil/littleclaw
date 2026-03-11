package tools_test

import (
	"littleclaw/pkg/tools"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"littleclaw/pkg/memory"
	"littleclaw/pkg/workspace"
)

// newTestRegistry creates a tools.Registry backed by a temp workspace directory.
func newTestRegistry(t *testing.T) (*tools.Registry, string) {
	t.Helper()
	dir := t.TempDir()

	mem, err := memory.NewStore(dir)
	if err != nil {
		t.Fatalf("memory.NewStore: %v", err)
	}
	wsMgr, err := workspace.NewManager(dir)
	if err != nil {
		t.Fatalf("workspace.NewManager: %v", err)
	}

	r := tools.NewRegistry(dir, mem, wsMgr, "")
	return r, dir
}

// ---------------------------------------------------------------------------
// tools.Registry bootstrap tests
// ---------------------------------------------------------------------------

func TestNewRegistry_RegistersCoreTools(t *testing.T) {
	r, _ := newTestRegistry(t)

	defs := r.GetDefinitions()
	names := make(map[string]bool)
	for _, d := range defs {
		names[d.Function.Name] = true
	}

	required := []string{"read_file", "write_file", "append_file", "exec", "send_telegram_file", "reload_skills"}
	for _, name := range required {
		if !names[name] {
			t.Errorf("expected tool %q to be registered", name)
		}
	}
}

func TestRegistry_ExecuteUnknownTool(t *testing.T) {
	r, _ := newTestRegistry(t)

	result := r.Execute(context.Background(), "nonexistent_tool", nil)
	if result == nil || !strings.Contains(result.ForLLM, "not found") {
		t.Errorf("expected 'not found' error for unknown tool, got %v", result)
	}
}

// ---------------------------------------------------------------------------
// Path protection tests
// ---------------------------------------------------------------------------

func TestResolveWorkspacePath_EscapeBlocked(t *testing.T) {
	r, _ := newTestRegistry(t)

	// Attempt to escape workspace with ../
	result := r.Execute(context.Background(), "read_file", map[string]interface{}{
		"path": "../../etc/passwd",
	})
	if result == nil {
		t.Fatal("expected a result, got nil")
	}
	if !strings.Contains(strings.ToLower(result.ForLLM), "error") &&
		!strings.Contains(strings.ToLower(result.ForLLM), "escapes") &&
		!strings.Contains(strings.ToLower(result.ForLLM), "prohibited") {
		t.Errorf("expected path-escape error, got: %q", result.ForLLM)
	}
}

func TestResolveWorkspacePath_ProtectedMemoryFile(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"MEMORY.md", "memory/MEMORY.md"},
		{"SOUL.md", "memory/SOUL.md"},
		{"IDENTITY.md", "memory/IDENTITY.md"},
		{"USER.md", "memory/USER.md"},
		{"INTERNAL.md", "memory/INTERNAL.md"},
	}

	r, _ := newTestRegistry(t)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := r.Execute(context.Background(), "read_file", map[string]interface{}{
				"path": tc.path,
			})
			if result == nil {
				t.Fatal("expected a result, got nil")
			}
			if !strings.Contains(strings.ToLower(result.ForLLM), "error") &&
				!strings.Contains(strings.ToLower(result.ForLLM), "prohibited") {
				t.Errorf("expected protected path error for %q, got: %q", tc.path, result.ForLLM)
			}
		})
	}
}

func TestResolveWorkspacePath_ValidPath(t *testing.T) {
	r, dir := newTestRegistry(t)

	// Write a file directly, then read it via the tool
	testFile := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(testFile, []byte("hello world"), 0644); err != nil {
		t.Fatal(err)
	}

	result := r.Execute(context.Background(), "read_file", map[string]interface{}{
		"path": "hello.txt",
	})
	if result == nil {
		t.Fatal("expected a result")
	}
	if result.ForLLM != "hello world" {
		t.Errorf("read_file returned %q, want %q", result.ForLLM, "hello world")
	}
}

// ---------------------------------------------------------------------------
// read_file tool tests
// ---------------------------------------------------------------------------

func TestReadFile_MissingFile(t *testing.T) {
	r, _ := newTestRegistry(t)

	result := r.Execute(context.Background(), "read_file", map[string]interface{}{
		"path": "does_not_exist.txt",
	})
	if !strings.Contains(strings.ToLower(result.ForLLM), "error") {
		t.Errorf("read_file on missing file should return error, got %q", result.ForLLM)
	}
}

func TestReadFile_BadArgsType(t *testing.T) {
	r, _ := newTestRegistry(t)

	result := r.Execute(context.Background(), "read_file", map[string]interface{}{
		"path": 12345, // wrong type
	})
	if !strings.Contains(strings.ToLower(result.ForLLM), "error") {
		t.Errorf("read_file with non-string path should return error, got %q", result.ForLLM)
	}
}

// ---------------------------------------------------------------------------
// write_file tool tests
// ---------------------------------------------------------------------------

func TestWriteFile_CreatesFile(t *testing.T) {
	r, dir := newTestRegistry(t)

	result := r.Execute(context.Background(), "write_file", map[string]interface{}{
		"path":    "newfile.txt",
		"content": "test content",
	})
	if strings.Contains(strings.ToLower(result.ForLLM), "error") {
		t.Errorf("write_file failed: %q", result.ForLLM)
	}

	data, err := os.ReadFile(filepath.Join(dir, "newfile.txt"))
	if err != nil {
		t.Fatalf("expected file to be created: %v", err)
	}
	if string(data) != "test content" {
		t.Errorf("file content = %q, want %q", string(data), "test content")
	}
}

func TestWriteFile_OverwritesExisting(t *testing.T) {
	r, dir := newTestRegistry(t)

	testFile := filepath.Join(dir, "overwrite.txt")
	_ = os.WriteFile(testFile, []byte("original"), 0644)

	r.Execute(context.Background(), "write_file", map[string]interface{}{
		"path":    "overwrite.txt",
		"content": "replaced",
	})

	data, _ := os.ReadFile(testFile)
	if string(data) != "replaced" {
		t.Errorf("file not overwritten: got %q", string(data))
	}
}

func TestWriteFile_CreatesParentDirs(t *testing.T) {
	r, dir := newTestRegistry(t)

	r.Execute(context.Background(), "write_file", map[string]interface{}{
		"path":    "nested/subdir/file.txt",
		"content": "deep",
	})

	data, err := os.ReadFile(filepath.Join(dir, "nested", "subdir", "file.txt"))
	if err != nil {
		t.Fatalf("expected nested file to be created: %v", err)
	}
	if string(data) != "deep" {
		t.Errorf("nested file content = %q, want %q", string(data), "deep")
	}
}

func TestWriteFile_BadArgs(t *testing.T) {
	r, _ := newTestRegistry(t)

	result := r.Execute(context.Background(), "write_file", map[string]interface{}{
		"path": "foo.txt",
		// missing "content"
	})
	if !strings.Contains(strings.ToLower(result.ForLLM), "error") {
		t.Errorf("write_file missing content should error, got %q", result.ForLLM)
	}
}

// ---------------------------------------------------------------------------
// append_file tool tests
// ---------------------------------------------------------------------------

func TestAppendFile_AppendsToExistingFile(t *testing.T) {
	r, dir := newTestRegistry(t)

	testFile := filepath.Join(dir, "log.txt")
	_ = os.WriteFile(testFile, []byte("line1\n"), 0644)

	r.Execute(context.Background(), "append_file", map[string]interface{}{
		"path":    "log.txt",
		"content": "line2\n",
	})

	data, _ := os.ReadFile(testFile)
	if !strings.Contains(string(data), "line1") || !strings.Contains(string(data), "line2") {
		t.Errorf("append_file did not preserve original content. Got: %q", string(data))
	}
}

func TestAppendFile_CreatesFileIfMissing(t *testing.T) {
	r, dir := newTestRegistry(t)

	r.Execute(context.Background(), "append_file", map[string]interface{}{
		"path":    "new_append.txt",
		"content": "first line",
	})

	data, err := os.ReadFile(filepath.Join(dir, "new_append.txt"))
	if err != nil {
		t.Fatalf("expected file to be created by append: %v", err)
	}
	if string(data) != "first line" {
		t.Errorf("appended content = %q, want %q", string(data), "first line")
	}
}

// ---------------------------------------------------------------------------
// write_file → read_file round-trip
// ---------------------------------------------------------------------------

func TestWriteReadRoundTrip(t *testing.T) {
	r, _ := newTestRegistry(t)
	ctx := context.Background()

	content := "The quick brown fox jumps over the lazy dog."

	writeResult := r.Execute(ctx, "write_file", map[string]interface{}{
		"path":    "roundtrip.txt",
		"content": content,
	})
	if strings.Contains(strings.ToLower(writeResult.ForLLM), "error") {
		t.Fatalf("write_file failed: %q", writeResult.ForLLM)
	}

	readResult := r.Execute(ctx, "read_file", map[string]interface{}{
		"path": "roundtrip.txt",
	})
	if readResult.ForLLM != content {
		t.Errorf("round-trip mismatch: got %q, want %q", readResult.ForLLM, content)
	}
}

// ---------------------------------------------------------------------------
// send_telegram_file tool tests
// ---------------------------------------------------------------------------

func TestSendTelegramFile_MissingFile(t *testing.T) {
	r, _ := newTestRegistry(t)

	result := r.Execute(context.Background(), "send_telegram_file", map[string]interface{}{
		"path": "nonexistent_file.txt",
	})
	if !strings.Contains(strings.ToLower(result.ForLLM), "cannot find") &&
		!strings.Contains(strings.ToLower(result.ForLLM), "error") {
		t.Errorf("expected error for missing file, got: %q", result.ForLLM)
	}
}

func TestSendTelegramFile_Directory(t *testing.T) {
	r, dir := newTestRegistry(t)
	_ = os.MkdirAll(filepath.Join(dir, "mydir"), 0755)

	result := r.Execute(context.Background(), "send_telegram_file", map[string]interface{}{
		"path": "mydir",
	})
	if !strings.Contains(strings.ToLower(result.ForLLM), "cannot send entire") &&
		!strings.Contains(strings.ToLower(result.ForLLM), "error") {
		t.Errorf("expected error when sending a directory, got: %q", result.ForLLM)
	}
}

func TestSendTelegramFile_ValidFile(t *testing.T) {
	r, dir := newTestRegistry(t)
	testFile := filepath.Join(dir, "attach.txt")
	_ = os.WriteFile(testFile, []byte("attachment"), 0644)

	result := r.Execute(context.Background(), "send_telegram_file", map[string]interface{}{
		"path":    "attach.txt",
		"caption": "Here is your file",
	})
	if strings.Contains(strings.ToLower(result.ForLLM), "error") {
		t.Errorf("send_telegram_file failed unexpectedly: %q", result.ForLLM)
	}
	if len(result.Files) == 0 {
		t.Error("expected Files to be set in result")
	}
}

// ---------------------------------------------------------------------------
// exec tool tests
// ---------------------------------------------------------------------------

func TestExec_ValidCommand(t *testing.T) {
	r, _ := newTestRegistry(t)

	result := r.Execute(context.Background(), "exec", map[string]interface{}{
		"command": "echo hello",
	})
	if !strings.Contains(result.ForLLM, "hello") {
		t.Errorf("exec 'echo hello' output = %q, want 'hello'", result.ForLLM)
	}
}

func TestExec_CommandFailure(t *testing.T) {
	r, _ := newTestRegistry(t)

	result := r.Execute(context.Background(), "exec", map[string]interface{}{
		"command": "exit 1",
	})
	if !strings.Contains(strings.ToLower(result.ForLLM), "failed") &&
		!strings.Contains(strings.ToLower(result.ForLLM), "error") {
		t.Errorf("exec with failing command should return error, got: %q", result.ForLLM)
	}
}

// ---------------------------------------------------------------------------
// tools.IsBannedCommand tests
// ---------------------------------------------------------------------------

func TestIsBannedCommand_Banned(t *testing.T) {
	cases := []string{
		"rm -rf /",
		"sudo rm -rf /home",
		"mkfs /dev/sda",
		"dd if=/dev/zero of=/dev/sda",
	}
	for _, cmd := range cases {
		if !tools.IsBannedCommand(cmd) {
			t.Errorf("tools.IsBannedCommand(%q) = false, want true", cmd)
		}
	}
}

func TestIsBannedCommand_Allowed(t *testing.T) {
	cases := []string{
		"ls -la",
		"echo hello",
		"cat file.txt",
		"go test ./...",
		"python3 script.py",
	}
	for _, cmd := range cases {
		if tools.IsBannedCommand(cmd) {
			t.Errorf("tools.IsBannedCommand(%q) = true, want false", cmd)
		}
	}
}

// ---------------------------------------------------------------------------
// tools.IsProtectedMemoryPath tests
// ---------------------------------------------------------------------------

func TestIsProtectedMemoryPath(t *testing.T) {
	t.Run("MEMORY.md", func(t *testing.T) {
		if !tools.IsProtectedMemoryPath("MEMORY.md", "/workspace/memory") {
			t.Error("MEMORY.md should be protected")
		}
	})
	t.Run("SOUL.md", func(t *testing.T) {
		if !tools.IsProtectedMemoryPath("SOUL.md", "/workspace/memory") {
			t.Error("SOUL.md should be protected")
		}
	})
	t.Run("IDENTITY.md", func(t *testing.T) {
		if !tools.IsProtectedMemoryPath("IDENTITY.md", "/workspace/memory") {
			t.Error("IDENTITY.md should be protected")
		}
	})
	t.Run("INTERNAL.md", func(t *testing.T) {
		if !tools.IsProtectedMemoryPath("INTERNAL.md", "/workspace/memory") {
			t.Error("INTERNAL.md should be protected")
		}
	})
	t.Run("daily log", func(t *testing.T) {
		if !tools.IsProtectedMemoryPath("2024-01-15.md", "/workspace/memory") {
			t.Error("daily log file should be protected")
		}
	})
	t.Run("entity dir", func(t *testing.T) {
		if !tools.IsProtectedMemoryPath("alice.md", "/workspace/memory/ENTITIES") {
			t.Error("entity files should be protected")
		}
	})
	t.Run("summaries dir", func(t *testing.T) {
		if !tools.IsProtectedMemoryPath("2024-01-01.md", "/workspace/memory/summaries") {
			t.Error("summary files should be protected")
		}
	})
	t.Run("MEMORY backup", func(t *testing.T) {
		if !tools.IsProtectedMemoryPath("MEMORY_20240101_120000.md", "/workspace/memory") {
			t.Error("MEMORY backup should be protected")
		}
	})
	t.Run("regular file", func(t *testing.T) {
		if tools.IsProtectedMemoryPath("notes.txt", "/workspace/scripts") {
			t.Error("regular workspace file should NOT be protected")
		}
	})
	t.Run("readme", func(t *testing.T) {
		if tools.IsProtectedMemoryPath("README.md", "/workspace") {
			t.Error("README.md in workspace root should NOT be protected")
		}
	})
}

// ---------------------------------------------------------------------------
// Dynamic skills loading tests
// ---------------------------------------------------------------------------

func TestLoadSkills_RegistersDynamicScript(t *testing.T) {
	r, dir := newTestRegistry(t)

	// Create a simple shell skill
	skillsDir := filepath.Join(dir, "skills")
	_ = os.WriteFile(filepath.Join(skillsDir, "greet.sh"), []byte("#!/bin/sh\necho 'hello from skill'"), 0755)

	// Re-load skills
	r.LoadSkills()

	// The tool should now be registered
	defs := r.GetDefinitions()
	var found bool
	for _, d := range defs {
		if d.Function.Name == "greet" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'greet' skill to be registered after loadSkills()")
	}
}

func TestReloadSkills_Tool(t *testing.T) {
	r, _ := newTestRegistry(t)

	result := r.Execute(context.Background(), "reload_skills", nil)
	if result == nil || result.ForLLM == "" {
		t.Error("reload_skills should return a non-empty result")
	}
	if strings.Contains(strings.ToLower(result.ForLLM), "error") {
		t.Errorf("reload_skills returned error: %q", result.ForLLM)
	}
}
