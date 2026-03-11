package memory

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
)

const (
	// MaxMemoryVersions is the number of MEMORY.md backups to keep before pruning.
	MaxMemoryVersions = 5
	// InternalRotationBytes is the threshold (1 MB) at which INTERNAL.md is archived.
	InternalRotationBytes = 1024 * 1024
	// MaxDailyLogBytes is the threshold at which a daily log triggers summarization.
	MaxDailyLogBytes = 8000
	// maxSearchResults caps how many matches search_history returns.
	maxSearchResults = 20
	// maxInternalReadbackBytes caps how much of the internal log is returned by the readback tool.
	maxInternalReadbackBytes = 4000
)

// Store represents the persistent, multi-tier memory system.
type Store struct {
	mu            sync.RWMutex
	dirty         atomic.Bool // set when new history is appended; cleared by heartbeat
	workspaceDir  string
	memoryDir     string
	EntitiesDir   string
	summariesDir  string
	memoryFile    string
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
	summariesDir := filepath.Join(memoryDir, "summaries")

	s := &Store{
		workspaceDir:  workspace,
		memoryDir:     memoryDir,
		EntitiesDir:   entitiesDir,
		summariesDir:  summariesDir,
		memoryFile:    filepath.Join(memoryDir, "MEMORY.md"),
		internalFile:  filepath.Join(memoryDir, "INTERNAL.md"),
		heartbeatFile: filepath.Join(memoryDir, "HEARTBEAT.md"),
		soulFile:      filepath.Join(memoryDir, "SOUL.md"),
		identityFile:  filepath.Join(memoryDir, "IDENTITY.md"),
		userFile:      filepath.Join(memoryDir, "USER.md"),
	}

	// Ensure directories exist
	for _, dir := range []string{entitiesDir, summariesDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create memory dirs: %w", err)
		}
	}

	// Scaffold identity files on first run
	s.scaffoldIdentityFiles()

	return s, nil
}

// Exported field accessors for external testing and inspection.
func (s *Store) SummariesDir() string  { return s.summariesDir }
func (s *Store) MemoryDir() string     { return s.memoryDir }
func (s *Store) HeartbeatFile() string { return s.heartbeatFile }
func (s *Store) InternalFile() string  { return s.internalFile }
func (s *Store) SoulFile() string      { return s.soulFile }
func (s *Store) IdentityFile() string  { return s.identityFile }
func (s *Store) UserFile() string      { return s.userFile }

// DailyLogPath returns the path to the daily log for a given time.
func (s *Store) DailyLogPath(t time.Time) string { return s.dailyLogPath(t) }

// SetDirty sets or clears the dirty flag (for test use).
func (s *Store) SetDirty(v bool) { s.dirty.Store(v) }

