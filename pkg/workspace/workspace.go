package workspace

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// FolderType defines well-known workspace folder categories.
type FolderType string

const (
	FolderScripts FolderType = "scripts"
	FolderSkills  FolderType = "skills"
	FolderTools   FolderType = "tools"
	FolderMemory  FolderType = "memory"
	FolderCustom  FolderType = "custom"
)

// TrackedItem represents a script, skill, or tool entry in the tracker JSON.
type TrackedItem struct {
	Name        string     `json:"name"`
	File        string     `json:"file"`
	Description string     `json:"description"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	LastRunAt   *time.Time `json:"last_run_at,omitempty"`
	RunCount    int        `json:"run_count"`
	LastRunArgs string     `json:"last_run_args,omitempty"`
	LastRunOut  string     `json:"last_run_output,omitempty"`
	LastRunOK   bool       `json:"last_run_ok"`
	Tags        []string   `json:"tags,omitempty"`
	Notes       string     `json:"notes,omitempty"`
}

// Tracker holds all tracked items for a specific folder category.
type Tracker struct {
	Category  FolderType             `json:"category"`
	UpdatedAt time.Time              `json:"updated_at"`
	Items     map[string]TrackedItem `json:"items"` // keyed by item name
}

// FolderMeta stores metadata about a custom folder.
type FolderMeta struct {
	Name        string     `json:"name"`
	Path        string     `json:"path"`
	Description string     `json:"description"`
	Type        FolderType `json:"type"`
	CreatedAt   time.Time  `json:"created_at"`
}

// Index is the top-level workspace index file (~/.littleclaw/workspace/INDEX.json).
type Index struct {
	Version   string                `json:"version"`
	UpdatedAt time.Time             `json:"updated_at"`
	Folders   map[string]FolderMeta `json:"folders"` // keyed by folder name
}

// Manager owns the workspace root and coordinates all structure.
type Manager struct {
	mu           sync.RWMutex
	workspaceDir string
	indexFile    string
	index        *Index
}

// NewManager creates a Manager and initialises the canonical workspace structure.
func NewManager(workspaceDir string) (*Manager, error) {
	m := &Manager{
		workspaceDir: workspaceDir,
		indexFile:    filepath.Join(workspaceDir, "INDEX.json"),
	}

	if err := m.ensureStructure(); err != nil {
		return nil, fmt.Errorf("workspace init failed: %w", err)
	}
	return m, nil
}

// ensureStructure creates all canonical folders and initialises tracker files.
func (m *Manager) ensureStructure() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Load or create the index
	idx, err := m.loadIndex()
	if err != nil {
		idx = &Index{
			Version:   "1",
			UpdatedAt: time.Now(),
			Folders:   make(map[string]FolderMeta),
		}
	}
	m.index = idx

	// Canonical folders that always exist
	canonical := []struct {
		name string
		typ  FolderType
		desc string
	}{
		{"scripts", FolderScripts, "Shell and Python scripts for automation"},
		{"skills", FolderSkills, "Dynamic executable skills loaded as agent tools"},
		{"tools", FolderTools, "Utility programs and binary helpers"},
	}

	for _, c := range canonical {
		dir := filepath.Join(m.workspaceDir, c.name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("cannot create %s: %w", c.name, err)
		}

		// Register in index if not already there
		if _, exists := m.index.Folders[c.name]; !exists {
			m.index.Folders[c.name] = FolderMeta{
				Name:        c.name,
				Path:        c.name,
				Description: c.desc,
				Type:        c.typ,
				CreatedAt:   time.Now(),
			}
		}

		// Ensure tracker file exists
		if err := m.ensureTracker(c.name, c.typ); err != nil {
			return err
		}
	}

	return m.saveIndex()
}

// ensureTracker creates the tracker JSON for a folder if it does not exist.
func (m *Manager) ensureTracker(folderName string, typ FolderType) error {
	trackerPath := filepath.Join(m.workspaceDir, folderName, "tracker.json")
	if _, err := os.Stat(trackerPath); err == nil {
		return nil // already exists
	}

	t := &Tracker{
		Category:  typ,
		UpdatedAt: time.Now(),
		Items:     make(map[string]TrackedItem),
	}
	return m.writeTrackerFile(trackerPath, t)
}

// CreateFolder creates a new custom folder with a tracker and registers it in the index.
func (m *Manager) CreateFolder(name, description string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Sanitize name
	name = strings.ToLower(strings.ReplaceAll(name, " ", "_"))

	dir := filepath.Join(m.workspaceDir, name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("cannot create folder: %w", err)
	}

	// Register in index
	m.index.Folders[name] = FolderMeta{
		Name:        name,
		Path:        name,
		Description: description,
		Type:        FolderCustom,
		CreatedAt:   time.Now(),
	}
	m.index.UpdatedAt = time.Now()

	if err := m.saveIndex(); err != nil {
		return "", err
	}

	// Create tracker
	trackerPath := filepath.Join(dir, "tracker.json")
	t := &Tracker{
		Category:  FolderCustom,
		UpdatedAt: time.Now(),
		Items:     make(map[string]TrackedItem),
	}
	if err := m.writeTrackerFile(trackerPath, t); err != nil {
		return "", err
	}

	return dir, nil
}

// ListFolders returns all known folders from the index.
func (m *Manager) ListFolders() []FolderMeta {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.index == nil {
		return nil
	}
	result := make([]FolderMeta, 0, len(m.index.Folders))
	for _, f := range m.index.Folders {
		result = append(result, f)
	}
	return result
}

// TrackItem upserts a tracked item into the folder's tracker.json.
func (m *Manager) TrackItem(folderName string, item TrackedItem) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	trackerPath := filepath.Join(m.workspaceDir, folderName, "tracker.json")
	t, err := m.loadTrackerFile(trackerPath)
	if err != nil {
		t = &Tracker{
			Category:  FolderCustom,
			UpdatedAt: time.Now(),
			Items:     make(map[string]TrackedItem),
		}
	}

	existing, exists := t.Items[item.Name]
	if exists {
		// Preserve creation time and run stats if not explicitly supplied
		item.CreatedAt = existing.CreatedAt
		if item.RunCount == 0 {
			item.RunCount = existing.RunCount
		}
	} else {
		item.CreatedAt = time.Now()
	}
	item.UpdatedAt = time.Now()

	t.Items[item.Name] = item
	t.UpdatedAt = time.Now()
	return m.writeTrackerFile(trackerPath, t)
}

// RecordRun updates run statistics for a tracked item.
func (m *Manager) RecordRun(folderName, itemName, args, output string, ok bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	trackerPath := filepath.Join(m.workspaceDir, folderName, "tracker.json")
	t, err := m.loadTrackerFile(trackerPath)
	if err != nil {
		return fmt.Errorf("tracker not found for %s: %w", folderName, err)
	}

	item, exists := t.Items[itemName]
	if !exists {
		item = TrackedItem{
			Name:      itemName,
			CreatedAt: time.Now(),
		}
	}

	now := time.Now()
	item.LastRunAt = &now
	item.RunCount++
	item.LastRunArgs = args
	// Truncate output to avoid massive tracker files
	if len(output) > 1000 {
		output = output[:1000] + "...[truncated]"
	}
	item.LastRunOut = output
	item.LastRunOK = ok
	item.UpdatedAt = now

	t.Items[itemName] = item
	t.UpdatedAt = now
	return m.writeTrackerFile(trackerPath, t)
}

// ReadTracker returns the full tracker for a folder.
func (m *Manager) ReadTracker(folderName string) (*Tracker, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	trackerPath := filepath.Join(m.workspaceDir, folderName, "tracker.json")
	return m.loadTrackerFile(trackerPath)
}

// ResolvePath returns the absolute path for a relative workspace path,
// ensuring it doesn't escape the workspace root.
func (m *Manager) ResolvePath(rel string) (string, error) {
	clean := filepath.Clean(filepath.Join(m.workspaceDir, rel))
	if !strings.HasPrefix(clean, m.workspaceDir) {
		return "", fmt.Errorf("path escapes workspace: %s", rel)
	}
	return clean, nil
}

// WorkspaceDir returns the absolute workspace root.
func (m *Manager) WorkspaceDir() string {
	return m.workspaceDir
}

// --- internal helpers ---

func (m *Manager) loadIndex() (*Index, error) {
	data, err := os.ReadFile(m.indexFile)
	if err != nil {
		return nil, err
	}
	var idx Index
	if err := json.Unmarshal(data, &idx); err != nil {
		return nil, err
	}
	return &idx, nil
}

func (m *Manager) saveIndex() error {
	m.index.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(m.index, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.indexFile, data, 0644)
}

func (m *Manager) loadTrackerFile(path string) (*Tracker, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var t Tracker
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	// Guard against nil map from JSON null or missing "items" field.
	if t.Items == nil {
		t.Items = make(map[string]TrackedItem)
	}
	return &t, nil
}

func (m *Manager) writeTrackerFile(path string, t *Tracker) error {
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
