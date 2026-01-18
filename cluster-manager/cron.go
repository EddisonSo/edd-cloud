package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

type CronJob struct {
	ID         string       `json:"id"`
	Name       string       `json:"name"`
	Schedule   CronSchedule `json:"schedule"`
	Command    string       `json:"command"`
	Enabled    bool         `json:"enabled"`
	LastRun    *time.Time   `json:"last_run,omitempty"`
	LastStatus string       `json:"last_status,omitempty"`
	CreatedAt  time.Time    `json:"created_at"`
}

type CronSchedule struct {
	Second  string `json:"second"`  // 0-59 or *
	Minute  string `json:"minute"`  // 0-59 or *
	Hour    string `json:"hour"`    // 0-23 or *
	Day     string `json:"day"`     // 1-31 or *
	Month   string `json:"month"`   // 1-12 or *
	Weekday string `json:"weekday"` // 0-6 or *
}

type cronStore struct {
	mu   sync.RWMutex
	jobs map[string]*CronJob
	path string
}

var store *cronStore

func initCronStorage() error {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	store = &cronStore{
		jobs: make(map[string]*CronJob),
		path: filepath.Join(dataDir, "crons.json"),
	}

	return store.load()
}

func (s *cronStore) load() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	var jobs []*CronJob
	if err := json.Unmarshal(data, &jobs); err != nil {
		return err
	}

	for _, job := range jobs {
		s.jobs[job.ID] = job
	}
	return nil
}

func (s *cronStore) save() error {
	jobs := make([]*CronJob, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, job)
	}

	data, err := json.MarshalIndent(jobs, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(s.path, data, 0644)
}

func (s *cronStore) list() []*CronJob {
	s.mu.RLock()
	defer s.mu.RUnlock()

	jobs := make([]*CronJob, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, job)
	}
	return jobs
}

func (s *cronStore) get(id string) (*CronJob, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[id]
	return job, ok
}

func (s *cronStore) create(job *CronJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	job.ID = uuid.New().String()
	job.CreatedAt = time.Now()
	s.jobs[job.ID] = job

	if err := s.save(); err != nil {
		delete(s.jobs, job.ID)
		return err
	}

	if job.Enabled {
		syncCrontab()
	}
	return nil
}

func (s *cronStore) update(id string, job *CronJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.jobs[id]
	if !ok {
		return fmt.Errorf("job not found")
	}

	job.ID = id
	job.CreatedAt = existing.CreatedAt
	job.LastRun = existing.LastRun
	job.LastStatus = existing.LastStatus
	s.jobs[id] = job

	if err := s.save(); err != nil {
		s.jobs[id] = existing
		return err
	}

	syncCrontab()
	return nil
}

func (s *cronStore) delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	existing, ok := s.jobs[id]
	if !ok {
		return fmt.Errorf("job not found")
	}

	delete(s.jobs, id)

	if err := s.save(); err != nil {
		s.jobs[id] = existing
		return err
	}

	syncCrontab()
	return nil
}

func (s *cronStore) updateRunStatus(id string, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if job, ok := s.jobs[id]; ok {
		now := time.Now()
		job.LastRun = &now
		job.LastStatus = status
		s.save()
	}
}

func getCronCount() int {
	if store == nil {
		return 0
	}
	store.mu.RLock()
	defer store.mu.RUnlock()
	return len(store.jobs)
}

func handleCronList(w http.ResponseWriter, r *http.Request) {
	jobs := store.list()
	writeJSON(w, jobs)
}

func handleCronCreate(w http.ResponseWriter, r *http.Request) {
	var job CronJob
	if err := json.NewDecoder(r.Body).Decode(&job); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	if job.Name == "" || job.Command == "" {
		http.Error(w, "name and command required", http.StatusBadRequest)
		return
	}

	if err := validateSchedule(&job.Schedule); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := store.create(&job); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, job)
}

func handleCronUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}

	var job CronJob
	if err := json.NewDecoder(r.Body).Decode(&job); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}

	if job.Name == "" || job.Command == "" {
		http.Error(w, "name and command required", http.StatusBadRequest)
		return
	}

	if err := validateSchedule(&job.Schedule); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := store.update(id, &job); err != nil {
		if err.Error() == "job not found" {
			http.Error(w, err.Error(), http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	updatedJob, _ := store.get(id)
	writeJSON(w, updatedJob)
}

func handleCronDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}

	if err := store.delete(id); err != nil {
		if err.Error() == "job not found" {
			http.Error(w, err.Error(), http.StatusNotFound)
		} else {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}

	writeJSON(w, map[string]string{"status": "ok"})
}

func handleCronRun(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.Error(w, "id required", http.StatusBadRequest)
		return
	}

	job, ok := store.get(id)
	if !ok {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}

	// Run the command asynchronously
	go func() {
		cmd := exec.Command("chroot", hostRoot, "/bin/sh", "-c", job.Command)
		err := cmd.Run()
		status := "success"
		if err != nil {
			status = "failed: " + err.Error()
		}
		store.updateRunStatus(id, status)
	}()

	writeJSON(w, map[string]string{"status": "started"})
}

func validateSchedule(s *CronSchedule) error {
	if s.Second == "" {
		s.Second = "0"
	}
	if s.Minute == "" {
		s.Minute = "*"
	}
	if s.Hour == "" {
		s.Hour = "*"
	}
	if s.Day == "" {
		s.Day = "*"
	}
	if s.Month == "" {
		s.Month = "*"
	}
	if s.Weekday == "" {
		s.Weekday = "*"
	}
	return nil
}

// syncCrontab syncs enabled cron jobs to the host's crontab
func syncCrontab() {
	store.mu.RLock()
	defer store.mu.RUnlock()

	var lines []string
	lines = append(lines, "# Managed by cluster-manager - DO NOT EDIT")

	for _, job := range store.jobs {
		if !job.Enabled {
			continue
		}
		// Standard cron format: min hour day month weekday command
		line := fmt.Sprintf("%s %s %s %s %s %s # cluster-manager:%s",
			job.Schedule.Minute,
			job.Schedule.Hour,
			job.Schedule.Day,
			job.Schedule.Month,
			job.Schedule.Weekday,
			job.Command,
			job.ID,
		)
		lines = append(lines, line)
	}

	crontabPath := filepath.Join(dataDir, "crontab")
	content := ""
	for _, line := range lines {
		content += line + "\n"
	}
	os.WriteFile(crontabPath, []byte(content), 0644)

	// Install to host crontab
	hostCrontab := filepath.Join(hostRoot, "var/spool/cron/crontabs/root")
	os.MkdirAll(filepath.Dir(hostCrontab), 0755)
	os.WriteFile(hostCrontab, []byte(content), 0600)
}
