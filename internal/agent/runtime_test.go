package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
)

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
