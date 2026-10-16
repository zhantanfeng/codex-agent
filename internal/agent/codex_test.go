package agent

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type failedCodexReader struct{}

func (failedCodexReader) Read([]byte) (int, error) {
	return 0, &os.PathError{Op: "read", Path: "|0", Err: os.ErrClosed}
}

func TestCodexReadDoesNotReportExpectedShutdownAsError(t *testing.T) {
	for _, reason := range []string{"Ctrl+C", "session close"} {
		t.Run(reason, func(t *testing.T) {
			var logs bytes.Buffer
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			client := &CodexClient{log: slog.New(slog.NewTextHandler(&logs, nil)), processCtx: ctx}
			if reason == "Ctrl+C" {
				cancel()
			} else {
				client.closing.Store(true)
			}
			client.read(failedCodexReader{})
			if logs.Len() != 0 {
				t.Fatalf("expected shutdown emitted an error: %s", logs.String())
			}
		})
	}
}

func TestCodexReadStillReportsUnexpectedPipeFailure(t *testing.T) {
	var logs bytes.Buffer
	client := &CodexClient{log: slog.New(slog.NewTextHandler(&logs, nil)), processCtx: context.Background()}
	client.read(failedCodexReader{})
	if !strings.Contains(logs.String(), "app-server output failed") {
		t.Fatal("unexpected pipe failure was hidden")
	}
}

// Opt in to verify a CLI upgrade against its actual stable app-server API.
// This test creates only an ephemeral thread and does not call the model.
func TestInstalledCodexStableAPI(t *testing.T) {
	if os.Getenv("CODEX_REMOTE_CODEX_INTEGRATION") != "1" {
		t.Skip("set CODEX_REMOTE_CODEX_INTEGRATION=1 to verify the installed Codex CLI")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := StartCodex(ctx, "codex", slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	selected := filepath.Join(t.TempDir(), "手机选择目录 with spaces")
	if err := os.Mkdir(selected, 0o700); err != nil {
		t.Fatal(err)
	}
	cwd, err := readableDirectory(selected)
	if err != nil {
		t.Fatal(err)
	}
	var listed map[string]any
	if err := client.Call(ctx, "thread/list", map[string]any{
		"limit": 1, "sortKey": "recency_at", "sortDirection": "desc",
		"sourceKinds": []string{"cli", "vscode", "exec", "appServer", "unknown"},
		"cwd":         []string{cwd},
	}, &listed); err != nil {
		t.Fatal(err)
	}
	if _, ok := listed["data"].([]any); !ok {
		t.Fatal("thread/list did not return a session list")
	}
	params := threadStartParams(cwd)
	params["ephemeral"] = true
	var started map[string]any
	if err := client.Call(ctx, "thread/start", params, &started); err != nil {
		t.Fatal(err)
	}
	thread, _ := started["thread"].(map[string]any)
	if id, _ := thread["id"].(string); id == "" || thread["ephemeral"] != true {
		t.Fatal("thread/start did not return an ephemeral session")
	}
	if actual, _ := thread["cwd"].(string); !sameDirectory(cwd, actual) {
		t.Fatalf("thread/start used working directory %q, want %q", actual, cwd)
	}
	t.Log("Installed Codex CLI initialized, listed sessions, and started an ephemeral session using the stable API.")
}
