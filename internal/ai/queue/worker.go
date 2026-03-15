package queue

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sync/semaphore"
)

// JobProcessor processes a single AI job.
type JobProcessor interface {
	Process(ctx context.Context, job *Job) error
}

// WorkerPool manages concurrent AI processing workers with a global semaphore.
type WorkerPool struct {
	sem       *semaphore.Weighted
	queue     *Queue
	processor JobProcessor
	capacity  int64
	wg        sync.WaitGroup
}

// NewWorkerPool creates a worker pool with the given concurrency limit.
func NewWorkerPool(queue *Queue, processor JobProcessor, capacity int) *WorkerPool {
	return &WorkerPool{
		sem:       semaphore.NewWeighted(int64(capacity)),
		queue:     queue,
		processor: processor,
		capacity:  int64(capacity),
	}
}

// Run starts the worker pool. It continuously dequeues jobs and processes them
// with bounded concurrency. Blocks until ctx is cancelled.
func (wp *WorkerPool) Run(ctx context.Context) error {
	slog.Info("worker pool started", "capacity", wp.capacity)

	for {
		select {
		case <-ctx.Done():
			// Wait for in-flight jobs to complete
			wp.wg.Wait()
			slog.Info("worker pool stopped")
			return nil
		default:
		}

		// Try to acquire a semaphore slot
		if err := wp.sem.Acquire(ctx, 1); err != nil {
			// Context cancelled
			wp.wg.Wait()
			return nil
		}

		// Dequeue a job
		job, err := wp.queue.Dequeue(ctx)
		if err != nil {
			wp.sem.Release(1)
			slog.Error("worker pool: dequeue error", "error", err)
			select {
			case <-ctx.Done():
				wp.wg.Wait()
				return nil
			case <-time.After(1 * time.Second):
			}
			continue
		}

		if job == nil {
			// No jobs available, release semaphore and wait briefly
			wp.sem.Release(1)
			select {
			case <-ctx.Done():
				wp.wg.Wait()
				return nil
			case <-time.After(500 * time.Millisecond):
			}
			continue
		}

		// Process job in a goroutine
		wp.wg.Add(1)
		go func(j *Job) {
			defer wp.wg.Done()
			defer wp.sem.Release(1)

			if err := wp.processor.Process(ctx, j); err != nil {
				slog.Error("worker pool: job processing failed",
					"email_id", j.EmailID,
					"user_id", j.UserID,
					"error", err,
				)
			}
		}(job)
	}
}
