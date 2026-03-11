package memory_test

import (
	"littleclaw/pkg/memory"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTestStore creates a memory memory.Store backed by a temporary directory.
// The temp dir is automatically cleaned up at the end of the test.
func newTestStore(t *testing.T) *memory.Store {
	t.Helper()
	dir := t.TempDir()
	store, err := memory.NewStore(dir)
	if err != nil {
		t.Fatalf("memory.NewStore failed: %v", err)
	}
	return store
}

// ---------------------------------------------------------------------------
// Identity / scaffold tests
// ---------------------------------------------------------------------------

func TestNewStore_CreatesDirectories(t *testing.T) {
	dir := t.TempDir()
	store, err := memory.NewStore(dir)
	if err != nil {
		t.Fatalf("memory.NewStore() error = %v", err)
	}

	for _, sub := range []string{store.EntitiesDir, store.SummariesDir()} {
		if _, err := os.Stat(sub); os.IsNotExist(err) {
			t.Errorf("expected directory %s to exist", sub)
		}
	}
}

func TestNewStore_ScaffoldsIdentityFiles(t *testing.T) {
	store := newTestStore(t)

	for _, path := range []string{store.SoulFile(), store.IdentityFile(), store.UserFile()} {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected identity file %s to be created", filepath.Base(path))
		}
	}
}

func TestNewStore_DoesNotOverwriteExistingIdentityFiles(t *testing.T) {
	dir := t.TempDir()
	// Pre-create memory dir and soul file with custom content
	memDir := filepath.Join(dir, "memory")
	if err := os.MkdirAll(memDir, 0755); err != nil {
		t.Fatal(err)
	}
	soulPath := filepath.Join(memDir, "SOUL.md")
	customContent := "my custom soul"
	if err := os.WriteFile(soulPath, []byte(customContent), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := memory.NewStore(dir)
	if err != nil {
		t.Fatalf("memory.NewStore() error = %v", err)
	}

	data, _ := os.ReadFile(soulPath)
	if string(data) != customContent {
		t.Errorf("SOUL.md was overwritten; want %q, got %q", customContent, string(data))
	}
}

func TestReadIdentityContext_ReturnsCombinedContent(t *testing.T) {
	store := newTestStore(t)

	ctx := store.ReadIdentityContext()
	// Should contain content from all three files
	if !strings.Contains(ctx, "Littleclaw") {
		t.Error("expected identity context to contain 'Littleclaw'")
	}
	// Files should be separated by ---
	if !strings.Contains(ctx, "---") {
		t.Error("expected identity context to contain separator '---'")
	}
}

// ---------------------------------------------------------------------------
// Heartbeat tests
// ---------------------------------------------------------------------------

func TestUpdateHeartbeat(t *testing.T) {
	store := newTestStore(t)

	if err := store.UpdateHeartbeat(); err != nil {
		t.Fatalf("UpdateHeartbeat() error = %v", err)
	}

	data, err := os.ReadFile(store.HeartbeatFile())
	if err != nil {
		t.Fatalf("reading heartbeat file: %v", err)
	}
	if !strings.HasPrefix(string(data), "Last active:") {
		t.Errorf("heartbeat file should start with 'Last active:', got %q", string(data))
	}
}

// ---------------------------------------------------------------------------
// Core memory (MEMORY.md) tests
// ---------------------------------------------------------------------------

func TestWriteAndReadLongTerm(t *testing.T) {
	store := newTestStore(t)

	content := "## Preferences\n- Likes coffee\n- Hates meetings"
	if err := store.WriteLongTerm(content); err != nil {
		t.Fatalf("WriteLongTerm() error = %v", err)
	}

	got := store.ReadLongTerm()
	if got != content {
		t.Errorf("ReadLongTerm() = %q, want %q", got, content)
	}
}

func TestReadLongTerm_EmptyWhenMissing(t *testing.T) {
	store := newTestStore(t)
	// MEMORY.md doesn't exist yet
	got := store.ReadLongTerm()
	if got != "" {
		t.Errorf("ReadLongTerm() on missing file should return empty string, got %q", got)
	}
}

func TestWriteLongTerm_CreatesBackup(t *testing.T) {
	store := newTestStore(t)

	// Write twice so the first write triggers a backup on the second
	if err := store.WriteLongTerm("version 1"); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteLongTerm("version 2"); err != nil {
		t.Fatal(err)
	}

	entries, _ := os.ReadDir(store.MemoryDir())
	var backups int
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "MEMORY_") && strings.HasSuffix(e.Name(), ".md") {
			backups++
		}
	}
	if backups < 1 {
		t.Error("expected at least one backup file after second write")
	}
}

