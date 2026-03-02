// internal/transfer/engine.go
package transfer

import (
	"context"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"sync"
	"time"

	"github.com/HopperShell/ferry/internal/fs"
)

const (
	tempSuffix     = ".ferry-tmp"
	maxRetries     = 2
	defaultRetryDelay = 500 * time.Millisecond
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
	retries int // number of retries attempted so far
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
	jobs        []*Job
	progress    chan ProgressEvent
	maxWorkers  int
	resumable   bool
	retryDelay  time.Duration
	mu          sync.Mutex
	ctx         context.Context
	cancel      context.CancelFunc
	nextID      int
	enqueueDone bool // set by Done() to signal no more jobs coming
}

// NewEngine creates a new transfer engine with the given concurrency.
// When resumable is true, completed files (same size+mtime) are skipped
// and writes use a temp file with atomic rename.
func NewEngine(maxWorkers int, resumable bool) *Engine {
	if maxWorkers < 1 {
		maxWorkers = 1
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Engine{
		progress:   make(chan ProgressEvent, 64),
		maxWorkers: maxWorkers,
		resumable:  resumable,
		retryDelay: defaultRetryDelay,
		ctx:        ctx,
		cancel:     cancel,
	}
}

// SetRetryDelay overrides the delay between retries (useful for testing).
func (e *Engine) SetRetryDelay(d time.Duration) {
	e.retryDelay = d
}

// Enqueue adds a job to the queue.
func (e *Engine) Enqueue(job *Job) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.nextID++
	job.ID = fmt.Sprintf("job-%d", e.nextID)
	e.jobs = append(e.jobs, job)
}

// EnqueueMkdir creates a destination directory and records it as an
// immediately-completed job so it appears in the transfer overlay.
func (e *Engine) EnqueueMkdir(name string, dstPath string, dstFS fs.FileSystem) {
	_ = dstFS.Mkdir(dstPath, 0o755)
	e.mu.Lock()
	e.nextID++
	job := &Job{
		ID:      fmt.Sprintf("job-%d", e.nextID),
		Name:    name + "/",
		DstPath: dstPath,
		DstFS:   dstFS,
		Status:  JobCompleted,
	}
	e.jobs = append(e.jobs, job)
	evt := ProgressEvent{
		JobID: job.ID,
		Name:  job.Name,
		Done:  true,
	}
	e.mu.Unlock()
	// Send progress AFTER releasing the lock to avoid deadlock with Start().
	e.progress <- evt
}

// EnqueueEntry enqueues a transfer for a single fs.Entry. srcBase is the
// directory that contains the entry; the entry's relative path from srcBase
// is preserved on the destination side under dstBase.
// If the entry is a directory, it walks it and enqueues each file individually.
func (e *Engine) EnqueueEntry(entry fs.Entry, srcFS fs.FileSystem, srcBase string, dstFS fs.FileSystem, dstBase string) {
	// Compute the relative path from srcBase to preserve directory structure.
	relPath, err := filepath.Rel(srcBase, entry.Path)
	if err != nil || relPath == "." {
		relPath = entry.Name
	}

	if !entry.IsDir {
		e.Enqueue(&Job{
			Name:    relPath,
			SrcPath: entry.Path,
			SrcFS:   srcFS,
			DstPath: filepath.Join(dstBase, relPath),
			DstFS:   dstFS,
			Size:    entry.Size,
		})
		return
	}

	// If the destination already ends with the same directory name,
	// merge into it instead of creating a nested subdirectory.
	// e.g. copying "Test/" into "/root/Test" → merge into "/root/Test",
	// not "/root/Test/Test".
	dstDir := filepath.Join(dstBase, relPath)
	if filepath.Base(dstBase) == entry.Name {
		dstDir = dstBase
	}
	e.walkAndEnqueue(srcFS, entry.Path, dstFS, dstDir, relPath)
}

