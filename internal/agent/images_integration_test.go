package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
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

// Opt in because this test calls the installed Codex model using its current login.
func TestImageAttachmentThroughRelayAndCodex(t *testing.T) {
	if os.Getenv("CODEX_REMOTE_IMAGE_INTEGRATION") != "1" {
		t.Skip("set CODEX_REMOTE_IMAGE_INTEGRATION=1 to test actual Codex image input")
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := httptest.NewServer(relay.NewHub(log).Handler())
	defer server.Close()
	endpoint := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	_, hostPrivate, _ := protocol.NewKeyPair()
	_, phonePrivate, _ := protocol.NewKeyPair()
	phoneSigner, _ := protocol.NewSigner("phone-image-test", phonePrivate)
	directory := t.TempDir()
	store := &Store{Dir: directory, ConfigPath: filepath.Join(directory, "config.json")}
	config := &Config{Version: 1, HostID: "host-image-test", RelayURL: endpoint, CodexCommand: "codex", Projects: map[string]string{"test": directory}, Clients: map[string]string{"phone-image-test": phoneSigner.PublicKeyBase64()}}
	if err := store.Save(config); err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(store, config, hostPrivate, log)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- runtime.Run(ctx) }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Error("test Agent did not shut down")
		}
	}()
	deadline := time.Now().Add(20 * time.Second)
	for {
		runtime.connMu.Lock()
		connected := runtime.conn != nil
		runtime.connMu.Unlock()
		if connected {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("test Agent did not connect to relay")
		}
		time.Sleep(10 * time.Millisecond)
	}
	var started map[string]any
	if err := runtime.callCodex(ctx, "thread/start", map[string]any{"cwd": directory, "ephemeral": true, "sandbox": "read-only", "approvalPolicy": "never"}, &started); err != nil {
		t.Fatal(err)
	}
	threadID := started["thread"].(map[string]any)["id"].(string)
	runtime.markThreadResult(started)
	phone, _, err := websocket.DefaultDialer.Dial(endpoint+"?role=client&host_id=host-image-test&client_id=phone-image-test", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer phone.Close()
	hello, _ := phoneSigner.Sign("host-image-test", "hello", map[string]string{"publicKey": phoneSigner.PublicKeyBase64()})
	if err := phone.WriteJSON(hello); err != nil {
		t.Fatal(err)
	}
	sequence := 0
	request := func(action string, data map[string]any) map[string]any {
		t.Helper()
		sequence++
		requestID := fmt.Sprintf("image-request-%d", sequence)
		envelope, _ := phoneSigner.Sign("host-image-test", "command", map[string]any{"requestId": requestID, "action": action, "data": data})
		if err := phone.WriteJSON(envelope); err != nil {
			t.Fatal(err)
		}
		_ = phone.SetReadDeadline(time.Now().Add(70 * time.Second))
		for {
			var response protocol.Envelope
			if err := phone.ReadJSON(&response); err != nil {
				t.Fatal(err)
			}
			if response.Kind != "response" {
				continue
			}
			var payload responsePayload
			if err := protocol.DecodePayload(response, &payload); err != nil {
				t.Fatal(err)
			}
			if payload.RequestID == requestID {
				if !payload.OK {
					t.Fatalf("%s failed: %s", action, payload.Error)
				}
				return payload.Data.(map[string]any)
			}
		}
	}
	// A large, mostly blue PNG exercises chunking while retaining an obvious color.
	picture := image.NewRGBA(image.Rect(0, 0, 900, 900))
	var noise uint32 = 1
	for y := 0; y < 900; y++ {
		for x := 0; x < 900; x++ {
			noise = noise*1664525 + 1013904223
			picture.SetRGBA(x, y, color.RGBA{uint8(noise >> 26), uint8(noise>>18) & 63, 200 + uint8(noise>>10)%56, 255})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, picture); err != nil {
		t.Fatal(err)
	}
	raw := buffer.Bytes()
	digest := sha256.Sum256(raw)
	identity := map[string]any{"id": "image-blue-test", "threadId": threadID}
	request("image.begin", map[string]any{"id": identity["id"], "threadId": threadID, "name": "blue.png", "mimeType": "image/png", "size": len(raw), "sha256": hex.EncodeToString(digest[:])})
	for offset := 0; offset < len(raw); offset += imageChunkBytes {
		end := min(offset+imageChunkBytes, len(raw))
		response := request("image.chunk", map[string]any{"id": identity["id"], "threadId": threadID, "offset": offset, "bytes": base64.RawURLEncoding.EncodeToString(raw[offset:end])})
		if int(response["offset"].(float64)) != end {
			t.Fatal("relay upload offset mismatch")
		}
	}
	request("image.finish", identity)
	request("turn.start", map[string]any{"threadId": threadID, "text": "What is the dominant color in this image? Reply with only the English color name. Do not use tools or edit files.", "imageIds": []string{"image-blue-test"}})
	var answer strings.Builder
	imageObserved := false
	for {
		var envelope protocol.Envelope
		if err := phone.ReadJSON(&envelope); err != nil {
			t.Fatal(err)
		}
		if envelope.Kind != "codex.event" {
			continue
		}
		var event eventPayload
		if err := protocol.DecodePayload(envelope, &event); err != nil {
			t.Fatal(err)
		}
		var params map[string]any
		_ = json.Unmarshal(event.Params, &params)
		if event.Method == "item/agentMessage/delta" {
			answer.WriteString(params["delta"].(string))
		}
		if event.Method == "item/started" || event.Method == "item/completed" {
			if item, ok := params["item"].(map[string]any); ok && item["type"] == "userMessage" {
				for _, raw := range item["content"].([]any) {
					input := raw.(map[string]any)
					if input["attachmentId"] == identity["id"] {
						imageObserved = true
					}
				}
			}
		}
		if event.Method == "turn/completed" {
			turn, _ := params["turn"].(map[string]any)
			if turn["status"] != "completed" {
				t.Fatalf("Codex image turn failed: %#v", turn)
			}
			break
		}
	}
	if !strings.Contains(strings.ToLower(answer.String()), "blue") || !imageObserved {
		t.Fatalf("image input was not visible to Codex/history: answer=%q, imageObserved=%t", answer.String(), imageObserved)
	}
	preview := request("image.thumbnail", identity)
	if preview["bytes"] == "" {
		t.Fatal("history thumbnail was unavailable")
	}
	t.Logf("Uploaded %d bytes through the relay; Codex answered %q and the image preview was retrieved.", len(raw), answer.String())
}

func TestInstalledCodexHistorySize(t *testing.T) {
	threadID := os.Getenv("CODEX_REMOTE_HISTORY_TEST_THREAD")
	if threadID == "" {
		t.Skip("set CODEX_REMOTE_HISTORY_TEST_THREAD for a read-only history diagnostic")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	client, err := StartCodex(ctx, "codex", slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var result map[string]any
	if err := client.Call(ctx, "thread/read", map[string]any{"threadId": threadID, "includeTurns": true}, &result); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(result)
	_, private, _ := protocol.NewKeyPair()
	signer, _ := protocol.NewSigner("host-diagnostic", private)
	envelope, _ := signer.Sign("phone-diagnostic", "response", responsePayload{RequestID: "diagnostic", OK: true, Data: result})
	frame, _ := json.Marshal(envelope)
	thread, _ := result["thread"].(map[string]any)
	turns, _ := thread["turns"].([]any)
	items := 0
	for _, raw := range turns {
		turn, _ := raw.(map[string]any)
		rows, _ := turn["items"].([]any)
		items += len(rows)
	}
	t.Logf("History: result=%d bytes, relayFrame=%d bytes, relayLimit=%d bytes, turns=%d, items=%d", len(raw), len(frame), 2<<20, len(turns), items)
	if endpoint := os.Getenv("CODEX_REMOTE_PUBLIC_RELAY"); endpoint != "" {
		testLargeResponseTransfer(t, endpoint, result)
	}
}
