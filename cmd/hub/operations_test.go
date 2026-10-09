package main

import (
	"strings"
	"testing"
)

func TestOperationsCLIResponseRejectsSensitiveExtrasAndUnsafeMetadata(t *testing.T) {
	for _, sample := range []struct{ op, raw string }{
		{"recovery.status", `{"revision":1,"recovery_closed":true,"audit_gap":false,"mode":"fixture","value":"fake-secret"}`},
		{"recovery.status", `{"revision":1,"recovery_closed":true,"audit_gap":false,"mode":"fake-sensitive"}`},
		{"recovery.open", `{"revision":2,"recovery_closed":true,"audit_gap":false,"mode":"fixture"}`},
		{"operations.backup", `{"backup_id":"/fake/private/path","revision":1,"audit_gap":false,"created_at":"2026-01-01T00:00:00Z","mode":"fixture"}`},
		{"operations.retention", `{"removed":-1,"cutoff":"2025-01-01T00:00:00Z","mode":"fixture"}`},
		{"operations.retention", `{"removed":0,"cutoff":"fake-private","mode":"fixture"}`},
	} {
		raw, _, err := validateResponse(strings.NewReader(sample.raw), sample.op)
		if err == nil || raw != nil {
			t.Fatalf("accepted unsafe %s", sample.op)
		}
	}
	for _, sample := range []struct{ op, raw string }{
		{"recovery.status", `{"revision":1,"recovery_closed":true,"audit_gap":false,"mode":"fixture"}`},
		{"recovery.open", `{"revision":2,"recovery_closed":false,"audit_gap":true,"mode":"fixture"}`},
		{"operations.backup", `{"backup_id":"backup-ABCDEFGHIJKLMNOPQRSTUVWXYZ","revision":1,"audit_gap":false,"created_at":"2026-01-01T00:00:00Z","mode":"fixture"}`},
		{"operations.retention", `{"removed":0,"cutoff":"2025-01-01T00:00:00Z","mode":"fixture"}`},
	} {
		_, exit, err := validateResponse(strings.NewReader(sample.raw), sample.op)
		if err != nil || exit != 0 {
			t.Fatalf("rejected safe %s: %v", sample.op, err)
		}
	}
}
