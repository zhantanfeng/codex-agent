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

func TestPairingSASIsStable(t *testing.T) {
	first := pairingSAS("host", "phone", "client-key", "code")
	second := pairingSAS("host", "phone", "client-key", "code")
	if first != second || len(first) != 6 {
		t.Fatalf("unexpected SAS %q %q", first, second)
	}
}