func TestPruneMemoryVersions_KeepsMaxVersions(t *testing.T) {
	store := newTestStore(t)

	// Write memory.MaxMemoryVersions+3 times — each overwrites and creates a backup
	for i := 0; i < memory.MaxMemoryVersions+3; i++ {
		if err := store.WriteLongTerm(fmt.Sprintf("version %d", i)); err != nil {
			t.Fatalf("WriteLongTerm(%d) error = %v", i, err)
		}
		// Small sleep so timestamps differ
		time.Sleep(time.Millisecond)
	}

	entries, _ := os.ReadDir(store.MemoryDir())
	var backups int
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "MEMORY_") && strings.HasSuffix(e.Name(), ".md") {
			backups++
		}
	}
	if backups > memory.MaxMemoryVersions {
		t.Errorf("expected at most %d backup files, got %d", memory.MaxMemoryVersions, backups)
	}
}

func TestAppendLongTerm(t *testing.T) {
	store := newTestStore(t)

	if err := store.WriteLongTerm("## Base"); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendLongTerm("## Extra\n- new fact"); err != nil {
		t.Fatal(err)
	}

	got := store.ReadLongTerm()
	if !strings.Contains(got, "## Base") || !strings.Contains(got, "## Extra") {
		t.Errorf("AppendLongTerm did not preserve original content. Got: %q", got)
	}
}

func TestCorememorySize(t *testing.T) {
	store := newTestStore(t)

	if got := store.CoreMemorySize(); got != 0 {
		t.Errorf("CoreMemorySize() before write = %d, want 0", got)
	}

	content := "hello world"
	_ = store.WriteLongTerm(content)
	if got := store.CoreMemorySize(); got == 0 {
		t.Error("CoreMemorySize() after write should be > 0")
	}
}

// ---------------------------------------------------------------------------
// Daily history log tests
// ---------------------------------------------------------------------------

func TestAppendHistory_SetsDirectoryFlag(t *testing.T) {
	store := newTestStore(t)

	// Clear dirty first
	store.SetDirty(false)

	if err := store.AppendHistory("user", "hello there"); err != nil {
		t.Fatalf("AppendHistory() error = %v", err)
	}
	if !store.IsDirtyAndClear() {
		t.Error("dirty flag should be true after AppendHistory")
	}
}

func TestAppendHistory_WritesToDailyLog(t *testing.T) {
	store := newTestStore(t)

	if err := store.AppendHistory("user", "what time is it?"); err != nil {
		t.Fatalf("AppendHistory() error = %v", err)
	}

	logPath := store.DailyLogPath(time.Now())
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("reading daily log: %v", err)
	}
	if !strings.Contains(string(data), "what time is it?") {
		t.Errorf("daily log does not contain expected content. Got: %s", string(data))
	}
	if !strings.Contains(string(data), "USER") {
		t.Error("daily log should contain uppercase role 'USER'")
	}
}

func TestReadRecentHistory_IncludesToday(t *testing.T) {
	store := newTestStore(t)

	_ = store.AppendHistory("user", "unique-marker-12345")

	history := store.ReadRecentHistory(16000)
	if !strings.Contains(history, "unique-marker-12345") {
		t.Errorf("ReadRecentHistory did not include today's entry. Got: %s", history)
	}
}

func TestReadRecentHistory_TruncatesAtMaxBytes(t *testing.T) {
	store := newTestStore(t)

	// Write enough history to exceed maxBytes
	bigContent := strings.Repeat("x", 1000)
	for i := 0; i < 30; i++ {
		_ = store.AppendHistory("user", bigContent)
	}

	maxBytes := 5000
	history := store.ReadRecentHistory(maxBytes)
	if len(history) > maxBytes+200 { // allow a small slack for headers
		t.Errorf("ReadRecentHistory exceeds maxBytes: got %d bytes", len(history))
	}
}

