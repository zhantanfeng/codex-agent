package agent

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestRelayReadUnblocksWhenAgentIsCanceled(t *testing.T) {
	ready := make(chan struct{})
	disconnected := make(chan struct{})
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		conn, err := upgrader.Upgrade(w, request, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		if _, _, err := conn.ReadMessage(); err != nil { // Agent hello.
			return
		}
		if _, _, err := conn.ReadMessage(); err != nil { // Online status.
			return
		}
		close(ready)
		_, _, _ = conn.ReadMessage() // Relay sends no messages while idle.
		close(disconnected)
	}))
	defer server.Close()
	_, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(nil, &Config{HostID: "host-test", RelayURL: "ws" + strings.TrimPrefix(server.URL, "http")}, private, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runtime.connect(ctx) }()
	defer func() {
		runtime.connMu.Lock()
		if runtime.conn != nil {
			_ = runtime.conn.Close()
		}
		runtime.connMu.Unlock()
	}()
	select {
	case <-ready:
	case <-time.After(3 * time.Second):
		t.Fatal("Agent did not connect to the test relay")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Agent is still blocked in ReadMessage after cancellation")
	}
	select {
	case <-disconnected:
	case <-time.After(3 * time.Second):
		t.Fatal("relay connection stayed open after cancellation")
	}
	runtime.connMu.Lock()
	defer runtime.connMu.Unlock()
	if runtime.conn != nil {
		t.Fatal("Agent retained the closed relay connection")
	}
}

func TestCommandCancellationFollowsAgentShutdown(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runtime := &Runtime{runCtx: ctx}
	commandCtx, commandCancel := context.WithTimeout(runtime.commandContext(), time.Minute)
	defer commandCancel()
	cancel()
	select {
	case <-commandCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("in-flight command did not stop when the Agent was canceled")
	}
}

func TestTurnIsQueuedWhenThreadIsActive(t *testing.T) {
	runtime := &Runtime{
		config:      &Config{Projects: map[string]string{"demo": `D:\demo`}},
		log:         slog.New(slog.NewTextHandler(io.Discard, nil)),
		threads:     map[string]bool{"thread-1": true},
		activeTurns: map[string]string{"thread-1": "turn-1"},
		queues:      map[string][]queuedTurn{},
	}
	raw, _ := json.Marshal(map[string]string{"threadId": "thread-1", "text": "next"})
	result, err := runtime.execute(context.Background(), commandPayload{RequestID: "request-1", Action: "turn.start", Data: raw})
	if err != nil {
		t.Fatal(err)
	}
	value := result.(map[string]any)
	if value["queued"] != true || len(runtime.queues["thread-1"]) != 1 {
		t.Fatalf("turn was not queued: %#v", value)
	}
}

func TestThreadListIsEmptyWithoutProjects(t *testing.T) {
	runtime := &Runtime{config: &Config{Projects: map[string]string{}}, threads: map[string]bool{}, activeTurns: map[string]string{}, queues: map[string][]queuedTurn{}}
	result, err := runtime.execute(context.Background(), commandPayload{Action: "threads.list"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.(map[string]any)["data"].([]any)) != 0 {
		t.Fatal("empty allowlist exposed threads")
	}
}

func TestThreadStartParamsUseStableAPI(t *testing.T) {
	params := threadStartParams(`D:\demo`)
	if params["cwd"] != `D:\demo` {
		t.Fatalf("unexpected cwd: %#v", params["cwd"])
	}
	if _, ok := params["runtimeWorkspaceRoots"]; ok {
		t.Fatal("thread/start must not send experimental runtimeWorkspaceRoots")
	}
}

func TestPhoneSessionCannotCloseDuringActiveTurn(t *testing.T) {
	runtime := &Runtime{
		config:      &Config{Projects: map[string]string{"demo": `D:\demo`}},
		threads:     map[string]bool{"thread-1": true},
		activeTurns: map[string]string{"thread-1": "turn-1"},
		queues:      map[string][]queuedTurn{},
	}
	raw, _ := json.Marshal(map[string]string{"threadId": "thread-1"})
	_, err := runtime.execute(context.Background(), commandPayload{Action: "session.close", Data: raw})
	if err == nil || err.Error() != "stop the active turn before closing the phone session" {
		t.Fatalf("unexpected close result: %v", err)
	}
}

func TestPairingSASIsStable(t *testing.T) {
	first := pairingSAS("host", "phone", "client-key", "code")
	second := pairingSAS("host", "phone", "client-key", "code")
	if first != second || len(first) != 6 {
		t.Fatalf("unexpected SAS %q %q", first, second)
	}
}

func TestCodexVersionCompatibility(t *testing.T) {
	tests := []struct {
		version string
		want    bool
	}{
		{version: "codex-cli 0.156.1", want: true},
		{version: "codex-cli 0.161.0", want: true},
		{version: "codex-cli 0.161.9", want: true},
		{version: "codex-cli 0.157.0", want: false},
		{version: "codex-cli 0.160.9", want: false},
		{version: "codex-cli 0.162.0", want: false},
	}
	for _, test := range tests {
		t.Run(test.version, func(t *testing.T) {
			if got := isSupportedCodexVersion(test.version); got != test.want {
				t.Fatalf("isSupportedCodexVersion(%q) = %t, want %t", test.version, got, test.want)
			}
		})
	}
}
