// internal/transfer/progress.go
package transfer

import (
	"io"
	"time"
)

const (
	emitInterval = 100 * time.Millisecond
	emitBytes    = 64 * 1024 // 64KB
	windowSize   = 3 * time.Second
)

type speedSample struct {
	bytes int64
	time  time.Time
}

// ProgressReader wraps an io.Reader and emits ProgressEvents as bytes are read.
type ProgressReader struct {
	reader       io.Reader
	total        int64
	read         int64
	jobID        string
	name         string
	progress     chan<- ProgressEvent
	startTime    time.Time
	samples      []speedSample
	lastEmit     time.Time
	lastEmitRead int64
}

// NewProgressReader creates a ProgressReader wrapping the given reader.
func NewProgressReader(r io.Reader, total int64, jobID, name string, progress chan<- ProgressEvent) *ProgressReader {
	now := time.Now()
	return &ProgressReader{
		reader:    r,
		total:     total,
		jobID:     jobID,
		name:      name,
		progress:  progress,
		startTime: now,
		lastEmit:  now,
	}
}

// Read implements io.Reader, tracking bytes and emitting progress events.
func (pr *ProgressReader) Read(p []byte) (int, error) {
	n, err := pr.reader.Read(p)
	if n > 0 {
		pr.read += int64(n)
		now := time.Now()

		// Record sample for speed calculation.
		pr.samples = append(pr.samples, speedSample{
			bytes: pr.read,
			time:  now,
		})

		// Prune old samples outside the sliding window.
		cutoff := now.Add(-windowSize)
		start := 0
		for start < len(pr.samples) && pr.samples[start].time.Before(cutoff) {
			start++
		}
		// Keep at least one old sample for speed calculation.
		if start > 0 {
			start--
		}
		pr.samples = pr.samples[start:]

		// Check if we should emit a progress event.
		bytesDelta := pr.read - pr.lastEmitRead
		timeDelta := now.Sub(pr.lastEmit)
		if bytesDelta >= emitBytes || timeDelta >= emitInterval {
			pr.emit(now, false)
		}
	}

	if err == io.EOF {
		pr.emit(time.Now(), true)
	}

	return n, err
}

// Finish emits a final done event.
func (pr *ProgressReader) Finish() {
	pr.emit(time.Now(), true)
}

func (pr *ProgressReader) emit(now time.Time, done bool) {
	speed := pr.calcSpeed(now)
	pr.lastEmit = now
	pr.lastEmitRead = pr.read

	pr.progress <- ProgressEvent{
		JobID:      pr.jobID,
		Name:       pr.name,
		BytesSent:  pr.read,
		TotalBytes: pr.total,
		Speed:      speed,
		Done:       done,
	}
}

func (pr *ProgressReader) calcSpeed(now time.Time) float64 {
	if len(pr.samples) < 2 {
		elapsed := now.Sub(pr.startTime).Seconds()
		if elapsed <= 0 {
			return 0
		}
		return float64(pr.read) / elapsed
	}

	oldest := pr.samples[0]
	newest := pr.samples[len(pr.samples)-1]
	dt := newest.time.Sub(oldest.time).Seconds()
	if dt <= 0 {
		return 0
	}
	db := newest.bytes - oldest.bytes
	return float64(db) / dt
}
