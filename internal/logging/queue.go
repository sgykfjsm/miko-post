package logging

import (
	"errors"
	"sync"
	"time"
)

// The production recorder must never wait for storage while a sink's deadline
// is running. One worker preserves admission order, with bounded backlog and
// bounded flushing. These are diagnostics limits, not delivery timeouts.
const (
	recordQueueCapacity = 256
	recordFlushTimeout  = 250 * time.Millisecond
)

var (
	errRecordQueueFull    = errors.New("diagnostic queue is full; further records discarded")
	errRecordFlushTimeout = errors.New("diagnostics did not finish within 250ms; further records discarded")
)

type recordQueue struct {
	mu       sync.Mutex
	jobs     chan func()
	done     chan struct{}
	closed   bool
	err      error
	closeErr error // published by closing done
}

func newRecordQueue(closeWriter func() error) *recordQueue {
	q := &recordQueue{jobs: make(chan func(), recordQueueCapacity), done: make(chan struct{})}
	go func() {
		defer close(q.done)
		for job := range q.jobs {
			q.mu.Lock()
			failed := q.err != nil
			q.mu.Unlock()
			if !failed {
				job()
			}
		}
		// This worker alone owns cleanup. If a Write never returns, at most
		// one worker and its open handle remain per logger. If it returns
		// late, no queued writes follow it and the handle is released here.
		q.closeErr = closeWriter()
	}()
	return q
}

func (q *recordQueue) submit(job func()) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.closed {
		return
	}
	select {
	case q.jobs <- job:
	default:
		q.failLocked(errRecordQueueFull)
	}
}

// failLocked disables this logger permanently. A timed-out write may still
// land, but retrying or writing a suffix after it could duplicate/reorder a
// post's trace. Drop queued records and retain only the in-flight operation.
func (q *recordQueue) failLocked(err error) {
	if q.err == nil {
		q.err = err
	}
	if !q.closed {
		q.closed = true
		close(q.jobs)
	}
	for range q.jobs {
	}
}

func (q *recordQueue) wait(done <-chan struct{}) error {
	q.mu.Lock()
	err := q.err
	q.mu.Unlock()
	if err != nil {
		return err
	}
	timer := time.NewTimer(recordFlushTimeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		q.mu.Lock()
		q.failLocked(errRecordFlushTimeout)
		q.mu.Unlock()
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.err
}

func (q *recordQueue) flush() error {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return q.wait(q.done)
	}
	barrier := make(chan struct{})
	select {
	case q.jobs <- func() { close(barrier) }:
	default:
		q.failLocked(errRecordQueueFull)
	}
	q.mu.Unlock()
	return q.wait(barrier)
}

func (q *recordQueue) close() error {
	q.mu.Lock()
	if !q.closed {
		q.closed = true
		close(q.jobs)
	}
	q.mu.Unlock()
	if err := q.wait(q.done); err != nil {
		return err
	}
	return q.closeErr
}
