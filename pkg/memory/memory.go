package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Store represents the persistent, two-tier memory system.
type Store struct {
	mu           sync.RWMutex
	workspaceDir string
	memoryDir    string
	entitiesDir  string
	memoryFile   string
	historyFile  string
	internalFile string
}

// NewStore initializes the memory system paths and creates directories holding the knowledge.
func NewStore(workspace string) (*Store, error) {
	memoryDir := filepath.Join(workspace, "memory")
	entitiesDir := filepath.Join(memoryDir, "ENTITIES")

	s := &Store{
		workspaceDir: workspace,
		memoryDir:    memoryDir,
		entitiesDir:  entitiesDir,
		memoryFile:   filepath.Join(memoryDir, "MEMORY.md"),
		historyFile:  filepath.Join(memoryDir, "HISTORY.md"),
		internalFile: filepath.Join(memoryDir, "INTERNAL.md"),
	}

	// Ensure directories exist
	if err := os.MkdirAll(entitiesDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create memory dirs: %w", err)
	}

	return s, nil
}

// ReadLongTerm returns the current core facts and preferences from MEMORY.md.
func (s *Store) ReadLongTerm() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := os.ReadFile(s.memoryFile)
	if err != nil {
		return ""
	}
	return string(data)
}

// WriteLongTerm completely overwrites MEMORY.md with new consolidated facts.
func (s *Store) WriteLongTerm(content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return os.WriteFile(s.memoryFile, []byte(content), 0644)
}

// AppendHistory logs an interaction block to the chronological HISTORY.md file.
func (s *Store) AppendHistory(role, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Handle history rotation if file gets too large (e.g., > 1MB)
	if info, err := os.Stat(s.historyFile); err == nil && info.Size() > 1024*1024 {
		archiveName := fmt.Sprintf("HISTORY_ARCHIVE_%s.md", time.Now().Format("20060102_150405"))
		archivePath := filepath.Join(s.memoryDir, archiveName)
		_ = os.Rename(s.historyFile, archivePath)
	}

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	entry := fmt.Sprintf("[%s] %s: %s\n\n", timestamp, strings.ToUpper(role), content)
	
	f, err := os.OpenFile(s.historyFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(entry)
	return err
}

// AppendInternal logs background operations and reasoning blocks to INTERNAL.md.
func (s *Store) AppendInternal(role, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	entry := fmt.Sprintf("[%s] %s: %s\n\n", timestamp, strings.ToUpper(role), content)
	
	f, err := os.OpenFile(s.internalFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(entry)
	return err
}

// ReadRecentHistory returns the most recent portion of the HISTORY.md file (up to maxBytes).
func (s *Store) ReadRecentHistory(maxBytes int) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	info, err := os.Stat(s.historyFile)
	if err != nil {
		return ""
	}

	size := info.Size()
	if size == 0 {
		return ""
	}

	start := int64(0)
	if size > int64(maxBytes) {
		start = size - int64(maxBytes)
	}

	f, err := os.Open(s.historyFile)
	if err != nil {
		return ""
	}
	defer f.Close()

	if _, err := f.Seek(start, 0); err != nil {
		return ""
	}

	buf := make([]byte, size-start)
	if _, err := f.Read(buf); err != nil {
		return ""
	}

	str := string(buf)
	// If we didn't read from the very beginning, snap to the first full line to avoid cut-off words
	if start > 0 {
		idx := strings.Index(str, "\n")
		if idx >= 0 && idx < len(str)-1 {
			str = str[idx+1:]
		}
	}
	
	return strings.TrimSpace(str)
}

// ReadEntity reads specific deeply-contextualized knowledge about a person, project, or topic.
func (s *Store) ReadEntity(entityName string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// sanitize entity name for file system
	safeName := strings.ReplaceAll(entityName, " ", "_") + ".md"
	data, err := os.ReadFile(filepath.Join(s.entitiesDir, safeName))
	if err != nil {
		return ""
	}
	return string(data)
}

// WriteEntity creates or updates a deeply-contextualized knowledge record.
func (s *Store) WriteEntity(entityName, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	safeName := strings.ReplaceAll(entityName, " ", "_") + ".md"
	return os.WriteFile(filepath.Join(s.entitiesDir, safeName), []byte(content), 0644)
}

// BuildContext forms the complete context string to inject into the LLM system prompt.
func (s *Store) BuildContext() string {
	longTerm := s.ReadLongTerm()
	
	if longTerm == "" {
		return "No deeply personalized memory found yet."
	}
	
	return "## Personal Context & Memory\n\n" + longTerm
}

// ListEntities returns a list of all existing entity names (without the .md extension).
func (s *Store) ListEntities() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.entitiesDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read entities directory: %w", err)
	}

	var entities []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			name := strings.TrimSuffix(entry.Name(), ".md")
			// Convert back from snake_case to spaces if needed (best effort)
			name = strings.ReplaceAll(name, "_", " ")
			entities = append(entities, name)
		}
	}
	return entities, nil
}