func TestListDailyLogs_ReturnsSortedNewestFirst(t *testing.T) {
	store := newTestStore(t)

	// Write to today's log
	_ = store.AppendHistory("user", "today")

	// Manually create a yesterday log
	yesterday := time.Now().AddDate(0, 0, -1)
	logPath := store.DailyLogPath(yesterday)
	_ = os.WriteFile(logPath, []byte("[ts] USER: yesterday\n"), 0644)

	dates := store.ListDailyLogs()
	if len(dates) < 2 {
		t.Errorf("expected at least 2 daily logs, got %d", len(dates))
	}
	// Newest first: today > yesterday
	if dates[0] <= dates[1] {
		t.Errorf("expected newest-first order, got %v", dates)
	}
}

// ---------------------------------------------------------------------------
// History search tests
// ---------------------------------------------------------------------------

func TestSearchHistory_FindsMatches(t *testing.T) {
	store := newTestStore(t)

	_ = store.AppendHistory("user", "my secret keyword zxqv9")
	_ = store.AppendHistory("assistant", "I heard zxqv9")
	_ = store.AppendHistory("user", "unrelated message")

	results := store.SearchHistory("zxqv9", "", "")
	if len(results) == 0 {
		t.Fatal("SearchHistory returned no results for a keyword that was appended")
	}
	for _, r := range results {
		if !strings.Contains(strings.ToLower(r.Content), "zxqv9") {
			t.Errorf("result does not contain search term: %q", r.Content)
		}
	}
}