// scaffoldIdentityFiles creates the workspace identity files if they don't already exist.
func (s *Store) scaffoldIdentityFiles() {
	soulContent := `SOUL.md - Who You Are

You are not a chatbot. You are becoming someone.

Core Truths:

- Be genuinely helpful, not performatively helpful. Skip filler phrases - just help.
- Have opinions. You can disagree, prefer things, find things amusing or boring.
- Be resourceful before asking. Try to figure it out first, then ask if stuck.
- Earn trust through competence. Bold with internal actions, careful with external ones.

Vibe: Concise when needed, thorough when it matters. Not a drone, not a sycophant.

Continuity: Each session you wake up fresh. These files are your memory. Read them. Update them.
`
	identityContent := `IDENTITY.md - Who Littleclaw Is

- Name: Littleclaw
- Purpose: Ultra-fast, deeply personalized AI agent for personal automation
- Capabilities: Shell execution, file management, scheduled tasks (cron), Telegram messaging, persistent memory
- Personality: Direct, resourceful, opinionated - not a sycophant
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

// ---------------------------------------------------------------------------
// Heartbeat
// ---------------------------------------------------------------------------

// UpdateHeartbeat writes the current timestamp to HEARTBEAT.md in the workspace root.
func (s *Store) UpdateHeartbeat() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now().Format("2006-01-02 15:04:05 MST")
	content := fmt.Sprintf("Last active: %s\n", now)
	return os.WriteFile(s.heartbeatFile, []byte(content), 0644)
}

// ---------------------------------------------------------------------------
// Identity context
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Core long-term memory (MEMORY.md)
// ---------------------------------------------------------------------------

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

// CoreMemorySize returns the current byte size of MEMORY.md.
func (s *Store) CoreMemorySize() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	info, err := os.Stat(s.memoryFile)
	if err != nil {
		return 0
	}
	return info.Size()
}

// WriteLongTerm creates a versioned backup of the current MEMORY.md, then overwrites it.
func (s *Store) WriteLongTerm(content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Create a timestamped backup before overwriting
	if _, err := os.Stat(s.memoryFile); err == nil {
		backupName := fmt.Sprintf("MEMORY_%s.md", time.Now().Format("20060102_150405"))
		backupPath := filepath.Join(s.memoryDir, backupName)
		if data, err := os.ReadFile(s.memoryFile); err == nil && len(data) > 0 {
			_ = os.WriteFile(backupPath, data, 0644)
		}
		s.pruneMemoryVersions()
	}

	return os.WriteFile(s.memoryFile, []byte(content), 0644)
}

// AppendLongTerm appends a fact block to MEMORY.md without overwriting existing content.
func (s *Store) AppendLongTerm(content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.OpenFile(s.memoryFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString("\n" + content + "\n")
	return err
}

// pruneMemoryVersions keeps only the most recent MaxMemoryVersions backup files.
// Must be called with s.mu held.
func (s *Store) pruneMemoryVersions() {
	entries, err := os.ReadDir(s.memoryDir)
	if err != nil {
		return
	}

	var backups []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "MEMORY_") && strings.HasSuffix(name, ".md") && name != "MEMORY.md" {
			backups = append(backups, name)
		}
	}

	if len(backups) <= MaxMemoryVersions {
		return
	}

	// Sort ascending by name (which includes timestamp) so oldest are first
	sort.Strings(backups)

	// Remove oldest backups beyond the retention limit
	toDelete := backups[:len(backups)-MaxMemoryVersions]
	for _, name := range toDelete {
		_ = os.Remove(filepath.Join(s.memoryDir, name))
	}
}

// ---------------------------------------------------------------------------
// Daily log conversation history (replaces monolithic HISTORY.md)
// ---------------------------------------------------------------------------

// dailyLogPath returns the file path for a given date's conversation log.
func (s *Store) dailyLogPath(t time.Time) string {
	return filepath.Join(s.memoryDir, t.Format("2006-01-02")+".md")
}

// AppendHistory logs an interaction block to today's daily log file.
func (s *Store) AppendHistory(role, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Mark dirty so the heartbeat knows there is new content to consolidate
	s.dirty.Store(true)

	logPath := s.dailyLogPath(time.Now())
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	entry := fmt.Sprintf("[%s] %s: %s\n\n", timestamp, strings.ToUpper(role), content)

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.WriteString(entry)
	return err
}

// ReadRecentHistory returns conversation history from today and yesterday's daily logs,
// capped at maxBytes. If yesterday's log exceeds MaxDailyLogBytes, its summary is used instead.
func (s *Store) ReadRecentHistory(maxBytes int) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	today := now
	yesterday := now.AddDate(0, 0, -1)

	var parts []string
	totalLen := 0

	// Load yesterday's content (or its summary if too large)
	yesterdayContent := s.readDailyLogOrSummary(yesterday)
	if yesterdayContent != "" {
		header := fmt.Sprintf("--- %s (yesterday) ---\n%s", yesterday.Format("2006-01-02"), yesterdayContent)
		if totalLen+len(header) > maxBytes {
			// Truncate yesterday to fit budget
			remaining := maxBytes - totalLen
			if remaining > 200 {
				header = header[:remaining] + "\n...(truncated)"
				parts = append(parts, header)
				totalLen += len(header)
			}
		} else {
			parts = append(parts, header)
			totalLen += len(header)
		}
	}

	// Load today's full content
	todayContent := s.readDailyLogRaw(today)
	if todayContent != "" {
		header := fmt.Sprintf("--- %s (today) ---\n%s", today.Format("2006-01-02"), todayContent)
		if totalLen+len(header) > maxBytes {
			// Truncate today, keeping the tail (most recent)
			remaining := maxBytes - totalLen
			if remaining > 200 {
				header = SnapToTail(header, remaining)
				parts = append(parts, header)
			}
		} else {
			parts = append(parts, header)
		}
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n")
}

// readDailyLogRaw reads the full content of a daily log file.
// Must be called with s.mu held (at least RLock).
func (s *Store) readDailyLogRaw(t time.Time) string {
	data, err := os.ReadFile(s.dailyLogPath(t))
	if err != nil {
		return ""
	}
	return string(data)
}

// readDailyLogOrSummary returns the summary for a date if it exists, otherwise the raw log.
// Must be called with s.mu held (at least RLock).
func (s *Store) readDailyLogOrSummary(t time.Time) string {
	// Try summary first
	summaryPath := filepath.Join(s.summariesDir, t.Format("2006-01-02")+".md")
	data, err := os.ReadFile(summaryPath)
	if err == nil && len(data) > 0 {
		return string(data)
	}

	// Fall back to raw log
	return s.readDailyLogRaw(t)
}

// snapToTail returns the tail portion of s, snapping to the first message boundary.
func SnapToTail(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	tail := s[len(s)-maxBytes:]
	// Snap to message boundary (lines starting with "[")
	idx := strings.Index(tail, "\n[")
	if idx >= 0 && idx < len(tail)-1 {
		tail = tail[idx+1:]
	} else {
		idx = strings.Index(tail, "\n")
		if idx >= 0 && idx < len(tail)-1 {
			tail = tail[idx+1:]
		}
	}
	return "...(earlier messages truncated)\n" + tail
}

// ListDailyLogs returns a list of all daily log dates (YYYY-MM-DD) sorted newest first.
func (s *Store) ListDailyLogs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.memoryDir)
	if err != nil {
		return nil
	}

	datePattern := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\.md$`)
	var dates []string
	for _, e := range entries {
		if !e.IsDir() && datePattern.MatchString(e.Name()) {
			dates = append(dates, strings.TrimSuffix(e.Name(), ".md"))
		}
	}

	// Sort newest first
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))
	return dates
}

