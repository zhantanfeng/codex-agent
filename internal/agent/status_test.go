package agent

import (
	"crypto/ed25519"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"codexremote/internal/protocol"
	"github.com/gorilla/websocket"
)

func TestOnlinePhonesCountHeartbeatsInsteadOfHistoricalAuthorizations(t *testing.T) {
	now := time.Now()
	r := &Runtime{config: &Config{Clients: map[string]string{
		"phone-current": "key-1", "old-install-1": "key-2", "old-install-2": "key-3", "old-install-3": "key-4",
	}}}
	r.notePhone("phone-current", now)
	r.notePhone("phone-current", now.Add(time.Second)) // Same phone reconnects.
	if got := r.onlinePhones(now.Add(2 * time.Second)); !reflect.DeepEqual(got, []string{"phone-current"}) {
		t.Fatalf("one phone with four saved authorizations should count once: %v", got)
	}
	if got := r.onlinePhones(now.Add(21 * time.Second)); len(got) != 0 {
		t.Fatalf("disconnected phone did not expire: %v", got)
	}
	if len(r.currentConfig().Clients) != 4 {
		t.Fatal("online count must not remove historical authorizations")
	}
}

func TestOnlinePhonesIncludeSeparateDevicesAndExcludeRevokedOnes(t *testing.T) {
	now := time.Now()
	r := &Runtime{config: &Config{Clients: map[string]string{"phone-a": "a", "phone-b": "b"}}}
	r.notePhone("phone-b", now)
	r.notePhone("phone-a", now)
	r.notePhone("unpaired", now)
	if got := r.onlinePhones(now); !reflect.DeepEqual(got, []string{"phone-a", "phone-b"}) {
		t.Fatalf("incorrect online phones: %v", got)
	}
	r.setConfig(&Config{Clients: map[string]string{"phone-b": "b"}})
	if got := r.onlinePhones(now); !reflect.DeepEqual(got, []string{"phone-b"}) {
		t.Fatalf("revoked phone remained online: %v", got)
	}
	r.conn = &websocket.Conn{}
	if len(r.Status().OnlineDevices) != 1 {
		t.Fatal("online phone missing while relay is connected")
	}
	r.conn = nil
	if len(r.Status().OnlineDevices) != 0 {
		t.Fatal("phones shown online while relay is disconnected")
	}
}

func TestOnlyAuthorizedSignedCommandsRefreshPhonePresence(t *testing.T) {
	_, hostPrivate, _ := ed25519.GenerateKey(nil)
	_, phonePrivate, _ := ed25519.GenerateKey(nil)
	phone, _ := protocol.NewSigner("phone", phonePrivate)
	dir := t.TempDir()
	store := &Store{Dir: dir, ConfigPath: filepath.Join(dir, "config.json")}
	config := &Config{Version: 1, HostID: "host", RelayURL: "wss://relay.example/ws", Clients: map[string]string{"phone": phone.PublicKeyBase64()}}
	if err := store.Save(config); err != nil {
		t.Fatal(err)
	}
	r, err := NewRuntime(store, config, hostPrivate, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	env, _ := phone.Sign("host", "command", commandPayload{Action: "projects.list", Data: json.RawMessage(`{}`)})
	invalid := env
	invalid.Signature = "invalid"
	r.handleEnvelope(invalid)
	if len(r.onlinePhones(time.Now())) != 0 {
		t.Fatal("invalid signature counted as online")
	}
	r.handleEnvelope(env)
	if got := r.onlinePhones(time.Now()); !reflect.DeepEqual(got, []string{"phone"}) {
		t.Fatalf("verified heartbeat was not counted: %v", got)
	}
	r.statusMu.Lock()
	old := time.Now().Add(-phonePresenceWindow)
	r.phoneSeen["phone"] = old
	r.statusMu.Unlock()
	r.handleEnvelope(env) // Replay must not make an old phone appear online.
	if len(r.onlinePhones(time.Now())) != 0 {
		t.Fatal("replayed command refreshed presence")
	}
}