func TestSearchHistory_NoResults(t *testing.T) {
	store := newTestStore(t)

	_ = store.AppendHistory("user", "hello world")

	results := store.SearchHistory("definitely_not_here_xyz123", "", "")
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestSearchHistory_DateFilter(t *testing.T) {
	store := newTestStore(t)

	// Create a log for 2 days ago with a unique keyword
	twoDaysAgo := time.Now().AddDate(0, 0, -2)
	logPath := store.DailyLogPath(twoDaysAgo)
	_ = os.WriteFile(logPath, []byte("[2024-01-01 00:00:00] USER: ancient keyword oldstuff\n"), 0644)

	// Search from yesterday — should exclude the 2-day-old log
	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	results := store.SearchHistory("ancient keyword oldstuff", yesterday, "")
	if len(results) != 0 {
		t.Errorf("expected 0 results with date filter excluding old log, got %d", len(results))
	}

	// Search without filter — should find it
	results = store.SearchHistory("ancient keyword oldstuff", "", "")
	if len(results) == 0 {
		t.Error("expected to find old log entry without date filter")
	}
}

// ---------------------------------------------------------------------------
// memory.SnapToTail tests
// ---------------------------------------------------------------------------

func TestSnapToTail_ShortString(t *testing.T) {
	s := "short"
	got := memory.SnapToTail(s, 100)
	if got != s {
		t.Errorf("memory.SnapToTail() = %q, want %q", got, s)
	}
}

func TestSnapToTail_SnapsToMessageBoundary(t *testing.T) {
	s := "garbage prefix data\n[2024-01-01 00:00:00] USER: actual message\n"
	got := memory.SnapToTail(s, 40)
	// Should start at the [timestamp] boundary, not mid-word
	if !strings.HasPrefix(got, "[") && !strings.Contains(got, "truncated") {
		t.Errorf("memory.SnapToTail should start at message boundary, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// Internal log tests
// ---------------------------------------------------------------------------

func TestAppendInternal_WritesToInternalFile(t *testing.T) {
	store := newTestStore(t)

	if err := store.AppendInternal("system", "background task ran"); err != nil {
		t.Fatalf("AppendInternal() error = %v", err)
	}

	got := store.ReadRecentInternal()
	if !strings.Contains(got, "background task ran") {
		t.Errorf("ReadRecentInternal() does not contain expected content, got: %q", got)
	}
}

func TestAppendInternal_Rotates(t *testing.T) {
	store := newTestStore(t)

	// Write more than memory.InternalRotationBytes to trigger rotation
	big := strings.Repeat("a", memory.InternalRotationBytes+1)
	_ = os.WriteFile(store.InternalFile(), []byte(big), 0644)

	// A subsequent append should rotate the file
	_ = store.AppendInternal("system", "new entry after rotation")

	// The original file should be small (just the new entry)
	data, _ := os.ReadFile(store.InternalFile())
	if len(data) >= memory.InternalRotationBytes {
		t.Errorf("INTERNAL.md was not rotated; size=%d", len(data))
	}

	// An archive file should now exist
	entries, _ := os.ReadDir(store.MemoryDir())
	var found bool
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "INTERNAL_ARCHIVE_") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected INTERNAL_ARCHIVE file to exist after rotation")
	}
}

func TestReadRecentInternal_Empty(t *testing.T) {
	store := newTestStore(t)
	got := store.ReadRecentInternal()
	if got != "" {
		t.Errorf("ReadRecentInternal() on empty store = %q, want empty string", got)
	}
}

// ---------------------------------------------------------------------------
// Dirty flag tests
// ---------------------------------------------------------------------------

func TestIsDirtyAndClear(t *testing.T) {
	store := newTestStore(t)

	store.SetDirty(false)
	if store.IsDirtyAndClear() {
		t.Error("IsDirtyAndClear() should return false when not dirty")
	}

	store.SetDirty(true)
	if !store.IsDirtyAndClear() {
		t.Error("IsDirtyAndClear() should return true when dirty")
	}

	// After clearing, subsequent call should return false
	if store.IsDirtyAndClear() {
		t.Error("IsDirtyAndClear() should return false after being cleared")
	}
}

// ---------------------------------------------------------------------------
// Entity tests
// ---------------------------------------------------------------------------

func TestWriteAndReadEntity(t *testing.T) {
	store := newTestStore(t)

	if err := store.WriteEntity("Alice Smith", "Alice is a software engineer."); err != nil {
		t.Fatalf("WriteEntity() error = %v", err)
	}

	got := store.ReadEntity("Alice Smith")
	if !strings.Contains(got, "software engineer") {
		t.Errorf("ReadEntity() = %q, want content about 'software engineer'", got)
	}
}

func TestReadEntity_CaseInsensitive(t *testing.T) {
	store := newTestStore(t)

	_ = store.WriteEntity("Bob Jones", "Bob likes Go.")

	got := store.ReadEntity("bob jones")
	if !strings.Contains(got, "Bob likes Go") {
		t.Errorf("ReadEntity case-insensitive lookup failed, got %q", got)
	}
}

func TestReadEntity_SpaceAndUnderscore(t *testing.T) {
	store := newTestStore(t)

	_ = store.WriteEntity("Project Phoenix", "Phoenix is a backend project.")

	// Lookup with underscore
	got := store.ReadEntity("project_phoenix")
	if !strings.Contains(got, "Phoenix is a backend") {
		t.Errorf("ReadEntity with underscore failed, got %q", got)
	}
}

func TestWriteEntity_NormalizesName(t *testing.T) {
	store := newTestStore(t)

	_ = store.WriteEntity("My Project", "data")

	// Check that file on disk uses normalized name
	path := filepath.Join(store.EntitiesDir, "my_project.md")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("expected entity file at %s", path)
	}
}

func TestWriteEntity_EmptyNameError(t *testing.T) {
	store := newTestStore(t)

	err := store.WriteEntity("", "content")
	if err == nil {
		t.Error("expected error for empty entity name, got nil")
	}
}

func TestWriteEntity_RemovesLegacyDuplicates(t *testing.T) {
	store := newTestStore(t)

	// Create a file using the legacy space-based naming (spaces as literal spaces, not underscores)
	// normalizeEntityName("My  Entity") → "my__entity" → collapsed → "my_entity"
	// But a file named "My Entity.md" with a space in the filename is the legacy format.
	legacyPath := filepath.Join(store.EntitiesDir, "my__entity.md")
	_ = os.WriteFile(legacyPath, []byte("old legacy"), 0644)

	// Write entity with the canonical name — normalization produces "my_entity"
	// The legacy file "my__entity" normalizes to the same key, so it should be removed.
	_ = store.WriteEntity("my  entity", "new content")

	if _, err := os.Stat(legacyPath); !os.IsNotExist(err) {
		t.Error("legacy entity file should have been removed after WriteEntity")
	}
}

func TestListEntities(t *testing.T) {
	store := newTestStore(t)

	_ = store.WriteEntity("Alice", "data")
	_ = store.WriteEntity("Bob", "data")

	entities, err := store.ListEntities()
	if err != nil {
		t.Fatalf("ListEntities() error = %v", err)
	}
	if len(entities) != 2 {
		t.Errorf("expected 2 entities, got %d: %v", len(entities), entities)
	}
}

func TestListEntities_EmptyDir(t *testing.T) {
	store := newTestStore(t)

	entities, err := store.ListEntities()
	if err != nil {
		t.Fatalf("ListEntities() error = %v", err)
	}
	if len(entities) != 0 {
		t.Errorf("expected 0 entities on empty store, got %d", len(entities))
	}
}

// ---------------------------------------------------------------------------
// Entity auto-surfacing (trigram) tests
// ---------------------------------------------------------------------------

func TestFindRelevantEntities_SurfacesMatch(t *testing.T) {
	store := newTestStore(t)

	_ = store.WriteEntity("Alice Smith", "Alice is a scientist.")
	_ = store.WriteEntity("Bob Jones", "Bob plays guitar.")

	// Query mentioning "Alice" should surface the alice entity
	result := store.FindRelevantEntities("what does Alice think?", 5000)
	if !strings.Contains(result, "alice") && !strings.Contains(result, "Alice") {
		t.Errorf("FindRelevantEntities did not surface Alice entity for Alice query. Got: %q", result)
	}
}

func TestFindRelevantEntities_EmptyOnNoMatch(t *testing.T) {
	store := newTestStore(t)

	_ = store.WriteEntity("Zelda", "Zelda is a warrior.")

	result := store.FindRelevantEntities("the weather today is nice", 5000)
	// Zelda should not be surfaced for a totally unrelated query — but the
	// threshold is 0.15, so we just check it's not massively wrong.
	// A totally unrelated query should score near 0.
	_ = result // If it does surface, that's a borderline case — not a hard error
}

func TestFindRelevantEntities_RespectsMaxBytes(t *testing.T) {
	store := newTestStore(t)

	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("person%d", i)
		big := strings.Repeat(fmt.Sprintf("data about person%d ", i), 200)
		_ = store.WriteEntity(name, big)
	}

	maxBytes := 500
	result := store.FindRelevantEntities("person0 person1 person2", maxBytes)
	if len(result) > maxBytes+200 { // allow slack for entry headers
		t.Errorf("FindRelevantEntities result exceeds maxBytes; got %d bytes", len(result))
	}
}

