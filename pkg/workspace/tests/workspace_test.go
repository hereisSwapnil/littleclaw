package workspace_test

import (
	"littleclaw/pkg/workspace"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestManager creates a workspace workspace.Manager backed by a temp directory.
func newTestManager(t *testing.T) (*workspace.Manager, string) {
	t.Helper()
	dir := t.TempDir()
	m, err := workspace.NewManager(dir)
	if err != nil {
		t.Fatalf("workspace.NewManager() error = %v", err)
	}
	return m, dir
}

// ---------------------------------------------------------------------------
// workspace.NewManager / init tests
// ---------------------------------------------------------------------------

func TestNewManager_CreatesCanonicalFolders(t *testing.T) {
	_, dir := newTestManager(t)

	for _, folder := range []string{"scripts", "skills", "tools"} {
		info, err := os.Stat(filepath.Join(dir, folder))
		if err != nil {
			t.Errorf("expected canonical folder %q to be created: %v", folder, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("expected %q to be a directory", folder)
		}
	}
}

func TestNewManager_CreatesTrackerFiles(t *testing.T) {
	_, dir := newTestManager(t)

	for _, folder := range []string{"scripts", "skills", "tools"} {
		trackerPath := filepath.Join(dir, folder, "tracker.json")
		if _, err := os.Stat(trackerPath); err != nil {
			t.Errorf("expected tracker.json in %q: %v", folder, err)
		}
	}
}

func TestNewManager_CreatesIndexFile(t *testing.T) {
	_, dir := newTestManager(t)

	indexPath := filepath.Join(dir, "INDEX.json")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("INDEX.json not created: %v", err)
	}

	var idx workspace.Index
	if err := json.Unmarshal(data, &idx); err != nil {
		t.Fatalf("INDEX.json is invalid JSON: %v", err)
	}
	if idx.Version == "" {
		t.Error("INDEX.json version should not be empty")
	}
	if len(idx.Folders) == 0 {
		t.Error("INDEX.json should have canonical folders registered")
	}
}

func TestNewManager_IdempotentOnReopen(t *testing.T) {
	_, dir := newTestManager(t)

	// Opening again on the same directory should not fail or clobber existing files
	m2, err := workspace.NewManager(dir)
	if err != nil {
		t.Fatalf("second workspace.NewManager() failed: %v", err)
	}
	folders := m2.ListFolders()
	if len(folders) == 0 {
		t.Error("reopened manager should still see canonical folders")
	}
}

// ---------------------------------------------------------------------------
// ListFolders tests
// ---------------------------------------------------------------------------

func TestListFolders_ReturnsCanonicalFolders(t *testing.T) {
	m, _ := newTestManager(t)

	folders := m.ListFolders()
	names := make(map[string]bool)
	for _, f := range folders {
		names[f.Name] = true
	}

	for _, expected := range []string{"scripts", "skills", "tools"} {
		if !names[expected] {
			t.Errorf("expected canonical folder %q in ListFolders(), got %v", expected, names)
		}
	}
}

func TestListFolders_FolderHasCorrectType(t *testing.T) {
	m, _ := newTestManager(t)

	for _, f := range m.ListFolders() {
		switch f.Name {
		case "scripts":
			if f.Type != workspace.FolderScripts {
				t.Errorf("scripts folder type = %q, want %q", f.Type, workspace.FolderScripts)
			}
		case "skills":
			if f.Type != workspace.FolderSkills {
				t.Errorf("skills folder type = %q, want %q", f.Type, workspace.FolderSkills)
			}
		case "tools":
			if f.Type != workspace.FolderTools {
				t.Errorf("tools folder type = %q, want %q", f.Type, workspace.FolderTools)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// CreateFolder tests
// ---------------------------------------------------------------------------

func TestCreateFolder_CreatesDirectory(t *testing.T) {
	m, dir := newTestManager(t)

	folderPath, err := m.CreateFolder("my notes", "Personal notes folder")
	if err != nil {
		t.Fatalf("CreateFolder() error = %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "my_notes"))
	if err != nil {
		t.Fatalf("CreateFolder() did not create directory: %v", err)
	}
	if !info.IsDir() {
		t.Error("Created path is not a directory")
	}
	if !strings.Contains(folderPath, "my_notes") {
		t.Errorf("returned path %q should contain 'my_notes'", folderPath)
	}
}

func TestCreateFolder_SanitizesName(t *testing.T) {
	m, dir := newTestManager(t)

	_, err := m.CreateFolder("Hello World", "desc")
	if err != nil {
		t.Fatalf("CreateFolder() error = %v", err)
	}

	// Spaces → underscores, lowercase
	if _, err := os.Stat(filepath.Join(dir, "hello_world")); err != nil {
		t.Error("CreateFolder should sanitize name to lowercase_underscore")
	}
}

func TestCreateFolder_RegistersInIndex(t *testing.T) {
	m, _ := newTestManager(t)

	_, err := m.CreateFolder("my_project", "A test project")
	if err != nil {
		t.Fatalf("CreateFolder() error = %v", err)
	}

	folders := m.ListFolders()
	var found bool
	for _, f := range folders {
		if f.Name == "my_project" {
			found = true
			if f.Type != workspace.FolderCustom {
				t.Errorf("custom folder type = %q, want %q", f.Type, workspace.FolderCustom)
			}
			if f.Description != "A test project" {
				t.Errorf("folder description = %q, want %q", f.Description, "A test project")
			}
			break
		}
	}
	if !found {
		t.Error("CreateFolder did not register folder in index")
	}
}

func TestCreateFolder_CreatesTracker(t *testing.T) {
	m, dir := newTestManager(t)

	_, _ = m.CreateFolder("mydata", "")
	trackerPath := filepath.Join(dir, "mydata", "tracker.json")
	if _, err := os.Stat(trackerPath); err != nil {
		t.Errorf("CreateFolder should create tracker.json: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TrackItem tests
// ---------------------------------------------------------------------------

func TestTrackItem_AddsItem(t *testing.T) {
	m, _ := newTestManager(t)

	item := workspace.TrackedItem{
		Name:        "backup.sh",
		File:        "backup.sh",
		Description: "Daily backup script",
	}
	if err := m.TrackItem("scripts", item); err != nil {
		t.Fatalf("TrackItem() error = %v", err)
	}

	tracker, err := m.ReadTracker("scripts")
	if err != nil {
		t.Fatalf("ReadTracker() error = %v", err)
	}

	got, exists := tracker.Items["backup.sh"]
	if !exists {
		t.Fatal("tracked item not found in tracker")
	}
	if got.Description != "Daily backup script" {
		t.Errorf("item description = %q, want %q", got.Description, "Daily backup script")
	}
}

func TestTrackItem_UpdatesExistingItem(t *testing.T) {
	m, _ := newTestManager(t)

	item := workspace.TrackedItem{Name: "deploy.sh", File: "deploy.sh", Description: "v1"}
	_ = m.TrackItem("scripts", item)

	createdAt := time.Now()
	item.Description = "v2"
	_ = m.TrackItem("scripts", item)

	tracker, _ := m.ReadTracker("scripts")
	got := tracker.Items["deploy.sh"]
	if got.Description != "v2" {
		t.Errorf("expected updated description 'v2', got %q", got.Description)
	}
	// CreatedAt should be preserved from original insertion
	if got.CreatedAt.After(createdAt.Add(time.Second)) {
		t.Error("CreatedAt should be preserved on update, not reset")
	}
}

func TestTrackItem_PreservesRunCount(t *testing.T) {
	m, _ := newTestManager(t)

	// Manually add an item with RunCount=5 then upsert without override
	item := workspace.TrackedItem{Name: "run.sh", File: "run.sh", RunCount: 5}
	_ = m.TrackItem("scripts", item)

	// Update with RunCount=0 — should preserve existing count
	item2 := workspace.TrackedItem{Name: "run.sh", File: "run.sh", Description: "updated", RunCount: 0}
	_ = m.TrackItem("scripts", item2)

	tracker, _ := m.ReadTracker("scripts")
	got := tracker.Items["run.sh"]
	if got.RunCount != 5 {
		t.Errorf("RunCount should be preserved when update passes 0, got %d", got.RunCount)
	}
}

func TestTrackItem_SetsUpdatedAt(t *testing.T) {
	m, _ := newTestManager(t)
	before := time.Now()

	item := workspace.TrackedItem{Name: "ts.sh", File: "ts.sh"}
	_ = m.TrackItem("scripts", item)

	tracker, _ := m.ReadTracker("scripts")
	got := tracker.Items["ts.sh"]
	if got.UpdatedAt.Before(before) {
		t.Errorf("UpdatedAt should be set to now, got %v", got.UpdatedAt)
	}
}

// ---------------------------------------------------------------------------
// RecordRun tests
// ---------------------------------------------------------------------------

func TestRecordRun_IncrementRunCount(t *testing.T) {
	m, _ := newTestManager(t)

	_ = m.TrackItem("scripts", workspace.TrackedItem{Name: "myscript.sh", File: "myscript.sh"})
	_ = m.RecordRun("scripts", "myscript.sh", "--arg1", "output", true)
	_ = m.RecordRun("scripts", "myscript.sh", "--arg2", "output2", true)

	tracker, _ := m.ReadTracker("scripts")
	item := tracker.Items["myscript.sh"]
	if item.RunCount != 2 {
		t.Errorf("RunCount = %d, want 2", item.RunCount)
	}
}

func TestRecordRun_RecordsLastRunAt(t *testing.T) {
	m, _ := newTestManager(t)
	before := time.Now()

	_ = m.TrackItem("scripts", workspace.TrackedItem{Name: "ts.sh", File: "ts.sh"})
	_ = m.RecordRun("scripts", "ts.sh", "", "ok", true)

	tracker, _ := m.ReadTracker("scripts")
	item := tracker.Items["ts.sh"]
	if item.LastRunAt == nil || item.LastRunAt.Before(before) {
		t.Error("LastRunAt should be set after RecordRun")
	}
}

func TestRecordRun_TruncatesLongOutput(t *testing.T) {
	m, _ := newTestManager(t)
	_ = m.TrackItem("scripts", workspace.TrackedItem{Name: "big.sh", File: "big.sh"})

	bigOutput := strings.Repeat("x", 2000)
	_ = m.RecordRun("scripts", "big.sh", "", bigOutput, true)

	tracker, _ := m.ReadTracker("scripts")
	item := tracker.Items["big.sh"]
	if len(item.LastRunOut) > 1100 {
		t.Errorf("RecordRun should truncate output, got %d chars", len(item.LastRunOut))
	}
	if !strings.Contains(item.LastRunOut, "truncated") {
		t.Error("truncated output should end with '[truncated]'")
	}
}

func TestRecordRun_RecordsFailure(t *testing.T) {
	m, _ := newTestManager(t)
	_ = m.TrackItem("scripts", workspace.TrackedItem{Name: "fail.sh", File: "fail.sh"})
	_ = m.RecordRun("scripts", "fail.sh", "", "error output", false)

	tracker, _ := m.ReadTracker("scripts")
	item := tracker.Items["fail.sh"]
	if item.LastRunOK {
		t.Error("LastRunOK should be false for a failed run")
	}
}

func TestRecordRun_CreatesItemIfNotTracked(t *testing.T) {
	m, _ := newTestManager(t)

	// Record run for an item that wasn't explicitly tracked
	err := m.RecordRun("scripts", "untracked.sh", "", "output", true)
	if err != nil {
		t.Fatalf("RecordRun() error = %v", err)
	}

	tracker, _ := m.ReadTracker("scripts")
	if _, exists := tracker.Items["untracked.sh"]; !exists {
		t.Error("RecordRun should create item in tracker if not already present")
	}
}

// ---------------------------------------------------------------------------
// ReadTracker tests
// ---------------------------------------------------------------------------

func TestReadTracker_ReturnsCorrectCategory(t *testing.T) {
	m, _ := newTestManager(t)

	tracker, err := m.ReadTracker("scripts")
	if err != nil {
		t.Fatalf("ReadTracker() error = %v", err)
	}
	if tracker.Category != workspace.FolderScripts {
		t.Errorf("tracker category = %q, want %q", tracker.Category, workspace.FolderScripts)
	}
}

func TestReadTracker_ErrorForMissingFolder(t *testing.T) {
	m, _ := newTestManager(t)

	_, err := m.ReadTracker("nonexistent_folder")
	if err == nil {
		t.Error("ReadTracker should return error for missing folder")
	}
}

func TestReadTracker_EmptyItemsNotNil(t *testing.T) {
	m, _ := newTestManager(t)

	tracker, err := m.ReadTracker("scripts")
	if err != nil {
		t.Fatalf("ReadTracker() error = %v", err)
	}
	if tracker.Items == nil {
		t.Error("ReadTracker should return non-nil Items map for empty tracker")
	}
}

// ---------------------------------------------------------------------------
// ResolvePath tests
// ---------------------------------------------------------------------------

func TestResolvePath_ValidRelPath(t *testing.T) {
	m, dir := newTestManager(t)

	resolved, err := m.ResolvePath("scripts/foo.sh")
	if err != nil {
		t.Fatalf("ResolvePath() error = %v", err)
	}
	expected := filepath.Join(dir, "scripts", "foo.sh")
	if resolved != expected {
		t.Errorf("ResolvePath() = %q, want %q", resolved, expected)
	}
}

func TestResolvePath_EscapeBlocked(t *testing.T) {
	m, _ := newTestManager(t)

	_, err := m.ResolvePath("../../etc/passwd")
	if err == nil {
		t.Error("ResolvePath should block path escape attempts")
	}
	if !strings.Contains(err.Error(), "escapes") {
		t.Errorf("error should say 'escapes', got: %v", err)
	}
}

func TestResolvePath_RootIsAllowed(t *testing.T) {
	m, dir := newTestManager(t)

	resolved, err := m.ResolvePath(".")
	if err != nil {
		t.Fatalf("ResolvePath('.') error = %v", err)
	}
	if resolved != dir {
		t.Errorf("ResolvePath('.') = %q, want %q", resolved, dir)
	}
}

// ---------------------------------------------------------------------------
// WorkspaceDir test
// ---------------------------------------------------------------------------

func TestWorkspaceDir_ReturnsRootDir(t *testing.T) {
	m, dir := newTestManager(t)

	if m.WorkspaceDir() != dir {
		t.Errorf("WorkspaceDir() = %q, want %q", m.WorkspaceDir(), dir)
	}
}
