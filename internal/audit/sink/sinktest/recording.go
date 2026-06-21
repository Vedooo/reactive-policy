/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

// Package sinktest is a recording sink.Sink for tests.
package sinktest

import (
	"context"
	"errors"
	"sync"

	"github.com/Vedooo/reactive-policy/internal/audit/sink"
)

// Recording captures every call for assertion.
type Recording struct {
	mu       sync.Mutex
	triggers [][]sink.Event
	reverts  [][]sink.RevertEvent
	closed   bool

	// FailNext, if set, is returned from the next RecordTrigger then cleared.
	FailNext error
}

func New() *Recording { return &Recording{} }

func (r *Recording) RecordTrigger(_ context.Context, events []sink.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.FailNext; err != nil {
		r.FailNext = nil
		return err
	}
	r.triggers = append(r.triggers, append([]sink.Event(nil), events...))
	return nil
}

func (r *Recording) RecordRevert(_ context.Context, events []sink.RevertEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reverts = append(r.reverts, append([]sink.RevertEvent(nil), events...))
	return nil
}

func (r *Recording) Close(context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	return nil
}

func (r *Recording) Triggers() [][]sink.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]sink.Event(nil), r.triggers...)
}

func (r *Recording) Reverts() [][]sink.RevertEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]sink.RevertEvent(nil), r.reverts...)
}

func (r *Recording) Closed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

var ErrInjected = errors.New("sinktest: injected failure")
