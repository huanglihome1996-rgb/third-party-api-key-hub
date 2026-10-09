package application

import (
	"context"
	"crypto/rand"
	v1 "github.com/third-party-api-key-hub/internal/contract/v1"
	"time"
)

// RecordBoundaryRejection contains no input bytes and only admits stable error categories.
func (e *Engine) RecordBoundaryRejection(ctx context.Context, actor Actor, code v1.ErrorCode) {
	safe := v1.Fail(code).Response.Error
	event := AuditEvent{Time: time.Now().UTC(), UID: actor.UID, ActorName: actor.Name, Operation: "local.reject", Target: "unknown", Result: "failure", Error: string(safe), OperationID: "op-" + rand.Text()}
	aCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 500*time.Millisecond)
	defer cancel()
	if e.audit.Record(aCtx, event) != nil {
		e.degraded.Store(true)
	}
}
