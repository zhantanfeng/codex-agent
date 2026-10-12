package agent

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"codexremote/internal/protocol"
	"codexremote/internal/relay"
	"github.com/gorilla/websocket"
)

func testLargeResponseTransfer(t *testing.T, endpoint string, result map[string]any) {
	t.Helper()
	hostID, _ := randomID("host-transfer-test")
	clientID, _ := randomID("phone-transfer-test")
	public, private, _ := protocol.NewKeyPair()
	runtime, err := NewRuntime(nil, &Config{HostID: hostID}, private, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	host, _, err := websocket.DefaultDialer.Dial(endpoint+"?role=agent&host_id="+hostID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	if err := runtime.sendOn(host, "*", "hello", map[string]string{"publicKey": runtime.signer.PublicKeyBase64()}); err != nil {
		t.Fatal(err)
	}
	_, phonePrivate, _ := protocol.NewKeyPair()
	phoneSigner, _ := protocol.NewSigner(clientID, phonePrivate)
	phone, _, err := websocket.DefaultDialer.Dial(endpoint+"?role=client&host_id="+hostID+"&client_id="+clientID, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer phone.Close()
	phone.SetReadLimit(2 << 20)
	hello, _ := phoneSigner.Sign(hostID, "hello", map[string]string{"publicKey": phoneSigner.PublicKeyBase64()})
	if err := phone.WriteJSON(hello); err != nil {
		t.Fatal(err)
	}
	command, _ := phoneSigner.Sign(hostID, "command", map[string]string{"requestId": "resume-test"})
	if err := phone.WriteJSON(command); err != nil {
		t.Fatal(err)
	}
	_ = host.SetReadDeadline(time.Now().Add(10 * time.Second))
	if _, _, err := host.ReadMessage(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	want := responsePayload{RequestID: "resume-test", OK: true, Data: result}
	go func() { done <- runtime.sendOn(host, clientID, "response", want) }()
	var collected bytes.Buffer
	chunks, expectedSize := 0, 0
	guard := protocol.NewReplayGuard(time.Minute)
	_ = phone.SetReadDeadline(time.Now().Add(30 * time.Second))
	for {
		_, raw, err := phone.ReadMessage()
		if err != nil {
			t.Fatal(err)
		}
		if len(raw) >= 2<<20 {
			t.Fatal("chunk exceeded the existing relay frame limit")
		}
		var envelope protocol.Envelope
		_ = json.Unmarshal(raw, &envelope)
		if err := guard.Accept(envelope, public, time.Now()); err != nil {
			t.Fatal(err)
		}
		if envelope.Kind != "transfer.chunk" {
			t.Fatalf("expected chunk, got %s", envelope.Kind)
		}
		var chunk transferChunk
		if err := protocol.DecodePayload(envelope, &chunk); err != nil {
			t.Fatal(err)
		}
		if chunk.Index != chunks || chunk.RequestID != want.RequestID || chunk.Kind != "response" {
			t.Fatalf("invalid chunk metadata: %#v", chunk)
		}
		part, err := base64.RawURLEncoding.DecodeString(chunk.Bytes)
		if err != nil {
			t.Fatal(err)
		}
		collected.Write(part)
		chunks++
		expectedSize = chunk.Size
		if chunks == chunk.Total {
			digest := sha256.Sum256(collected.Bytes())
			if hex.EncodeToString(digest[:]) != chunk.SHA256 {
				t.Fatal("history digest did not survive relay transfer")
			}
			break
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(want)
	if collected.Len() != expectedSize || !bytes.Equal(raw, collected.Bytes()) {
		t.Fatal("reassembled history differs from the original")
	}
	if err := runtime.sendOn(host, clientID, "response", responsePayload{RequestID: "after-transfer", OK: true}); err != nil {
		t.Fatal(err)
	}
	var after protocol.Envelope
	if err := phone.ReadJSON(&after); err != nil || after.Kind != "response" {
		t.Fatalf("connection did not survive history transfer: %v", err)
	}
	t.Logf("Transferred %d bytes in %d signed chunks; connection remained open.", collected.Len(), chunks)
}

func TestLargeHistoryFitsUnchangedRelay(t *testing.T) {
	server := httptest.NewServer(relay.NewHub(slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
	defer server.Close()
	testLargeResponseTransfer(t, "ws"+strings.TrimPrefix(server.URL, "http")+"/ws", map[string]any{"thread": map[string]any{"id": "long-thread", "history": strings.Repeat("会话历史", 350_000)}})
}

func TestLongHistoryThroughExistingPublicRelay(t *testing.T) {
	endpoint := os.Getenv("CODEX_REMOTE_PUBLIC_RELAY")
	if endpoint == "" {
		t.Skip("set CODEX_REMOTE_PUBLIC_RELAY to test the deployed relay")
	}
	testLargeResponseTransfer(t, endpoint, map[string]any{"thread": map[string]any{"id": "long-thread", "history": strings.Repeat("会话历史", 350_000)}})
}
