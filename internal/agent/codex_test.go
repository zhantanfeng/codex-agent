package agent

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
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
