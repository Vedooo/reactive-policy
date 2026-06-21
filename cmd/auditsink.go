/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/go-logr/logr"

	"github.com/Vedooo/reactive-policy/internal/audit/sink"
	"github.com/Vedooo/reactive-policy/internal/audit/sink/postgres"
)

func buildAuditSink(ctx context.Context, log logr.Logger, kind, dsnEnv string, queueSize int) (sink.Sink, error) {
	switch kind {
	case "", "none":
		return sink.Noop{}, nil
	case "postgres":
		dsn := os.Getenv(dsnEnv)
		if dsn == "" {
			log.Info("audit-sink=postgres but DSN env is empty; falling back to noop until the operator restarts with the DSN set", "envVar", dsnEnv)
			return sink.Noop{}, nil
		}
		return postgres.New(ctx, dsn, postgres.Config{QueueSize: queueSize, Logger: log})
	default:
		return nil, fmt.Errorf("unknown audit-sink %q (want one of: none, postgres)", kind)
	}
}
