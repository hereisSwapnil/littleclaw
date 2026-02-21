package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Store represents the persistent, two-tier memory system.
type Store struct {
	workspaceDir string
	memoryDir    string
	entitiesDir  string
	memoryFile   string
	historyFile  string
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
	}

	// Ensure directories exist
	if err := os.MkdirAll(entitiesDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create memory dirs: %w", err)
	}

	return s, nil
}

// ReadLongTerm returns the current core facts and preferences from MEMORY.md.
func (s *Store) ReadLongTerm() string {
	data, err := os.ReadFile(s.memoryFile)
	if err != nil {
		return ""
	}
	return string(data)
}

// WriteLongTerm completely overwrites MEMORY.md with new consolidated facts.
func (s *Store) WriteLongTerm(content string) error {
	return os.WriteFile(s.memoryFile, []byte(content), 0644)
}

// AppendHistory logs an interaction block to the chronological HISTORY.md file.
func (s *Store) AppendHistory(role, content string) error {
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

// ReadEntity reads specific deeply-contextualized knowledge about a person, project, or topic.
func (s *Store) ReadEntity(entityName string) string {
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
