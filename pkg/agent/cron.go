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

	"littleclaw/pkg/bus"
	"littleclaw/pkg/memory"

	"github.com/robfig/cron/v3"
)

// CronJob represents a single scheduled task persisted to disk.
type CronJob struct {
	ID       string `json:"id"`
	Schedule string `json:"schedule"` // robfig cron expression, e.g. "@every 10s" or "*/5 * * * *"
	Command  string `json:"command"`  // shell command OR description for the LLM to run in exec
	ChatID   string `json:"chat_id"`  // Telegram chat ID to reply to
	Channel  string `json:"channel"`  // channel to respond on (e.g. "telegram")
	Label    string `json:"label"`    // human-readable label shown to user
}

// CronService manages persistent, file-backed cron jobs and runs them on schedule.
type CronService struct {
	mu           sync.Mutex
	jobs         map[string]*CronJob
	entryIDs     map[string]cron.EntryID
	cronRunner   *cron.Cron
	dataFile     string // absolute path to CRON.json
	workspaceDir string
	msgBus       *bus.MessageBus
	memStore     *memory.Store
}

// NewCronService creates a CronService backed by $workspace/CRON.json.
func NewCronService(workspaceDir string, msgBus *bus.MessageBus, mem *memory.Store) *CronService {
	dataFile := filepath.Join(workspaceDir, "CRON.json")
	return &CronService{
		jobs:         make(map[string]*CronJob),
		entryIDs:     make(map[string]cron.EntryID),
		cronRunner:   cron.New(cron.WithSeconds()),
		dataFile:     dataFile,
		workspaceDir: workspaceDir,
		msgBus:       msgBus,
		memStore:     mem,
	}
}

// Start loads persisted jobs and begins the cron scheduler.
func (cs *CronService) Start(ctx context.Context) error {
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

// AddJob adds a new cron job (or replaces an existing one with the same label), persists it, and schedules it.
func (cs *CronService) AddJob(job *CronJob) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	// If a job with this ID (label) already exists, remove it first (un-schedule)
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
		log.Printf("⏰ CronService: firing job %s (%s)\n", job.ID, job.Label)

		cmd := exec.Command("sh", "-c", job.Command)
		cmd.Dir = cs.workspaceDir

		output, err := cmd.CombinedOutput()
		
		var msg string
		if err != nil {
			msg = fmt.Sprintf("⚠️ Cron job `%s` failed:\n```\n%s\n```", job.Label, output)
		} else {
			trimmed := string(output)
			if trimmed == "" {
				trimmed = "(no output)"
			}
			msg = trimmed
		}

		// Send result to the user's Telegram chat
		if job.ChatID != "" && job.Channel != "" {
			cs.msgBus.SendOutbound(bus.OutboundMessage{
				Channel: job.Channel,
				ChatID:  job.ChatID,
				Content: msg,
			})
		}

		// Log to INTERNAL.md for agent reflection
		logMsg := fmt.Sprintf("[Cron Job Runtime] Job '%s' (%s) fired. Result: %s", job.Label, job.ID, msg)
		cs.memStore.AppendInternal("CRON", logMsg)
	}
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
