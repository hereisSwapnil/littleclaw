package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"littleclaw/pkg/bus"
	"littleclaw/pkg/memory"

	"github.com/robfig/cron/v3"
)

// CronJobState holds runtime execution metadata for a job.
type CronJobState struct {
	LastRunAtMs       int64  `json:"lastRunAtMs,omitempty"`
	NextRunAtMs       int64  `json:"nextRunAtMs,omitempty"`
	LastStatus        string `json:"lastStatus,omitempty"` // "ok" | "error"
	LastDurationMs    int64  `json:"lastDurationMs,omitempty"`
	ConsecutiveErrors int    `json:"consecutiveErrors"`
	LastError         string `json:"lastError,omitempty"`
}

// CronJob represents a single scheduled task persisted to disk.
type CronJob struct {
	ID       string       `json:"id"`
	Schedule string       `json:"schedule"` // robfig cron expression, e.g. "@every 10s" or "*/5 * * * *"
	Command  string       `json:"command"`  // shell command OR description for the LLM to run in exec
	ChatID   string       `json:"chat_id"`  // Telegram chat ID to reply to
	Channel  string       `json:"channel"`  // channel to respond on (e.g. "telegram")
	Label    string       `json:"label"`    // human-readable label shown to user
	Once     bool         `json:"once"`     // if true, job is removed after one execution
	State    CronJobState `json:"state"`
}

// CronRunRecord is one line appended to the per-job JSONL run log.
type CronRunRecord struct {
	Ts          int64  `json:"ts"`
	JobID       string `json:"jobId"`
	Action      string `json:"action"`             // always "finished"
	Status      string `json:"status"`             // "ok" | "error"
	DurationMs  int64  `json:"durationMs"`
	NextRunAtMs int64  `json:"nextRunAtMs,omitempty"`
	Error       string `json:"error,omitempty"`
}

// CronService manages persistent, file-backed cron jobs and runs them on schedule.
type CronService struct {
	mu           sync.Mutex
	jobs         map[string]*CronJob
	entryIDs     map[string]cron.EntryID
	cronRunner   *cron.Cron
	dataFile     string // absolute path to CRON.json
	runsDir      string // absolute path to cron/runs/ directory
	workspaceDir string
	msgBus       *bus.MessageBus
	memStore     *memory.Store
}

// NewCronService creates a CronService backed by $workspace/CRON.json.
func NewCronService(workspaceDir string, msgBus *bus.MessageBus, mem *memory.Store) *CronService {
	runsDir := filepath.Join(workspaceDir, "cron", "runs")
	return &CronService{
		jobs:         make(map[string]*CronJob),
		entryIDs:     make(map[string]cron.EntryID),
		cronRunner:   cron.New(cron.WithSeconds()),
		dataFile:     filepath.Join(workspaceDir, "CRON.json"),
		runsDir:      runsDir,
		workspaceDir: workspaceDir,
		msgBus:       msgBus,
		memStore:     mem,
	}
}

// Start loads persisted jobs and begins the cron scheduler.
func (cs *CronService) Start(ctx context.Context) error {
	// Ensure the runs directory exists
	if err := os.MkdirAll(cs.runsDir, 0755); err != nil {
		return fmt.Errorf("failed to create cron runs dir: %w", err)
	}

	if err := cs.load(); err != nil {
		log.Printf("⏰ CronService: no existing jobs loaded (%v), starting fresh\n", err)
	}

	// Schedule all loaded jobs
	cs.mu.Lock()
	for id, job := range cs.jobs {
		if err := cs.schedule(job); err != nil {
			log.Printf("⏰ CronService: failed to schedule job %s: %v\n", id, err)
		}
	}
	cs.mu.Unlock()

	cs.cronRunner.Start()
	log.Printf("⏰ CronService started with %d job(s)\n", len(cs.jobs))

	// Stop when context is cancelled
	go func() {
		<-ctx.Done()
		cs.cronRunner.Stop()
		log.Println("⏰ CronService stopped")
	}()

	return nil
}