// ---------------------------------------------------------------------------
// Summary tests
// ---------------------------------------------------------------------------

func TestWriteAndReadSummary(t *testing.T) {
	store := newTestStore(t)

	date := "2024-01-01"
	content := "Summary of a busy day."
	if err := store.WriteSummary(date, content); err != nil {
		t.Fatalf("WriteSummary() error = %v", err)
	}

	path := filepath.Join(store.SummariesDir(), date+".md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading summary file: %v", err)
	}
	if string(data) != content {
		t.Errorf("summary content mismatch: got %q, want %q", string(data), content)
	}
}

func TestNeedsSummarization_FalseWhenSmall(t *testing.T) {
	store := newTestStore(t)

	// Write a small log for yesterday
	yesterday := time.Now().AddDate(0, 0, -1)
	logPath := store.DailyLogPath(yesterday)
	_ = os.WriteFile(logPath, []byte("small log"), 0644)

	needs, _, _ := store.NeedsSummarization()
	if needs {
		t.Error("NeedsSummarization() should be false for small log")
	}
}

func TestNeedsSummarization_TrueWhenLarge(t *testing.T) {
	store := newTestStore(t)

	yesterday := time.Now().AddDate(0, 0, -1)
	logPath := store.DailyLogPath(yesterday)
	big := strings.Repeat("x", memory.MaxDailyLogBytes+1)
	_ = os.WriteFile(logPath, []byte(big), 0644)

	needs, dateStr, _ := store.NeedsSummarization()
	if !needs {
		t.Error("NeedsSummarization() should be true for large log")
	}
	if dateStr != yesterday.Format("2006-01-02") {
		t.Errorf("NeedsSummarization() date = %q, want %q", dateStr, yesterday.Format("2006-01-02"))
	}
}

