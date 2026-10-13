package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestOccupiedSessionWithInstalledCodex(t *testing.T) {
	threadID := os.Getenv("CODEX_REMOTE_READONLY_TEST_THREAD")
	if threadID == "" {
		t.Skip("set CODEX_REMOTE_READONLY_TEST_THREAD to verify read-only fallback for an occupied session")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := StartCodex(ctx, "codex", slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	result, err := resumeOrReadThread(ctx, threadID, client.Call)
	if err != nil {
		t.Fatal(err)
	}
	thread, _ := result["thread"].(map[string]any)
	turns, _ := thread["turns"].([]any)
	if result["readOnly"] != true || thread["id"] != threadID || len(turns) == 0 {
		t.Fatalf("expected readable occupied session, got readOnly=%v, thread=%v, turns=%d", result["readOnly"], thread["id"], len(turns))
	}
	t.Logf("Occupied session remained owned by its other client; read %d history turns in read-only mode.", len(turns))
}

func TestOccupiedThreadFallsBackToReadingHistory(t *testing.T) {
	var methods []string
	history := []any{map[string]any{"items": []any{map[string]any{"type": "agentMessage", "text": "history remains visible"}}}}
	result, err := resumeOrReadThread(context.Background(), "thread-1", func(_ context.Context, method string, params, target any) error {
		methods = append(methods, method)
		if method == "thread/resume" {
			return errors.New(`app-server thread/resume: {"code":-32600,"message":"thread thread-1 already has an active writer"}`)
		}
		if params.(map[string]any)["includeTurns"] != true {
			t.Fatal("read-only fallback did not request history")
		}
		*target.(*map[string]any) = map[string]any{"thread": map[string]any{"id": "thread-1", "turns": history}}
		return nil
	})
	if err != nil || result["readOnly"] != true || !reflect.DeepEqual(methods, []string{"thread/resume", "thread/read"}) {
		t.Fatalf("unexpected occupied thread result: %#v, %v, %#v", result, err, methods)
	}
	if !reflect.DeepEqual(result["thread"].(map[string]any)["turns"], history) {
		t.Fatal("read-only fallback lost session contents")
	}
}

func TestThreadRetryRestoresWriteAccessWithoutReadingAgain(t *testing.T) {
	result, err := resumeOrReadThread(context.Background(), "thread-1", func(_ context.Context, method string, _, target any) error {
		if method != "thread/resume" {
			t.Fatalf("successful resume unexpectedly used %s", method)
		}
		*target.(*map[string]any) = map[string]any{"thread": map[string]any{"id": "thread-1"}}
		return nil
	})
	if err != nil || result["readOnly"] != false {
		t.Fatalf("write access was not restored: %#v, %v", result, err)
	}
}

func TestOtherResumeFailuresDoNotBecomeReadOnly(t *testing.T) {
	want := errors.New("invalid thread")
	_, err := resumeOrReadThread(context.Background(), "thread-1", func(_ context.Context, method string, _, _ any) error {
		if method != "thread/resume" {
			t.Fatal("unexpected fallback for unrelated error")
		}
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("original error was hidden: %v", err)
	}
}

func TestReadOnlySessionRejectsWritesAndAllowsPreview(t *testing.T) {
	runtime := imageTestRuntime(t)
	completeTestImage(t, runtime, testImageBytes(t))
	runtime.setThreadReadOnly("thread-1", true)
	for _, action := range []string{"turn.start", "turn.steer", "turn.interrupt", "session.close", "image.begin", "image.chunk", "image.finish"} {
		t.Run(action, func(t *testing.T) {
			raw, _ := json.Marshal(map[string]any{"threadId": "thread-1", "text": "hello", "id": "image-1"})
			if _, err := runtime.execute(context.Background(), commandPayload{Action: action, Data: raw, clientID: "phone-1"}); err == nil {
				t.Fatal("read-only session accepted a write")
			}
		})
	}
	if _, err := imageTestCommand(t, runtime, "phone-1", "image.thumbnail", map[string]any{"threadId": "thread-1", "id": "image-1"}); err != nil {
		t.Fatalf("read-only session could not display attachment: %v", err)
	}
	runtime.setThreadReadOnly("thread-1", false)
	if err := runtime.requireWritableThread("thread-1"); err != nil {
		t.Fatalf("successful retry kept the read-only restriction: %v", err)
	}
}