// AddJob adds a new cron job (or replaces an existing one with the same ID), persists it, and schedules it.
func (cs *CronService) AddJob(job *CronJob) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	// If a job with this ID already exists, remove it first (un-schedule)
	if oldJob, exists := cs.jobs[job.ID]; exists {
		log.Printf("⏰ CronService: replacing existing job %s\n", job.ID)
		if entryID, ok := cs.entryIDs[oldJob.ID]; ok {
			cs.cronRunner.Remove(entryID)
			delete(cs.entryIDs, oldJob.ID)
		}
	}

	if err := cs.schedule(job); err != nil {
		return fmt.Errorf("invalid schedule %q: %w", job.Schedule, err)
	}

	// Populate NextRunAtMs from the freshly-added robfig entry
	if entryID, ok := cs.entryIDs[job.ID]; ok {
		entry := cs.cronRunner.Entry(entryID)
		if !entry.Next.IsZero() {
			job.State.NextRunAtMs = entry.Next.UnixMilli()
		}
	}

	cs.jobs[job.ID] = job
	return cs.save()
}

// RemoveJob removes a running job by its ID.
func (cs *CronService) RemoveJob(id string) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	entryID, ok := cs.entryIDs[id]
	if !ok {
		return fmt.Errorf("job %q not found", id)
	}

	cs.cronRunner.Remove(entryID)
	delete(cs.entryIDs, id)
	delete(cs.jobs, id)
	return cs.save()
}

// ListJobs returns all currently scheduled jobs.
func (cs *CronService) ListJobs() []*CronJob {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	result := make([]*CronJob, 0, len(cs.jobs))
	for _, j := range cs.jobs {
		result = append(result, j)
	}
	return result
}

// schedule adds a job to the robfig cron runner (must hold mu).
func (cs *CronService) schedule(job *CronJob) error {
	entryID, err := cs.cronRunner.AddFunc(job.Schedule, cs.runnerFor(job))
	if err != nil {
		return err
	}
	cs.entryIDs[job.ID] = entryID
	return nil
}

// runnerFor returns the function that executes the job and messages the user.
func (cs *CronService) runnerFor(job *CronJob) func() {
	return func() {
		// Verify job still exists (it might have been removed by a near-simultaneous tick for a one-time job)
		cs.mu.Lock()
		_, exists := cs.jobs[job.ID]
		cs.mu.Unlock()
		if !exists {
			return
		}

		if job.Once {
			log.Printf("⏰ CronService: firing one-time job %s, removing immediately\n", job.ID)
			_ = cs.RemoveJob(job.ID)
		} else {
			log.Printf("⏰ CronService: firing job %s (%s)\n", job.ID, job.Label)
		}

		start := time.Now()
		cmd := exec.Command("sh", "-c", job.Command)
		cmd.Dir = cs.workspaceDir

		output, err := cmd.CombinedOutput()
		durationMs := time.Since(start).Milliseconds()

		var msg string
		var runStatus string
		var runErr string

		if err != nil {
			runStatus = "error"
			runErr = err.Error()
			msg = fmt.Sprintf("⚠️ Cron job `%s` failed:\n```\n%s\n```", job.Label, output)
		} else {
			runStatus = "ok"
			trimmed := string(output)
			if trimmed == "" {
				trimmed = "(no output)"
			}
			msg = trimmed
		}

		// Update in-memory state and persist
		cs.mu.Lock()
		if liveJob, ok := cs.jobs[job.ID]; ok {
			liveJob.State.LastRunAtMs = start.UnixMilli()
			liveJob.State.LastDurationMs = durationMs
			liveJob.State.LastStatus = runStatus
			if runStatus == "error" {
				liveJob.State.ConsecutiveErrors++
				liveJob.State.LastError = runErr
			} else {
				liveJob.State.ConsecutiveErrors = 0
				liveJob.State.LastError = ""
			}
			// Refresh next run time from the scheduler
			if entryID, ok := cs.entryIDs[job.ID]; ok {
				entry := cs.cronRunner.Entry(entryID)
				if !entry.Next.IsZero() {
					liveJob.State.NextRunAtMs = entry.Next.UnixMilli()
				}
			}
			_ = cs.save()
		}
		cs.mu.Unlock()

		// Append run record to per-job JSONL log
		cs.recordRun(job.ID, runStatus, runErr, durationMs)

		// Send result to the user's Telegram chat
		if job.ChatID != "" && job.Channel != "" {
			cs.msgBus.SendOutbound(bus.OutboundMessage{
				Channel: job.Channel,
				ChatID:  job.ChatID,
				Content: msg,
			})
		}

		// Log to INTERNAL.md for agent reflection
		logMsg := fmt.Sprintf("[Cron Job Runtime] Job '%s' (%s) fired. Status: %s. Duration: %dms. Result: %s", job.Label, job.ID, runStatus, durationMs, msg)
		cs.memStore.AppendInternal("CRON", logMsg)
	}
}

