/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package postgres is a sink.Sink that persists audit events to Postgres
// asynchronously. The operator's reconcile path enqueues batches and a single
// background worker drains them; on full queue or write failure the batch is
// dropped and logged — the ActionAudit CRD remains source of truth.
package postgres

import (
	"context"
	"errors"
	"sync"

	"github.com/go-logr/logr"

	"github.com/Vedooo/reactive-policy/internal/audit/sink"
)

// ErrBackpressure is returned when the in-process queue is full.
var ErrBackpressure = errors.New("audit sink: queue full, dropping batch")

// Config controls the async worker.
type Config struct {
	QueueSize int
	Logger    logr.Logger
}

// DefaultConfig returns sensible defaults for the sink.
func DefaultConfig() Config { return Config{QueueSize: 1024} }

type writer interface {
	writeTriggers(ctx context.Context, events []sink.Event) error
	writeReverts(ctx context.Context, events []sink.RevertEvent) error
	close(ctx context.Context) error
}

type workItem struct {
	triggers []sink.Event
	reverts  []sink.RevertEvent
}

// Sink is the async Postgres audit sink.
type Sink struct {
	w         writer
	queue     chan workItem
	logger    logr.Logger
	wg        sync.WaitGroup
	closeOnce sync.Once
}

func newSink(w writer, cfg Config) *Sink {
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = DefaultConfig().QueueSize
	}
	s := &Sink{
		w:      w,
		queue:  make(chan workItem, cfg.QueueSize),
		logger: cfg.Logger,
	}
	s.wg.Add(1)
	go s.run()
	return s
}

// RecordTrigger enqueues a non-empty trigger batch. Returns ErrBackpressure if
// the queue is full.
func (s *Sink) RecordTrigger(_ context.Context, events []sink.Event) error {
	if len(events) == 0 {
		return nil
	}
	select {
	case s.queue <- workItem{triggers: events}:
		return nil
	default:
		return ErrBackpressure
	}
}

// RecordRevert enqueues a non-empty revert batch.
func (s *Sink) RecordRevert(_ context.Context, events []sink.RevertEvent) error {
	if len(events) == 0 {
		return nil
	}
	select {
	case s.queue <- workItem{reverts: events}:
		return nil
	default:
		return ErrBackpressure
	}
}

// Close stops accepting new batches and waits for in-flight writes to finish
// or for ctx to expire. The underlying connection is closed last.
func (s *Sink) Close(ctx context.Context) error {
	s.closeOnce.Do(func() { close(s.queue) })

	drained := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(drained)
	}()
	select {
	case <-drained:
	case <-ctx.Done():
		return ctx.Err()
	}
	return s.w.close(ctx)
}

func (s *Sink) run() {
	defer s.wg.Done()
	for item := range s.queue {
		ctx := context.Background()
		if len(item.triggers) > 0 {
			if err := s.w.writeTriggers(ctx, item.triggers); err != nil {
				s.logger.Error(err, "audit sink: writing triggers", "count", len(item.triggers))
			}
		}
		if len(item.reverts) > 0 {
			if err := s.w.writeReverts(ctx, item.reverts); err != nil {
				s.logger.Error(err, "audit sink: writing reverts", "count", len(item.reverts))
			}
		}
	}
}
