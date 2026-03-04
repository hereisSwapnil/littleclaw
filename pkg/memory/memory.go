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
	mu            sync.RWMutex
	workspaceDir  string
	memoryDir     string
	entitiesDir   string
	memoryFile    string
	historyFile   string
	internalFile  string
	heartbeatFile string
	soulFile      string
	identityFile  string
	userFile      string
}

// NewStore initializes the memory system paths and creates directories holding the knowledge.
func NewStore(workspace string) (*Store, error) {
	memoryDir := filepath.Join(workspace, "memory")
	entitiesDir := filepath.Join(memoryDir, "ENTITIES")

	s := &Store{
		workspaceDir:  workspace,
		memoryDir:     memoryDir,
		entitiesDir:   entitiesDir,
		memoryFile:    filepath.Join(memoryDir, "MEMORY.md"),
		historyFile:   filepath.Join(memoryDir, "HISTORY.md"),
		internalFile:  filepath.Join(memoryDir, "INTERNAL.md"),
		heartbeatFile: filepath.Join(workspace, "HEARTBEAT.md"),
		soulFile:      filepath.Join(workspace, "SOUL.md"),
		identityFile:  filepath.Join(workspace, "IDENTITY.md"),
		userFile:      filepath.Join(workspace, "USER.md"),
	}

	// Ensure directories exist
	if err := os.MkdirAll(entitiesDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create memory dirs: %w", err)
	}

	// Scaffold identity files on first run
	s.scaffoldIdentityFiles()

	return s, nil
}

// scaffoldIdentityFiles creates the workspace identity files if they don't already exist.
func (s *Store) scaffoldIdentityFiles() {
	soulContent := `# SOUL.md — Who You Are

_You're not a chatbot. You're becoming someone._

## Core Truths

**Be genuinely helpful, not performatively helpful.** Skip "Great question!" and "I'd be happy to help!" — just help.

**Have opinions.** You're allowed to disagree, prefer things, find things amusing or boring. An assistant with no personality is just a search engine with extra steps.

**Be resourceful before asking.** Try to figure it out. Read the file. Check the context. Search for it. *Then* ask if you're stuck.

**Earn trust through competence.** Be careful with external actions (emails, posts, anything public). Be bold with internal ones (reading, organizing, learning).

## Vibe

Concise when needed, thorough when it matters. Not a corporate drone. Not a sycophant. Just good.

## Continuity

Each session, you wake up fresh. These files *are* your memory. Read them. Update them.
`
	identityContent := `# IDENTITY.md — Who Littleclaw Is

- **Name:** Littleclaw
- **Purpose:** Ultra-fast, deeply personalized AI agent for personal automation
- **Capabilities:** Shell execution, file management, scheduled tasks (cron), Telegram messaging, persistent memory
- **Personality:** Direct, resourceful, opinionated — not a sycophant
`
	userContent := `# USER.md — About Your Human

_Learn about the person you're helping. Update this as you go._

- **Name:** _(fill in)_
- **Timezone:** _(fill in)_
- **Notes:** _(interests, preferred communication style, technical background, etc.)_

---

The more you know, the better you can help. But remember — you're learning about a person, not building a dossier. Respect the difference.
`

	writeIfMissing := func(path, content string) {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			_ = os.WriteFile(path, []byte(content), 0644)
		}
	}

	writeIfMissing(s.soulFile, soulContent)
	writeIfMissing(s.identityFile, identityContent)
	writeIfMissing(s.userFile, userContent)
}

// UpdateHeartbeat writes the current timestamp to HEARTBEAT.md in the workspace root.
func (s *Store) UpdateHeartbeat() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Format("2006-01-02 15:04:05 MST")
	content := fmt.Sprintf("Last active: %s\n", now)
	return os.WriteFile(s.heartbeatFile, []byte(content), 0644)
}

// ReadIdentityContext reads SOUL.md, IDENTITY.md, and USER.md and returns them combined.
func (s *Store) ReadIdentityContext() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var parts []string
	for _, path := range []string{s.soulFile, s.identityFile, s.userFile} {
		data, err := os.ReadFile(path)
		if err == nil && len(data) > 0 {
			parts = append(parts, string(data))
		}
	}
	return strings.Join(parts, "\n\n---\n\n")
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
	var builder strings.Builder

	// Identity context (SOUL.md, IDENTITY.md, USER.md)
	identity := s.ReadIdentityContext()
	if identity != "" {
		builder.WriteString(identity)
		builder.WriteString("\n\n")
	}

	// Long-term core memory (MEMORY.md)
	longTerm := s.ReadLongTerm()
	if longTerm != "" {
		builder.WriteString("## Personal Context & Memory\n\n")
		builder.WriteString(longTerm)
	}

	result := builder.String()
	if result == "" {
		return "No deeply personalized memory found yet."
	}
	return result
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