// recordRun appends a CronRunRecord to the per-job JSONL file in the runs directory.
func (cs *CronService) recordRun(jobID, status, errMsg string, durationMs int64) {
	if err := os.MkdirAll(cs.runsDir, 0755); err != nil {
		log.Printf("⏰ CronService: failed to create runs dir: %v\n", err)
		return
	}

	// Compute nextRunAtMs from live state if available
	cs.mu.Lock()
	var nextRunAtMs int64
	if j, ok := cs.jobs[jobID]; ok {
		nextRunAtMs = j.State.NextRunAtMs
	}
	cs.mu.Unlock()

	record := CronRunRecord{
		Ts:          time.Now().UnixMilli(),
		JobID:       jobID,
		Action:      "finished",
		Status:      status,
		DurationMs:  durationMs,
		NextRunAtMs: nextRunAtMs,
		Error:       errMsg,
	}

	data, err := json.Marshal(record)
	if err != nil {
		log.Printf("⏰ CronService: failed to marshal run record: %v\n", err)
		return
	}

	logPath := filepath.Join(cs.runsDir, jobID+".jsonl")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Printf("⏰ CronService: failed to open run log %s: %v\n", logPath, err)
		return
	}
	defer f.Close()

	_, _ = f.Write(append(data, '\n'))
}

// GetRecentRuns reads the last N lines from a job's JSONL run log file.
func (cs *CronService) GetRecentRuns(jobID string, maxLines int) []CronRunRecord {
	logPath := filepath.Join(cs.runsDir, jobID+".jsonl")
	data, err := os.ReadFile(logPath)
	if err != nil {
		return nil
	}

	lines := splitLines(string(data))
	// Take the last maxLines non-empty lines
	var records []CronRunRecord
	for i := len(lines) - 1; i >= 0 && len(records) < maxLines; i-- {
		line := lines[i]
		if line == "" {
			continue
		}
		var rec CronRunRecord
		if err := json.Unmarshal([]byte(line), &rec); err == nil {
			records = append([]CronRunRecord{rec}, records...) // prepend to keep chronological order
		}
	}
	return records
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

// load reads CRON.json from disk.
func (cs *CronService) load() error {
	data, err := os.ReadFile(cs.dataFile)
	if err != nil {
		return err
	}

	var jobs []*CronJob
	if err := json.Unmarshal(data, &jobs); err != nil {
		return err
	}

	for _, j := range jobs {
		cs.jobs[j.ID] = j
	}
	return nil
}

// save writes the current jobs to CRON.json.
func (cs *CronService) save() error {
	jobs := make([]*CronJob, 0, len(cs.jobs))
	for _, j := range cs.jobs {
		jobs = append(jobs, j)
	}

	data, err := json.MarshalIndent(jobs, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(cs.dataFile, data, 0644)
}

// GenerateJobID creates a simple unique ID from a label
func GenerateJobID(label string) string {
	return sanitizeLabel(label)
}

func sanitizeLabel(s string) string {
	result := make([]byte, 0, len(s))
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			result = append(result, byte(c))
		} else {
			result = append(result, '_')
		}
	}
	if len(result) > 20 {
		result = result[:20]
	}
	return string(result)
}
