package agent

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"codexremote/internal/protocol"
	"codexremote/internal/relay"
	"github.com/gorilla/websocket"
)

func readLinkTestEnvelope(t *testing.T, phone *websocket.Conn, kind string) protocol.Envelope {
	t.Helper()
	_ = phone.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		var envelope protocol.Envelope
		if err := phone.ReadJSON(&envelope); err != nil {
			t.Fatalf("waiting for %s: %v", kind, err)
		}
		// An online-status broadcast can race the initial pairing response.
		if envelope.Kind == kind {
			return envelope
		}
	}
}

func TestSixDigitCodePairsPhoneAndKeepsAuthorization(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := httptest.NewServer(relay.NewHub(log).Handler())
	defer server.Close()
	directory := t.TempDir()
	store := &Store{Dir: directory, ConfigPath: filepath.Join(directory, "config.json")}
	config := &Config{Version: 1, HostID: "host-link-test", RelayURL: "ws" + strings.TrimPrefix(server.URL, "http") + "/ws", Projects: map[string]string{}, Clients: map[string]string{}}
	if err := store.Save(config); err != nil {
		t.Fatal(err)
	}
	_, hostPrivate, _ := ed25519.GenerateKey(nil)
	runtime, err := NewRuntime(store, config, hostPrivate, log)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.prepareLinkPairing(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runtime.connect(ctx) }()
	registrationSigner, _ := protocol.NewSigner(config.HostID, hostPrivate)
	var code string
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		pending := runtime.currentConfig().Pairing
		envelope, _ := registrationSigner.Sign("relay", "pair.register", map[string]any{"token": pending.Code, "expiresAt": pending.ExpiresAt.UnixMilli()})
		raw, _ := json.Marshal(envelope)
		response, err := http.Post(server.URL+"/pairing/register", "application/json", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		var result struct {
			Code string `json:"code"`
		}
		_ = json.NewDecoder(response.Body).Decode(&result)
		response.Body.Close()
		if result.Code != "" {
			code = result.Code
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(code) != 6 {
		t.Fatal("run did not register a six-digit code")
	}
	response, err := http.Post(server.URL+"/pairing/resolve", "application/json", strings.NewReader(`{"code":"`+code+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	var pairing struct {
		HostID    string `json:"hostId"`
		PublicKey string `json:"hostPublicKey"`
		Token     string `json:"code"`
	}
	_ = json.NewDecoder(response.Body).Decode(&pairing)
	response.Body.Close()
	public, err := protocol.ParsePublicKey(pairing.PublicKey)
	if err != nil || !bytes.Equal(public, hostPrivate.Public().(ed25519.PublicKey)) {
		t.Fatal("resolved the wrong host public key")
	}
	_, phonePrivate, _ := ed25519.GenerateKey(nil)
	phoneSigner, _ := protocol.NewSigner("phone-link-test", phonePrivate)
	phone, _, err := websocket.DefaultDialer.Dial(config.RelayURL+"?role=client&host_id="+pairing.HostID+"&client_id=phone-link-test", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer phone.Close()
	hello, _ := phoneSigner.Sign(pairing.HostID, "hello", map[string]string{"publicKey": phoneSigner.PublicKeyBase64()})
	_ = phone.WriteJSON(hello)
	pair, _ := phoneSigner.Sign(pairing.HostID, "pair.request", pairingRequest{Code: pairing.Token, ClientID: "phone-link-test", PublicKey: phoneSigner.PublicKeyBase64()})
	_ = phone.WriteJSON(pair)
	_ = phone.SetReadDeadline(time.Now().Add(3 * time.Second))
	accepted := readLinkTestEnvelope(t, phone, "pair.accepted")
	if accepted.Kind != "pair.accepted" || protocol.Verify(accepted, public) != nil {
		t.Fatalf("phone was not paired: %s", accepted.Kind)
	}
	saved, _ := store.LoadConfig()
	if saved.Pairing != nil || saved.Clients["phone-link-test"] != phoneSigner.PublicKeyBase64() {
		t.Fatal("pairing was not consumed and persisted")
	}
	// Directory commands use the unchanged Relay and the phone's saved key.
	selectedPath := t.TempDir()
	if err := os.Mkdir(filepath.Join(selectedPath, "手机选择的目录"), 0o700); err != nil {
		t.Fatal(err)
	}
	client := &CodexClient{pending: map[string]chan CodexMessage{}}
	client.stdin = &directoryCodexStub{client: client, path: selectedPath}
	runtime.codexMu.Lock()
	runtime.codex = client
	runtime.codexMu.Unlock()
	for index, action := range []string{"directories.list", "thread.start"} {
		requestID := fmt.Sprintf("directory-%d", index)
		command, _ := phoneSigner.Sign(pairing.HostID, "command", map[string]any{
			"requestId": requestID, "action": action, "data": map[string]string{"path": selectedPath},
		})
		if err := phone.WriteJSON(command); err != nil {
			t.Fatal(err)
		}
		response := readLinkTestEnvelope(t, phone, "response")
		if response.Kind != "response" || protocol.Verify(response, public) != nil {
			t.Fatalf("invalid signed directory response: %s", response.Kind)
		}
		var payload struct {
			RequestID string         `json:"requestId"`
			OK        bool           `json:"ok"`
			Data      map[string]any `json:"data"`
			Error     string         `json:"error"`
		}
		if err := protocol.DecodePayload(response, &payload); err != nil || !payload.OK || payload.RequestID != requestID {
			t.Fatalf("directory command failed: %+v, %v", payload, err)
		}
		if action == "directories.list" {
			entries, _ := payload.Data["entries"].([]any)
			if len(entries) != 1 || payload.Data["path"] != selectedPath {
				t.Fatalf("unexpected directory listing: %+v", payload.Data)
			}
		} else {
			thread, _ := payload.Data["thread"].(map[string]any)
			if thread["cwd"] != selectedPath || thread["id"] != "new-thread" {
				t.Fatalf("session did not use the phone-selected folder: %+v", thread)
			}
		}
	}
	// The saved device authenticates again without the one-time pairing code.
	pair, _ = phoneSigner.Sign(pairing.HostID, "pair.request", pairingRequest{ClientID: "phone-link-test", PublicKey: phoneSigner.PublicKeyBase64()})
	_ = phone.WriteJSON(pair)
	accepted = readLinkTestEnvelope(t, phone, "pair.accepted")
	if accepted.Kind != "pair.accepted" {
		t.Fatal("saved authorization was lost")
	}
	response, err = http.Post(server.URL+"/pairing/resolve", "application/json", strings.NewReader(`{"code":"`+code+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusNotFound {
		t.Fatal("consumed code still resolves")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Agent connection did not stop")
	}
}

func TestConnectionCodeCannotAuthorizeAfterExpiryOrConsumption(t *testing.T) {
	for _, mode := range []string{"expired", "consumed", "wrong-token"} {
		t.Run(mode, func(t *testing.T) {
			directory := t.TempDir()
			store := &Store{Dir: directory, ConfigPath: filepath.Join(directory, "config.json")}
			config := &Config{Version: 1, HostID: "host-test", RelayURL: "ws://localhost/ws", Clients: map[string]string{}, Pairing: &Pairing{Code: "token", AutoApprove: true, ExpiresAt: time.Now().Add(time.Minute)}}
			if mode == "expired" {
				config.Pairing.ExpiresAt = time.Now().Add(-time.Second)
			}
			if mode == "consumed" {
				config.Pairing = nil
			}
			_ = store.Save(config)
			_, hostPrivate, _ := ed25519.GenerateKey(nil)
			runtime, _ := NewRuntime(store, config, hostPrivate, slog.New(slog.NewTextHandler(io.Discard, nil)))
			_, phonePrivate, _ := ed25519.GenerateKey(nil)
			signer, _ := protocol.NewSigner("phone-test", phonePrivate)
			token := "token"
			if mode == "wrong-token" {
				token = "another-token"
			}
			envelope, _ := signer.Sign("host-test", "pair.request", pairingRequest{Code: token, ClientID: "phone-test", PublicKey: signer.PublicKeyBase64()})
			runtime.handlePairing(envelope)
			saved, _ := store.LoadConfig()
			if len(saved.Clients) != 0 {
				t.Fatal("invalid code granted authorization")
			}
		})
	}
}
