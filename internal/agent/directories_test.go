package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func directoryRequest(path string, offset, limit int) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{"path": path, "offset": offset, "limit": limit})
	return raw
}

func TestDirectoriesListRootsAndOnlyOneLevelOfFolders(t *testing.T) {
	roots, err := listDirectories(context.Background(), directoryRequest("", 0, 200))
	if err != nil || len(roots.Entries) == 0 || roots.Path != "" {
		t.Fatalf("unexpected roots: %+v, %v", roots, err)
	}
	path := t.TempDir()
	for _, name := range []string{"z-last", "A-first", "中文目录", "A-first/nested"} {
		if err := os.MkdirAll(filepath.Join(path, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(path, "not-a-directory.txt"), []byte("not exposed"), 0o600); err != nil {
		t.Fatal(err)
	}
	page, err := listDirectories(context.Background(), directoryRequest(path, 0, 2))
	if err != nil {
		t.Fatal(err)
	}
	if page.Path != path || page.Parent != filepath.Dir(path) || len(page.Entries) != 2 || page.NextOffset == nil || *page.NextOffset != 2 {
		t.Fatalf("unexpected first page: %+v", page)
	}
	if page.Entries[0].Name != "A-first" || page.Entries[1].Name != "z-last" {
		t.Fatalf("unexpected ordering: %+v", page.Entries)
	}
	last, err := listDirectories(context.Background(), directoryRequest(path, *page.NextOffset, 2))
	if err != nil || len(last.Entries) != 1 || last.Entries[0].Name != "中文目录" || last.NextOffset != nil {
		t.Fatalf("unexpected last page: %+v, %v", last, err)
	}
	empty, err := listDirectories(context.Background(), directoryRequest(filepath.Join(path, "A-first", "nested"), 0, 0))
	if err != nil || len(empty.Entries) != 0 || empty.Entries == nil {
		t.Fatalf("empty folder must return an array: %+v, %v", empty, err)
	}
}

func TestDirectoryValidationErrorsAndCancellation(t *testing.T) {
	path := t.TempDir()
	file := filepath.Join(path, "file.txt")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, request := range []json.RawMessage{
		directoryRequest("relative/path", 0, 0),
		directoryRequest(filepath.Join(path, "missing"), 0, 0),
		directoryRequest(file, 0, 0),
		directoryRequest(path, -1, 0),
		directoryRequest(path, 0, 501),
		json.RawMessage(`{"path":42}`),
	} {
		if _, err := listDirectories(context.Background(), request); err == nil {
			t.Errorf("accepted invalid request: %s", request)
		}
	}
	if message := directoryError(path, os.ErrPermission).Error(); !strings.Contains(message, "没有权限") {
		t.Fatalf("unfriendly permission error: %s", message)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := listDirectories(ctx, directoryRequest(path, 0, 0)); err != context.Canceled {
		t.Fatalf("expected cancellation, got %v", err)
	}
}

type directoryCodexStub struct {
	client *CodexClient
	path   string
	called bool
}

func (stub *directoryCodexStub) Write(raw []byte) (int, error) {
	var message CodexMessage
	if err := json.Unmarshal(raw, &message); err != nil {
		return 0, err
	}
	var params map[string]any
	_ = json.Unmarshal(message.Params, &params)
	if message.Method != "thread/start" || params["cwd"] != stub.path {
		return 0, fmt.Errorf("unexpected start: %s %v", message.Method, params)
	}
	stub.called = true
	result, _ := json.Marshal(map[string]any{"thread": map[string]any{"id": "new-thread", "cwd": stub.path}})
	stub.client.pendingMu.Lock()
	response := stub.client.pending[string(message.ID)]
	stub.client.pendingMu.Unlock()
	response <- CodexMessage{ID: message.ID, Result: result}
	return len(raw), nil
}

func (*directoryCodexStub) Close() error { return nil }

func TestStartInSelectedDirectoryPersistsOnlyThatProject(t *testing.T) {
	storage := t.TempDir()
	store := &Store{Dir: storage, ConfigPath: filepath.Join(storage, "config.json")}
	previous := t.TempDir()
	config := &Config{Version: 1, HostID: "test-host", RelayURL: "ws://localhost/ws", Projects: map[string]string{"existing": previous}, Clients: map[string]string{"phone": "unchanged-key"}}
	if err := store.Save(config); err != nil {
		t.Fatal(err)
	}
	path := t.TempDir()
	client := &CodexClient{pending: map[string]chan CodexMessage{}}
	stub := &directoryCodexStub{client: client, path: path}
	client.stdin = stub
	runtime := &Runtime{store: store, config: config, codex: client, threads: map[string]bool{}}
	raw, _ := json.Marshal(map[string]string{"path": path})
	if _, err := runtime.execute(context.Background(), commandPayload{Action: "thread.start", Data: raw}); err != nil {
		t.Fatal(err)
	}
	if !stub.called || !runtime.threadAllowed("new-thread") {
		t.Fatal("selected directory did not reach thread/start")
	}
	persisted, err := store.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(persisted.Projects) != 2 || persisted.Projects["existing"] != previous || persisted.Clients["phone"] != "unchanged-key" {
		t.Fatalf("existing configuration changed: %+v", persisted)
	}
	// Simulate restarting the Agent. The new session is still in its allowlist.
	restarted := &Runtime{config: persisted}
	if !restarted.resultInAllowlist(map[string]any{"thread": map[string]any{"cwd": path}}) {
		t.Fatal("chosen directory was lost on restart")
	}
	if restarted.resultInAllowlist(map[string]any{"thread": map[string]any{"cwd": filepath.Dir(path)}}) {
		t.Fatal("selecting one folder authorized its parent")
	}
	if err := runtime.rememberDirectory(path + string(os.PathSeparator)); err != nil {
		t.Fatal(err)
	}
	if len(runtime.currentConfig().Projects) != 2 {
		t.Fatal("selecting the same folder added a duplicate project")
	}
	if len(config.Projects) != 1 {
		t.Fatal("mutated a shared configuration snapshot")
	}
}

func TestBrowsingDoesNotAuthorizeDirectories(t *testing.T) {
	config := &Config{Projects: map[string]string{}}
	runtime := &Runtime{config: config}
	if _, err := runtime.execute(context.Background(), commandPayload{Action: "directories.list", Data: directoryRequest(t.TempDir(), 0, 0)}); err != nil {
		t.Fatal(err)
	}
	if len(config.Projects) != 0 {
		t.Fatal("merely browsing a folder changed project authorization")
	}
	for _, raw := range []string{`{"path":"relative"}`, `{"projectId":"unknown"}`} {
		if _, err := runtime.execute(context.Background(), commandPayload{Action: "thread.start", Data: json.RawMessage(raw)}); err == nil {
			t.Fatalf("accepted invalid directory selection: %s", raw)
		}
	}
}