func (e *Engine) walkAndEnqueue(srcFS fs.FileSystem, srcDir string, dstFS fs.FileSystem, dstDir string, displayPrefix string) {
	// Create destination directory and track it.
	e.EnqueueMkdir(displayPrefix, dstDir, dstFS)

	entries, err := srcFS.List(srcDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		childDisplay := filepath.Join(displayPrefix, entry.Name)
		if entry.IsDir {
			e.walkAndEnqueue(srcFS, entry.Path, dstFS, filepath.Join(dstDir, entry.Name), childDisplay)
		} else {
			e.Enqueue(&Job{
				Name:    childDisplay,
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
// It processes all pending jobs and then waits for any that were added
// while running (via EnqueueEntry from another goroutine). Call Done()
// to signal that no more jobs will be added.
func (e *Engine) Start() {
	defer close(e.progress)

	var wg sync.WaitGroup
	sem := make(chan struct{}, e.maxWorkers)

	for {
		e.mu.Lock()
		pending := e.pendingJobs()
		done := e.enqueueDone
		e.mu.Unlock()

		if len(pending) == 0 {
			if done {
				break
			}
			// Wait briefly for more jobs to arrive.
			select {
			case <-e.ctx.Done():
				wg.Wait()
				return
			case <-time.After(50 * time.Millisecond):
				continue
			}
		}

		for _, job := range pending {
			select {
			case <-e.ctx.Done():
				wg.Wait()
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
	}

	wg.Wait()
}

// Done signals that no more jobs will be enqueued. Start() will finish
// once all pending jobs are processed.
func (e *Engine) Done() {
	e.mu.Lock()
	e.enqueueDone = true
	e.mu.Unlock()
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

// IsFinished returns true when Done() has been called and no jobs are pending or active.
func (e *Engine) IsFinished() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.enqueueDone {
		return false
	}
	for _, j := range e.jobs {
		if j.Status == JobPending || j.Status == JobActive {
			return false
		}
	}
	return true
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

// failJob marks a job as failed, or requeues it for retry if retries remain.
// Returns true if the job will be retried.
func (e *Engine) failJob(j *Job, err error) bool {
	if j.retries < maxRetries {
		j.retries++
		e.mu.Lock()
		j.Status = JobPending
		j.Err = nil
		e.mu.Unlock()
		time.Sleep(e.retryDelay)
		return true
	}
	e.mu.Lock()
	j.Status = JobFailed
	j.Err = err
	e.mu.Unlock()
	e.progress <- ProgressEvent{
		JobID: j.ID,
		Name:  j.Name,
		Done:  true,
		Err:   err,
	}
	return false
}

func (e *Engine) runJob(j *Job) {
	e.mu.Lock()
	j.Status = JobActive
	e.mu.Unlock()

	// Stat source for size and metadata.
	var srcStat fs.Entry
	if stat, err := j.SrcFS.Stat(j.SrcPath); err == nil {
		srcStat = stat
		if j.Size == 0 {
			j.Size = stat.Size
		}
	} else if j.Size == 0 {
		if stat, err := j.SrcFS.Stat(j.SrcPath); err == nil {
			j.Size = stat.Size
		}
	}

	// Skip completed files when resumable: same size and mtime (2s tolerance).
	if e.resumable {
		if dstStat, err := j.DstFS.Stat(j.DstPath); err == nil {
			if dstStat.Size == srcStat.Size && !srcStat.ModTime.IsZero() &&
				math.Abs(dstStat.ModTime.Sub(srcStat.ModTime).Seconds()) < 2 {
				e.mu.Lock()
				j.Status = JobCompleted
				e.mu.Unlock()
				e.progress <- ProgressEvent{
					JobID:      j.ID,
					Name:       j.Name,
					BytesSent:  j.Size,
					TotalBytes: j.Size,
					Done:       true,
				}
				return
			}
		}
	}

	// Stream source → destination through a pipe with progress tracking.
	pr, pw := io.Pipe()

	// Ensure destination directory exists.
	dstDir := filepath.Dir(j.DstPath)
	_ = j.DstFS.Mkdir(dstDir, 0o755)

	// Determine permissions.
	perm := srcStat.Mode
	if perm == 0 {
		perm = 0o644
	}

	// Choose write path: use temp file for resumable transfers.
	writePath := j.DstPath
	if e.resumable {
		writePath = j.DstPath + tempSuffix
	}

	// Read source in a goroutine, streaming into the pipe.
	var readErr error
	var readWg sync.WaitGroup
	readWg.Add(1)
	go func() {
		defer readWg.Done()
		readErr = j.SrcFS.Read(j.SrcPath, pw)
		pw.CloseWithError(readErr)
	}()

	// Write from the pipe through a progress reader to the destination.
	progReader := NewProgressReader(pr, j.Size, j.ID, j.Name, e.progress)
	err := j.DstFS.Write(writePath, progReader, perm)

	// Wait for the read side to finish.
	readWg.Wait()

	if readErr != nil && err == nil {
		err = readErr
	}
	if err != nil {
		if e.resumable {
			_ = j.DstFS.Remove(writePath) // clean up temp file
		}
		e.failJob(j, fmt.Errorf("transfer: %w", err))
		return
	}

	// Atomic rename for resumable transfers.
	if e.resumable {
		if err := j.DstFS.Rename(writePath, j.DstPath); err != nil {
			_ = j.DstFS.Remove(writePath)
			e.failJob(j, fmt.Errorf("rename temp: %w", err))
			return
		}
	}

	// Preserve source modification time on the destination.
	if !srcStat.ModTime.IsZero() {
		_ = j.DstFS.Chtimes(j.DstPath, srcStat.ModTime)
	}

	// Emit final progress event.
	progReader.Finish()

	e.mu.Lock()
	j.Status = JobCompleted
	e.mu.Unlock()
}
