package core

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/phrony-platform/runtime/internal/model"
	"github.com/phrony-platform/runtime/internal/store"
)

func TestToolInvocationRecorder_LookupCompleted(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	now := time.Now()
	mock.ExpectQuery(`FROM tool_invocations`).
		WithArgs("call-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"call_id", "session_id", "agent_version_id", "turn", "tool", "version", "args",
			"result", "status", "worker_identity", "image_digest", "descriptor_hash",
			"manifest_content_hash", "attempt", "error_code", "error_message",
			"usage_input_tokens", "usage_output_tokens", "usage_estimated",
			"created_at", "updated_at", "dispatched_at", "completed_at",
		}).AddRow(
			"call-1", "sess-1", "av-1", 1, "tools.echo", "v1", []byte(`{}`),
			[]byte(`{"ok":true}`), model.ToolInvocationSucceeded,
			"", "", "", "", 1, nil, nil,
			1500, 600, false,
			now, now, nil, now,
		))

	rec := NewToolInvocationRecorder(store.New(sqlDB))
	res, ok, err := rec.LookupCompleted(context.Background(), "call-1")
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected completed result")
	}
	if string(res.Payload) != `{"ok":true}` {
		t.Fatalf("payload = %s", res.Payload)
	}
	if res.Usage == nil || res.Usage.Total() != 2100 {
		t.Fatalf("usage = %+v, want 2100 total tokens", res.Usage)
	}
}