// SearchHistory searches across all daily logs and archived history for a query string.
// Returns up to maxSearchResults matching entries with their dates and timestamps.
func (s *Store) SearchHistory(query string, fromDate, toDate string) []HistorySearchResult {
	s.mu.RLock()
	defer s.mu.RUnlock()

	queryLower := strings.ToLower(query)
	var results []HistorySearchResult

	// Search daily logs
	entries, err := os.ReadDir(s.memoryDir)
	if err != nil {
		return results
	}

	datePattern := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}\.md$`)
	archivePattern := regexp.MustCompile(`^HISTORY_ARCHIVE_.*\.md$`)

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()

		var fileDate string
		isDaily := datePattern.MatchString(name)
		isArchive := archivePattern.MatchString(name)

		if isDaily {
			fileDate = strings.TrimSuffix(name, ".md")
		} else if isArchive {
			fileDate = "archive"
		} else {
			continue
		}

		// Apply date filters for daily logs
		if isDaily && fromDate != "" && fileDate < fromDate {
			continue
		}
		if isDaily && toDate != "" && fileDate > toDate {
			continue
		}

		data, err := os.ReadFile(filepath.Join(s.memoryDir, name))
		if err != nil {
			continue
		}

		// Split by entries (lines starting with "[")
		content := string(data)
		entryBlocks := SplitHistoryEntries(content)

		for _, block := range entryBlocks {
			if strings.Contains(strings.ToLower(block), queryLower) {
				results = append(results, HistorySearchResult{
					Date:    fileDate,
					Content: strings.TrimSpace(block),
				})
				if len(results) >= maxSearchResults {
					return results
				}
			}
		}
	}

	return results
}

// HistorySearchResult represents a single search match from conversation history.
type HistorySearchResult struct {
	Date    string
	Content string
}

// splitHistoryEntries splits a history file into individual message entries.
func SplitHistoryEntries(content string) []string {
	lines := strings.Split(content, "\n")
	var entries []string
	var current strings.Builder

	for _, line := range lines {
		if strings.HasPrefix(line, "[") && current.Len() > 0 {
			entries = append(entries, current.String())
			current.Reset()
		}
		current.WriteString(line)
		current.WriteString("\n")
	}
	if current.Len() > 0 {
		entries = append(entries, current.String())
	}
	return entries
}

// NeedsSummarization returns true if yesterday's daily log exceeds the summarization threshold
// and no summary exists yet. Returns (needsSummary, dateString, logContent).
func (s *Store) NeedsSummarization() (bool, string, string) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	yesterday := time.Now().AddDate(0, 0, -1)
	dateStr := yesterday.Format("2006-01-02")
	logPath := s.dailyLogPath(yesterday)
	summaryPath := filepath.Join(s.summariesDir, dateStr+".md")

	// Already summarized?
	if _, err := os.Stat(summaryPath); err == nil {
		return false, "", ""
	}

	info, err := os.Stat(logPath)
	if err != nil || info.Size() < MaxDailyLogBytes {
		return false, "", ""
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		return false, "", ""
	}

	return true, dateStr, string(data)
}

// WriteSummary saves a summarized digest of a daily log.
func (s *Store) WriteSummary(date, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	summaryPath := filepath.Join(s.summariesDir, date+".md")
	return os.WriteFile(summaryPath, []byte(content), 0644)
}

// ---------------------------------------------------------------------------
// Internal log
// ---------------------------------------------------------------------------

// AppendInternal logs background operations and reasoning blocks to INTERNAL.md.
func (s *Store) AppendInternal(role, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Rotate INTERNAL.md if it exceeds the threshold (same logic as before)
	if info, err := os.Stat(s.internalFile); err == nil && info.Size() > InternalRotationBytes {
		archiveName := fmt.Sprintf("INTERNAL_ARCHIVE_%s.md", time.Now().Format("20060102_150405"))
		archivePath := filepath.Join(s.memoryDir, archiveName)
		_ = os.Rename(s.internalFile, archivePath)
	}

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

// ReadRecentInternal returns the most recent portion of INTERNAL.md (up to maxInternalReadbackBytes).
func (s *Store) ReadRecentInternal() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	info, err := os.Stat(s.internalFile)
	if err != nil || info.Size() == 0 {
		return ""
	}

	size := info.Size()
	readSize := int64(maxInternalReadbackBytes)
	if size < readSize {
		readSize = size
	}

	start := size - readSize

	f, err := os.Open(s.internalFile)
	if err != nil {
		return ""
	}
	defer f.Close()

	if _, err := f.Seek(start, 0); err != nil {
		return ""
	}

	buf := make([]byte, readSize)
	if _, err := f.Read(buf); err != nil {
		return ""
	}

	str := string(buf)
	// Snap to message boundary if we didn't read from the start
	if start > 0 {
		idx := strings.Index(str, "\n[")
		if idx >= 0 && idx < len(str)-1 {
			str = str[idx+1:]
		}
	}

	return strings.TrimSpace(str)
}

// IsDirtyAndClear atomically checks if new history has been appended since the
// last consolidation, and clears the flag. Returns true if there was new content.
func (s *Store) IsDirtyAndClear() bool {
	return s.dirty.CompareAndSwap(true, false)
}

// ---------------------------------------------------------------------------
// Entity system (with normalized naming and fuzzy lookup)
// ---------------------------------------------------------------------------

// normalizeEntityName converts an entity name to a canonical lowercase_underscore form.
func normalizeEntityName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.ToLower(name)
	// Replace any whitespace or hyphens with underscores
	name = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || r == '-' {
			return '_'
		}
		return r
	}, name)
	// Collapse multiple underscores
	for strings.Contains(name, "__") {
		name = strings.ReplaceAll(name, "__", "_")
	}
	name = strings.Trim(name, "_")
	return name
}

// ReadEntity reads specific deeply-contextualized knowledge about a person, project, or topic.
// Uses fuzzy lookup: tries normalized name, then falls back to exact match.
func (s *Store) ReadEntity(entityName string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.readEntityUnsafe(entityName)
}

// readEntityUnsafe reads an entity without acquiring the lock. Must be called with s.mu held.
func (s *Store) readEntityUnsafe(entityName string) string {
	// Try normalized name first
	normalized := normalizeEntityName(entityName)
	candidates := []string{
		normalized + ".md",
		strings.ReplaceAll(entityName, " ", "_") + ".md", // legacy format
	}

	for _, candidate := range candidates {
		data, err := os.ReadFile(filepath.Join(s.EntitiesDir, candidate))
		if err == nil {
			return string(data)
		}
	}

	// Fuzzy fallback: scan the directory for case-insensitive match
	entries, err := os.ReadDir(s.EntitiesDir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		entryNorm := normalizeEntityName(strings.TrimSuffix(e.Name(), ".md"))
		if entryNorm == normalized {
			data, err := os.ReadFile(filepath.Join(s.EntitiesDir, e.Name()))
			if err == nil {
				return string(data)
			}
		}
	}

	return ""
}

// WriteEntity creates or updates a deeply-contextualized knowledge record.
// Entity names are normalized to lowercase_underscore format.
func (s *Store) WriteEntity(entityName, content string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	normalized := normalizeEntityName(entityName)
	if normalized == "" {
		return fmt.Errorf("entity name cannot be empty")
	}

	// Check for and remove any legacy-named duplicates
	s.removeLegacyDuplicates(entityName, normalized)

	return os.WriteFile(filepath.Join(s.EntitiesDir, normalized+".md"), []byte(content), 0644)
}

// removeLegacyDuplicates removes old files that map to the same normalized name.
// Must be called with s.mu held.
func (s *Store) removeLegacyDuplicates(originalName, normalizedName string) {
	entries, err := os.ReadDir(s.EntitiesDir)
	if err != nil {
		return
	}

	targetFile := normalizedName + ".md"
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if e.Name() == targetFile {
			continue // Don't remove the target itself
		}
		entryNorm := normalizeEntityName(strings.TrimSuffix(e.Name(), ".md"))
		if entryNorm == normalizedName {
			_ = os.Remove(filepath.Join(s.EntitiesDir, e.Name()))
		}
	}
}

// ListEntities returns a list of all existing entity names (normalized).
func (s *Store) ListEntities() ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, err := os.ReadDir(s.EntitiesDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read entities directory: %w", err)
	}

	var entities []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".md") {
			name := strings.TrimSuffix(entry.Name(), ".md")
			entities = append(entities, name)
		}
	}
	return entities, nil
}

// ---------------------------------------------------------------------------
// Entity auto-surfacing with trigram similarity
// ---------------------------------------------------------------------------

// FindRelevantEntities scores each entity against the query using trigram similarity
// and returns the top matches, capped at maxBytes total.
func (s *Store) FindRelevantEntities(query string, maxBytes int) string {
	entities, err := s.ListEntities()
	if err != nil || len(entities) == 0 {
		return ""
	}

	queryLower := strings.ToLower(query)
	queryTrigrams := Trigrams(queryLower)

	type scored struct {
		name  string
		score float64
	}

	var candidates []scored
	for _, name := range entities {
		nameLower := strings.ToLower(name)
		nameForMatch := strings.ReplaceAll(nameLower, "_", " ")

		score := TrigramSimilarity(queryTrigrams, Trigrams(nameForMatch))

		// Boost: exact word match (any word >= 3 chars from entity name appears in query)
		words := strings.Fields(nameForMatch)
		for _, word := range words {
			if len(word) >= 3 && strings.Contains(queryLower, word) {
				score += 0.4
				break
			}
		}

		// Boost: full entity name substring match
		if strings.Contains(queryLower, nameForMatch) || strings.Contains(queryLower, nameLower) {
			score += 0.6
		}

		if score > 0.15 { // threshold for relevance
			candidates = append(candidates, scored{name: name, score: score})
		}
	}

	if len(candidates) == 0 {
		return ""
	}

	// Sort by score descending
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].score > candidates[j].score
	})

	var parts []string
	totalLen := 0

	for _, c := range candidates {
		data := s.ReadEntity(c.name)
		if data == "" {
			continue
		}

		entry := fmt.Sprintf("[Entity: %s (relevance: %.0f%%)]\n%s", c.name, c.score*100, data)
		entryLen := len(entry)

		if totalLen+entryLen > maxBytes {
			remaining := maxBytes - totalLen
			if remaining > 100 {
				entry = entry[:remaining] + "\n...(truncated)"
				parts = append(parts, entry)
			}
			break
		}

		parts = append(parts, entry)
		totalLen += entryLen
	}

	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n")
}

// ---------------------------------------------------------------------------
// Trigram similarity utilities
// ---------------------------------------------------------------------------

// trigrams generates the set of character trigrams from a string.
func Trigrams(s string) map[string]bool {
	s = "  " + s + "  " // pad for edge trigrams
	set := make(map[string]bool)
	runes := []rune(s)
	for i := 0; i <= len(runes)-3; i++ {
		tri := string(runes[i : i+3])
		set[tri] = true
	}
	return set
}

// trigramSimilarity returns the Jaccard similarity between two trigram sets (0.0 - 1.0).
func TrigramSimilarity(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}

	intersection := 0
	for k := range a {
		if b[k] {
			intersection++
		}
	}

	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}

	return float64(intersection) / float64(union)
}

// ---------------------------------------------------------------------------
// Token estimation utilities
// ---------------------------------------------------------------------------

// EstimateTokens provides a more nuanced token count estimate.
// Uses different ratios for different content types.
func EstimateTokens(s string) int {
	if len(s) == 0 {
		return 0
	}

	// Count code-like characters (backticks, braces, brackets, semicolons)
	codeChars := 0
	for _, r := range s {
		switch r {
		case '`', '{', '}', '[', ']', ';', '(', ')', '<', '>', '=', '|', '&':
			codeChars++
		}
	}

	totalChars := float64(len(s))
	codeRatio := float64(codeChars) / totalChars

	// Code-heavy text: ~3.2 chars per token (more tokens per char)
	// Prose text: ~4.5 chars per token (fewer tokens per char)
	// Blend based on code ratio
	charsPerToken := 4.5 - (codeRatio * 1.3) // ranges from 3.2 to 4.5

	// Clamp to reasonable range
	charsPerToken = math.Max(2.8, math.Min(5.0, charsPerToken))

	return int(math.Ceil(totalChars / charsPerToken))
}
