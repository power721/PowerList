package thunder_share

import (
	"context"
	"testing"
)

func TestThunderShareCleanupContextSurvivesParentCancellation(t *testing.T) {
	type contextKey string
	const key contextKey = "request-id"

	parent := context.WithValue(context.Background(), key, "req-1")
	parent, cancel := context.WithCancel(parent)
	cancel()

	cleanupCtx := thunderShareCleanupContext(parent)
	if err := cleanupCtx.Err(); err != nil {
		t.Fatalf("expected cleanup context to ignore parent cancellation, got %v", err)
	}
	if got := cleanupCtx.Value(key); got != "req-1" {
		t.Fatalf("expected cleanup context to preserve values, got %v", got)
	}
}

func TestThunderShareRestoreResponseFileIDFromTask(t *testing.T) {
	resp := thunderShareRestoreResponse{
		Params: thunderShareRestoreParams{
			TraceFileIDs: `{"share-file-id":"restored-file-id"}`,
		},
	}

	id, ok := resp.RestoredFileID("share-file-id")
	if !ok {
		t.Fatal("expected restored file id from restore task")
	}
	if id != "restored-file-id" {
		t.Fatalf("expected restored file id, got %q", id)
	}
}

func TestThunderShareRestoreResponseFileIDFromTaskIgnoresMissingAndInvalidTrace(t *testing.T) {
	tests := []struct {
		name string
		resp thunderShareRestoreResponse
	}{
		{
			name: "missing source id",
			resp: thunderShareRestoreResponse{
				Params: thunderShareRestoreParams{
					TraceFileIDs: `{"other-share-file-id":"restored-file-id"}`,
				},
			},
		},
		{
			name: "invalid trace json",
			resp: thunderShareRestoreResponse{
				Params: thunderShareRestoreParams{
					TraceFileIDs: `{`,
				},
			},
		},
		{
			name: "empty restored id",
			resp: thunderShareRestoreResponse{
				Params: thunderShareRestoreParams{
					TraceFileIDs: `{"share-file-id":""}`,
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, ok := tt.resp.RestoredFileID("share-file-id")
			if ok {
				t.Fatalf("did not expect restored file id, got %q", id)
			}
		})
	}
}
