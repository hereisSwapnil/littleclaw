package agent_test

import (
	"littleclaw/pkg/agent"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"littleclaw/pkg/bus"
	"littleclaw/pkg/memory"
)

// newTestCronService creates a agent.CronService backed by a temp dir.
func newTestCronService(t *testing.T) (*agent.CronService, string) {
	t.Helper()
	dir := t.TempDir()
	msgBus := bus.NewMessageBus()
	mem, err := memory.NewStore(dir)
	if err != nil {
		t.Fatalf("memory.NewStore: %v", err)
	}
	cs := agent.NewCronService(dir, msgBus, mem)
	return cs, dir
}

// ---------------------------------------------------------------------------
// agent.SanitizeLabel tests
// ---------------------------------------------------------------------------

func TestSanitizeLabel_Basic(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"hello world", "hello_world"},
		{"my-task!", "my_task_"},
		{"abc123", "abc123"},
		{"ALL CAPS", "ALL_CAPS"},
		{"hello.world", "hello_world"},
	}
	for _, tc := range cases {
		got := agent.SanitizeLabel(tc.input)
		if got != tc.want {
			t.Errorf("agent.SanitizeLabel(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestSanitizeLabel_TruncatesAt20Chars(t *testing.T) {
	long := "averylongnamet​hatexceedstwentycharacters"
	got := agent.SanitizeLabel(long)
	if len(got) > 20 {
		t.Errorf("agent.SanitizeLabel should truncate to 20 chars, got %d: %q", len(got), got)
	}
}

func TestSanitizeLabel_Empty(t *testing.T) {
	got := agent.SanitizeLabel("")
	if got != "" {
		t.Errorf("agent.SanitizeLabel(\"\") = %q, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// agent.GenerateJobID tests
// ---------------------------------------------------------------------------

func TestGenerateJobID_ProducesValidID(t *testing.T) {
	id := agent.GenerateJobID("my task")
	if id == "" {
		t.Error("agent.GenerateJobID should not return empty string")
	}
	// Should be alphanumeric + underscores, max 20 chars
	if len(id) > 20 {
		t.Errorf("agent.GenerateJobID ID too long: %q", id)
	}
}

// ---------------------------------------------------------------------------
// agent.SplitLines tests
// ---------------------------------------------------------------------------

func TestSplitLines_Basic(t *testing.T) {
	input := "line1\nline2\nline3"
	got := agent.SplitLines(input)
	if len(got) != 3 {
		t.Errorf("agent.SplitLines expected 3 lines, got %d: %v", len(got), got)
	}
	if got[0] != "line1" || got[1] != "line2" || got[2] != "line3" {
		t.Errorf("agent.SplitLines output mismatch: %v", got)
	}
}

func TestSplitLines_TrailingNewline(t *testing.T) {
	input := "line1\nline2\n"
	got := agent.SplitLines(input)
	// trailing newline produces an empty string at end
	if len(got) < 2 {
		t.Errorf("agent.SplitLines with trailing newline: got %d lines, want >= 2", len(got))
	}
}

func TestSplitLines_Empty(t *testing.T) {
	got := agent.SplitLines("")
	if len(got) != 0 {
		t.Errorf("agent.SplitLines(\"\") = %v, want empty", got)
	}
}

func TestSplitLines_SingleLine(t *testing.T) {
	got := agent.SplitLines("single")
	if len(got) != 1 || got[0] != "single" {
		t.Errorf("agent.SplitLines single line: %v", got)
	}
}

// ---------------------------------------------------------------------------
// agent.CronService save/load round-trip tests
// ---------------------------------------------------------------------------

func TestCronService_SaveAndLoad(t *testing.T) {
	cs, dir := newTestCronService(t)

	// Start so the cron runner is active (required for AddJob)
	if err := cs.Start(t.Context()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	job := &agent.CronJob{
		ID:       "test_job_1",
		Schedule: "@every 1h",
		Command:  "echo hello",
		Label:    "test job 1",
		ChatID:   "12345",
		Channel:  "telegram",
	}
	if err := cs.AddJob(job); err != nil {
		t.Fatalf("AddJob() error = %v", err)
	}

	// Verify it was written to disk
	data, err := os.ReadFile(filepath.Join(dir, "CRON.json"))
	if err != nil {
		t.Fatalf("CRON.json not created: %v", err)
	}

	var jobs []*agent.CronJob
	if err := json.Unmarshal(data, &jobs); err != nil {
		t.Fatalf("CRON.json parse error: %v", err)
	}
	if len(jobs) != 1 {
		t.Errorf("expected 1 job in CRON.json, got %d", len(jobs))
	}
	if jobs[0].ID != "test_job_1" {
		t.Errorf("job ID mismatch: got %q, want %q", jobs[0].ID, "test_job_1")
	}

	// Create a new agent.CronService from disk and load
	cs2, _ := newTestCronService2(t, dir)
	if err := cs2.Load(); err != nil {
		t.Fatalf("load() error = %v", err)
	}
	if len(cs2.Jobs()) != 1 {
		t.Errorf("expected 1 job after reload, got %d", len(cs2.Jobs()))
	}
	if cs2.Jobs()["test_job_1"] == nil {
		t.Error("expected 'test_job_1' to be present after reload")
	}
}

// newTestCronService2 creates a agent.CronService using an existing dir (for reload tests).
func newTestCronService2(t *testing.T, dir string) (*agent.CronService, string) {
	t.Helper()
	msgBus := bus.NewMessageBus()
	mem, err := memory.NewStore(dir)
	if err != nil {
		t.Fatalf("memory.NewStore: %v", err)
	}
	return agent.NewCronService(dir, msgBus, mem), dir
}

// ---------------------------------------------------------------------------
// AddJob tests
// ---------------------------------------------------------------------------

func TestAddJob_AppearsInListJobs(t *testing.T) {
	cs, _ := newTestCronService(t)
	if err := cs.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	job := &agent.CronJob{
		ID:       "myjob",
		Schedule: "@every 1h",
		Command:  "echo hi",
		Label:    "My Job",
	}
	if err := cs.AddJob(job); err != nil {
		t.Fatalf("AddJob() error = %v", err)
	}

	jobs := cs.ListJobs()
	if len(jobs) != 1 || jobs[0].ID != "myjob" {
		t.Errorf("ListJobs() = %v, expected 1 job with ID 'myjob'", jobs)
	}
}

func TestAddJob_InvalidSchedule(t *testing.T) {
	cs, _ := newTestCronService(t)
	if err := cs.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	job := &agent.CronJob{
		ID:       "badjob",
		Schedule: "not-a-valid-cron",
		Command:  "echo hi",
		Label:    "Bad Job",
	}
	err := cs.AddJob(job)
	if err == nil {
		t.Error("AddJob() with invalid schedule should return error")
	}
}

func TestAddJob_ReplacesExisting(t *testing.T) {
	cs, _ := newTestCronService(t)
	if err := cs.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	job := &agent.CronJob{
		ID:       "replace_me",
		Schedule: "@every 1h",
		Command:  "echo v1",
		Label:    "v1",
	}
	_ = cs.AddJob(job)

	job2 := &agent.CronJob{
		ID:       "replace_me",
		Schedule: "@every 2h",
		Command:  "echo v2",
		Label:    "v2",
	}
	_ = cs.AddJob(job2)

	jobs := cs.ListJobs()
	if len(jobs) != 1 {
		t.Errorf("expected 1 job after replacement, got %d", len(jobs))
	}
	if jobs[0].Label != "v2" {
		t.Errorf("expected replaced job to have label 'v2', got %q", jobs[0].Label)
	}
}

// ---------------------------------------------------------------------------
// RemoveJob tests
// ---------------------------------------------------------------------------

func TestRemoveJob_RemovesFromList(t *testing.T) {
	cs, _ := newTestCronService(t)
	if err := cs.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	job := &agent.CronJob{
		ID:       "to_remove",
		Schedule: "@every 1h",
		Command:  "echo bye",
		Label:    "To Remove",
	}
	_ = cs.AddJob(job)
	if err := cs.RemoveJob("to_remove"); err != nil {
		t.Fatalf("RemoveJob() error = %v", err)
	}

	jobs := cs.ListJobs()
	if len(jobs) != 0 {
		t.Errorf("expected 0 jobs after removal, got %d", len(jobs))
	}
}

func TestRemoveJob_UnknownID(t *testing.T) {
	cs, _ := newTestCronService(t)
	if err := cs.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	err := cs.RemoveJob("no_such_job")
	if err == nil {
		t.Error("RemoveJob() on non-existent ID should return error")
	}
}

// ---------------------------------------------------------------------------
// GetRecentRuns tests
// ---------------------------------------------------------------------------

func TestGetRecentRuns_ReturnsRecords(t *testing.T) {
	cs, dir := newTestCronService(t)

	runsDir := filepath.Join(dir, "cron", "runs")
	_ = os.MkdirAll(runsDir, 0755)

	// Write two fake JSONL records
	record1 := agent.CronRunRecord{
		Ts:         time.Now().UnixMilli(),
		JobID:      "job1",
		Action:     "finished",
		Status:     "ok",
		DurationMs: 100,
	}
	record2 := agent.CronRunRecord{
		Ts:         time.Now().UnixMilli(),
		JobID:      "job1",
		Action:     "finished",
		Status:     "error",
		DurationMs: 50,
		Error:      "exit status 1",
	}

	logPath := filepath.Join(runsDir, "job1.jsonl")
	b1, _ := json.Marshal(record1)
	b2, _ := json.Marshal(record2)
	_ = os.WriteFile(logPath, append(append(b1, '\n'), append(b2, '\n')...), 0644)

	records := cs.GetRecentRuns("job1", 10)
	if len(records) != 2 {
		t.Errorf("GetRecentRuns() = %d records, want 2", len(records))
	}
	if records[0].Status != "ok" || records[1].Status != "error" {
		t.Errorf("records order mismatch: %v", records)
	}
}

func TestGetRecentRuns_MissingFile(t *testing.T) {
	cs, _ := newTestCronService(t)

	records := cs.GetRecentRuns("nonexistent_job", 10)
	if records != nil {
		t.Errorf("GetRecentRuns for missing file should return nil, got %v", records)
	}
}

func TestGetRecentRuns_RespectsMaxLines(t *testing.T) {
	cs, dir := newTestCronService(t)

	runsDir := filepath.Join(dir, "cron", "runs")
	_ = os.MkdirAll(runsDir, 0755)

	logPath := filepath.Join(runsDir, "biglog.jsonl")
	var lines [][]byte
	for i := 0; i < 20; i++ {
		rec := agent.CronRunRecord{
			Ts:     time.Now().UnixMilli(),
			JobID:  "biglog",
			Status: "ok",
		}
		b, _ := json.Marshal(rec)
		lines = append(lines, append(b, '\n'))
	}
	var content []byte
	for _, l := range lines {
		content = append(content, l...)
	}
	_ = os.WriteFile(logPath, content, 0644)

	records := cs.GetRecentRuns("biglog", 5)
	if len(records) > 5 {
		t.Errorf("GetRecentRuns should return at most 5 records, got %d", len(records))
	}
}

// ---------------------------------------------------------------------------
// recordRun tests
// ---------------------------------------------------------------------------

func TestRecordRun_CreatesJSONLFile(t *testing.T) {
	cs, dir := newTestCronService(t)

	_ = os.MkdirAll(cs.RunsDir, 0755)
	cs.RecordRun("job42", "ok", "", 123)

	logPath := filepath.Join(dir, "cron", "runs", "job42.jsonl")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("expected JSONL log to be created: %v", err)
	}
	if !strings.Contains(string(data), "job42") {
		t.Errorf("JSONL log does not contain job ID: %q", string(data))
	}
}

func TestRecordRun_ErrorStatus(t *testing.T) {
	cs, _ := newTestCronService(t)

	_ = os.MkdirAll(cs.RunsDir, 0755)
	cs.RecordRun("errjob", "error", "something went wrong", 50)

	logPath := filepath.Join(cs.RunsDir, "errjob.jsonl")
	data, _ := os.ReadFile(logPath)

	var rec agent.CronRunRecord
	_ = json.Unmarshal([]byte(strings.TrimSpace(string(data))), &rec)
	if rec.Status != "error" {
		t.Errorf("expected status 'error', got %q", rec.Status)
	}
	if rec.Error != "something went wrong" {
		t.Errorf("expected error message 'something went wrong', got %q", rec.Error)
	}
}