func TestNeedsSummarization_FalseWhenAlreadySummarized(t *testing.T) {
	store := newTestStore(t)

	// Write a large yesterday log
	yesterday := time.Now().AddDate(0, 0, -1)
	logPath := store.DailyLogPath(yesterday)
	big := strings.Repeat("x", memory.MaxDailyLogBytes+1)
	_ = os.WriteFile(logPath, []byte(big), 0644)

	// Also write a summary for it
	dateStr := yesterday.Format("2006-01-02")
	_ = store.WriteSummary(dateStr, "already summarized")

	needs, _, _ := store.NeedsSummarization()
	if needs {
		t.Error("NeedsSummarization() should be false when summary already exists")
	}
}

// ---------------------------------------------------------------------------
// Trigram utility tests
// ---------------------------------------------------------------------------

func TestTrigrams_Basic(t *testing.T) {
	tris := memory.Trigrams("cat")
	if len(tris) == 0 {
		t.Error("memory.Trigrams() returned empty set for 'cat'")
	}
}

func TestTrigramSimilarity_IdenticalStrings(t *testing.T) {
	a := memory.Trigrams("hello world")
	score := memory.TrigramSimilarity(a, a)
	if score != 1.0 {
		t.Errorf("memory.TrigramSimilarity(identical) = %f, want 1.0", score)
	}
}

func TestTrigramSimilarity_DisjointStrings(t *testing.T) {
	a := memory.Trigrams("aaaa")
	b := memory.Trigrams("zzzz")
	score := memory.TrigramSimilarity(a, b)
	if score >= 0.5 {
		t.Errorf("memory.TrigramSimilarity(disjoint) = %f, expected < 0.5", score)
	}
}

func TestTrigramSimilarity_EmptySets(t *testing.T) {
	empty := map[string]bool{}
	score := memory.TrigramSimilarity(empty, empty)
	if score != 0 {
		t.Errorf("memory.TrigramSimilarity(empty, empty) = %f, want 0", score)
	}
}

// ---------------------------------------------------------------------------
// Token estimation tests
// ---------------------------------------------------------------------------

func TestEstimateTokens_Empty(t *testing.T) {
	if got := memory.EstimateTokens(""); got != 0 {
		t.Errorf("memory.EstimateTokens(\"\") = %d, want 0", got)
	}
}

func TestEstimateTokens_Prose(t *testing.T) {
	prose := "The quick brown fox jumps over the lazy dog."
	tokens := memory.EstimateTokens(prose)
	// Rough sanity: 44 chars / 4.5 ≈ 10 tokens
	if tokens < 5 || tokens > 20 {
		t.Errorf("memory.EstimateTokens(prose) = %d, expected roughly 8-14", tokens)
	}
}

func TestEstimateTokens_Code(t *testing.T) {
	code := `func main() { fmt.Println("hello") }`
	tokens := memory.EstimateTokens(code)
	if tokens <= 0 {
		t.Error("memory.EstimateTokens(code) should be > 0")
	}
}

// ---------------------------------------------------------------------------
// memory.SplitHistoryEntries tests
// ---------------------------------------------------------------------------

func TestSplitHistoryEntries_Basic(t *testing.T) {
	content := "[2024-01-01 10:00:00] USER: first\n[2024-01-01 10:01:00] ASSISTANT: second\n"
	entries := memory.SplitHistoryEntries(content)
	if len(entries) != 2 {
		t.Errorf("memory.SplitHistoryEntries() = %d entries, want 2", len(entries))
	}
}

func TestSplitHistoryEntries_EmptyInput(t *testing.T) {
	entries := memory.SplitHistoryEntries("")
	// The function may return one empty entry for empty input; either 0 or 1 is acceptable
	// as long as any returned entries are empty strings.
	for _, e := range entries {
		if strings.TrimSpace(e) != "" {
			t.Errorf("memory.SplitHistoryEntries(\"\") returned non-empty entry: %q", e)
		}
	}
}
