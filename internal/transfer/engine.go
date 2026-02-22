// internal/transfer/engine.go
package transfer

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"sync"

	"github.com/andrewstuart/ferry/internal/fs"
)

// JobStatus represents the state of a transfer job.
type JobStatus int

const (
	JobPending   JobStatus = iota
	JobActive
	JobCompleted
	JobFailed
)

// Job represents a single file transfer operation.
type Job struct {
	ID      string
	Name    string // filename for display
	SrcPath string
	SrcFS   fs.FileSystem
	DstPath string
	DstFS   fs.FileSystem
	Size    int64
	Status  JobStatus
	Err     error
}

// ProgressEvent reports progress on a single transfer job.
type ProgressEvent struct {
	JobID      string
	Name       string
	BytesSent  int64
	TotalBytes int64
	Speed      float64 // bytes/sec
	Done       bool
	Err        error
}

// Engine manages a queue of file transfer jobs with concurrent execution.
type Engine struct {
	jobs       []*Job
	progress   chan ProgressEvent
	maxWorkers int
	mu         sync.Mutex
	ctx        context.Context
	cancel     context.CancelFunc
	nextID     int
}

// NewEngine creates a new transfer engine with the given concurrency.
func NewEngine(maxWorkers int) *Engine {
	if maxWorkers < 1 {
		maxWorkers = 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Engine{
		progress:   make(chan ProgressEvent, 64),
		maxWorkers: maxWorkers,
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Enqueue adds a job to the queue. If the entry is a directory, it walks it
// recursively and enqueues individual file jobs.
func (e *Engine) Enqueue(job *Job) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.nextID++
	job.ID = fmt.Sprintf("job-%d", e.nextID)
	e.jobs = append(e.jobs, job)
}

// EnqueueEntry enqueues a transfer for a single fs.Entry. If the entry is a
// directory, it walks it and enqueues each file individually.
func (e *Engine) EnqueueEntry(entry fs.Entry, srcFS fs.FileSystem, srcBase string, dstFS fs.FileSystem, dstBase string) {
	if !entry.IsDir {
		e.Enqueue(&Job{
			Name:    entry.Name,
			SrcPath: entry.Path,
			SrcFS:   srcFS,
			DstPath: filepath.Join(dstBase, entry.Name),
			DstFS:   dstFS,
			Size:    entry.Size,
		})
		return
	}
	// Walk directory recursively.
	e.walkAndEnqueue(srcFS, entry.Path, dstFS, dstBase, entry.Name)
}

func (e *Engine) walkAndEnqueue(srcFS fs.FileSystem, srcDir string, dstFS fs.FileSystem, dstBase string, dirName string) {
	dstDir := filepath.Join(dstBase, dirName)
	// Create destination directory.
	_ = dstFS.Mkdir(dstDir, 0o755)

	entries, err := srcFS.List(srcDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir {
			e.walkAndEnqueue(srcFS, entry.Path, dstFS, dstDir, entry.Name)
		} else {
			e.Enqueue(&Job{
				Name:    entry.Name,
				SrcPath: entry.Path,
				SrcFS:   srcFS,
				DstPath: filepath.Join(dstDir, entry.Name),
				DstFS:   dstFS,
				Size:    entry.Size,
			})
		}
	}
}

// Start begins processing enqueued jobs with up to maxWorkers concurrency.
func (e *Engine) Start() {
	e.mu.Lock()
	pending := e.pendingJobs()
	e.mu.Unlock()

	var wg sync.WaitGroup
	sem := make(chan struct{}, e.maxWorkers)

	for _, job := range pending {
		select {
		case <-e.ctx.Done():
			return
		default:
		}

		sem <- struct{}{}
		wg.Add(1)
		go func(j *Job) {
			defer wg.Done()
			defer func() { <-sem }()
			e.runJob(j)
		}(job)
	}

	wg.Wait()
}

// Cancel cancels all active transfers.
func (e *Engine) Cancel() {
	e.cancel()
}

// Jobs returns the current job list.
func (e *Engine) Jobs() []*Job {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]*Job, len(e.jobs))
	copy(out, e.jobs)
	return out
}

// Progress returns a read-only channel for progress updates.
func (e *Engine) Progress() <-chan ProgressEvent {
	return e.progress
}

// ActiveCount returns the number of currently active jobs.
func (e *Engine) ActiveCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	count := 0
	for _, j := range e.jobs {
		if j.Status == JobActive {
			count++
		}
	}
	return count
}

// Clear removes completed and failed jobs from the list.
func (e *Engine) Clear() {
	e.mu.Lock()
	defer e.mu.Unlock()
	var kept []*Job
	for _, j := range e.jobs {
		if j.Status == JobPending || j.Status == JobActive {
			kept = append(kept, j)
		}
	}
	e.jobs = kept
}

func (e *Engine) pendingJobs() []*Job {
	var out []*Job
	for _, j := range e.jobs {
		if j.Status == JobPending {
			out = append(out, j)
		}
	}
	return out
}

func (e *Engine) runJob(j *Job) {
	e.mu.Lock()
	j.Status = JobActive
	e.mu.Unlock()

	// Stat source for size if not already set.
	if j.Size == 0 {
		if stat, err := j.SrcFS.Stat(j.SrcPath); err == nil {
			j.Size = stat.Size
		}
	}

	// Read source into a buffer through a progress reader.
	var buf bytes.Buffer
	pr := NewProgressReader(&buf, j.Size, j.ID, j.Name, e.progress)

	// Read source file, writing to our progress-tracking buffer.
	err := j.SrcFS.Read(j.SrcPath, &buf)
	if err != nil {
		e.mu.Lock()
		j.Status = JobFailed
		j.Err = fmt.Errorf("read source: %w", err)
		e.mu.Unlock()
		e.progress <- ProgressEvent{
			JobID: j.ID,
			Name:  j.Name,
			Done:  true,
			Err:   j.Err,
		}
		return
	}

	// Now write from progress reader to destination.
	perm := fs.Entry{}.Mode
	if perm == 0 {
		perm = 0o644
	}
	// Stat for permissions.
	if stat, err := j.SrcFS.Stat(j.SrcPath); err == nil {
		perm = stat.Mode
	}

	err = j.DstFS.Write(j.DstPath, pr, perm)
	if err != nil {
		e.mu.Lock()
		j.Status = JobFailed
		j.Err = fmt.Errorf("write dest: %w", err)
		e.mu.Unlock()
		e.progress <- ProgressEvent{
			JobID: j.ID,
			Name:  j.Name,
			Done:  true,
			Err:   j.Err,
		}
		return
	}

	// Emit final progress event.
	pr.Finish()

	e.mu.Lock()
	j.Status = JobCompleted
	e.mu.Unlock()
}
