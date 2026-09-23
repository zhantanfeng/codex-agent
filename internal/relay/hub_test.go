package relay

import (
	"crypto/ed25519"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codexremote/internal/protocol"
	"github.com/gorilla/websocket"
)

func TestHubRoutesSignedMessages(t *testing.T) {
	server := httptest.NewServer(NewHub(slog.New(slog.NewTextHandler(io.Discard, nil))).Handler())
	defer server.Close()
	endpoint := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"

	_, hostPrivate, _ := ed25519.GenerateKey(nil)
	hostSigner, _ := protocol.NewSigner("host-test", hostPrivate)
	host := dialAndHello(t, endpoint+"?role=agent&host_id=host-test", hostSigner, "*")
	defer host.Close()

	_, clientPrivate, _ := ed25519.GenerateKey(nil)
	clientSigner, _ := protocol.NewSigner("phone-test", clientPrivate)
	client := dialAndHello(t, endpoint+"?role=client&host_id=host-test&client_id=phone-test", clientSigner, "host-test")
	defer client.Close()

	want, _ := clientSigner.Sign("host-test", "command", map[string]string{"action": "projects.list"})
	if err := client.WriteJSON(want); err != nil {
		t.Fatal(err)
	}
	_ = host.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, raw, err := host.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var got protocol.Envelope
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got.Signature != want.Signature || got.Kind != "command" {
		t.Fatalf("message changed in transit: %#v", got)
	}
}

func dialAndHello(t *testing.T, endpoint string, signer *protocol.Signer, target string) *websocket.Conn {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	hello, _ := signer.Sign(target, "hello", map[string]string{"publicKey": signer.PublicKeyBase64()})
	if err := conn.WriteJSON(hello); err != nil {
		t.Fatal(err)
	}
	return conn
}
