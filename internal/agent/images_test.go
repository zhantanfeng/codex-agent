package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"codexremote/internal/protocol"
)

func imageTestRuntime(t *testing.T) *Runtime {
	t.Helper()
	return &Runtime{
		store:       &Store{Dir: t.TempDir()},
		config:      &Config{Projects: map[string]string{"test": t.TempDir()}},
		threads:     map[string]bool{"thread-1": true, "thread-2": true},
		activeTurns: map[string]string{}, queues: map[string][]queuedTurn{},
	}
}

func testImageBytes(t *testing.T) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, 600, 600))
	var noise uint32 = 1
	for y := 0; y < 600; y++ {
		for x := 0; x < 600; x++ {
			noise = noise*1664525 + 1013904223
			picture.SetRGBA(x, y, color.RGBA{uint8(noise >> 24), uint8(noise >> 16), uint8(noise >> 8), 255})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, picture); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func imageTestCommand(t *testing.T, runtime *Runtime, clientID, action string, data map[string]any) (map[string]any, error) {
	t.Helper()
	raw, _ := json.Marshal(data)
	result, err := runtime.execute(context.Background(), commandPayload{clientID: clientID, Action: action, Data: raw})
	if err != nil {
		return nil, err
	}
	return result.(map[string]any), nil
}

func beginTestImage(t *testing.T, runtime *Runtime, raw []byte) map[string]any {
	t.Helper()
	digest := sha256.Sum256(raw)
	data := map[string]any{"id": "image-1", "threadId": "thread-1", "name": "phone.png", "mimeType": "image/png", "size": len(raw), "sha256": hex.EncodeToString(digest[:])}
	if _, err := imageTestCommand(t, runtime, "phone-1", "image.begin", data); err != nil {
		t.Fatal(err)
	}
	return data
}

func completeTestImage(t *testing.T, runtime *Runtime, raw []byte) {
	t.Helper()
	beginTestImage(t, runtime, raw)
	for offset := 0; offset < len(raw); offset += imageChunkBytes {
		end := min(offset+imageChunkBytes, len(raw))
		data := map[string]any{"id": "image-1", "threadId": "thread-1", "offset": offset, "bytes": base64.RawURLEncoding.EncodeToString(raw[offset:end])}
		if _, err := imageTestCommand(t, runtime, "phone-1", "image.chunk", data); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := imageTestCommand(t, runtime, "phone-1", "image.finish", map[string]any{"id": "image-1", "threadId": "thread-1"}); err != nil {
		t.Fatal(err)
	}
}

func TestImageUploadResumesAndDoesNotDuplicateRepeatedChunks(t *testing.T) {
	runtime := imageTestRuntime(t)
	raw := testImageBytes(t)
	metadata := beginTestImage(t, runtime, raw)
	first := map[string]any{"id": "image-1", "threadId": "thread-1", "offset": 0, "bytes": base64.RawURLEncoding.EncodeToString(raw[:imageChunkBytes])}
	for range 2 {
		result, err := imageTestCommand(t, runtime, "phone-1", "image.chunk", first)
		if err != nil || result["offset"] != int64(imageChunkBytes) {
			t.Fatalf("unexpected repeated chunk response: %#v, %v", result, err)
		}
	}
	// A new Agent instance resumes the on-disk upload after a restart.
	restarted := &Runtime{store: runtime.store, config: runtime.config, threads: runtime.threads}
	result, err := imageTestCommand(t, restarted, "phone-1", "image.begin", metadata)
	if err != nil || result["offset"] != int64(imageChunkBytes) || result["complete"] != false {
		t.Fatalf("upload did not resume: %#v, %v", result, err)
	}
	completeTestImage(t, restarted, raw)
	paths, err := restarted.imagePaths("thread-1", "phone-1", []string{"image-1"})
	if err != nil || len(paths) != 1 || !filepath.IsAbs(paths[0]) {
		t.Fatalf("invalid image input paths: %#v, %v", paths, err)
	}
	stored, _ := os.ReadFile(paths[0])
	if !bytes.Equal(raw, stored) {
		t.Fatal("completed image changed during transfer")
	}
	result, err = imageTestCommand(t, restarted, "phone-1", "image.begin", metadata)
	if err != nil || result["complete"] != true {
		t.Fatal("completed upload was not reusable")
	}
}

func TestImageUploadRejectsForeignThreadsAndInvalidChunks(t *testing.T) {
	runtime := imageTestRuntime(t)
	raw := testImageBytes(t)
	beginTestImage(t, runtime, raw)
	for _, test := range []struct {
		name, client, id, thread string
		offset                   int
		chunk                    []byte
	}{
		{"path traversal", "phone-1", "../image-1", "thread-1", 0, raw[:10]},
		{"unknown thread", "phone-1", "image-1", "unallowed", 0, raw[:10]},
		{"foreign phone", "phone-2", "image-1", "thread-1", 0, raw[:10]},
		{"out of order", "phone-1", "image-1", "thread-1", 1, raw[:10]},
		{"oversized chunk", "phone-1", "image-1", "thread-1", 0, raw[:imageChunkBytes+1]},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := imageTestCommand(t, runtime, test.client, "image.chunk", map[string]any{"id": test.id, "threadId": test.thread, "offset": test.offset, "bytes": base64.RawURLEncoding.EncodeToString(test.chunk)})
			if err == nil {
				t.Fatal("invalid chunk was accepted")
			}
		})
	}
	if _, err := imageTestCommand(t, runtime, "phone-1", "image.finish", map[string]any{"id": "image-1", "threadId": "thread-1"}); err == nil {
		t.Fatal("incomplete image was accepted")
	}
}

func TestImageHistoryAndQueuePreserveAttachments(t *testing.T) {
	runtime := imageTestRuntime(t)
	completeTestImage(t, runtime, testImageBytes(t))
	paths, _ := runtime.imagePaths("thread-1", "phone-1", []string{"image-1"})
	if _, err := runtime.imagePaths("thread-1", "phone-2", []string{"image-1"}); err == nil {
		t.Fatal("foreign phone was allowed to reuse the image")
	}
	if _, err := runtime.imagePaths("thread-2", "phone-1", []string{"image-1"}); err == nil {
		t.Fatal("another thread was allowed to reuse the image")
	}
	input := turnStartParams("thread-1", "explain", "request-1", paths...)["input"].([]any)
	if len(input) != 2 || input[1].(map[string]any)["type"] != "localImage" {
		t.Fatal("image was not included in the Codex request")
	}
	item := map[string]any{"type": "userMessage", "content": input}
	runtime.annotateImages("thread-1", []any{item})
	if input[1].(map[string]any)["attachmentId"] != "image-1" {
		t.Fatal("history did not identify the uploaded image")
	}
	preview, err := imageTestCommand(t, runtime, "phone-1", "image.thumbnail", map[string]any{"id": "image-1", "threadId": "thread-1"})
	if err != nil {
		t.Fatal(err)
	}
	thumbnail, _ := base64.RawURLEncoding.DecodeString(preview["bytes"].(string))
	config, format, err := image.DecodeConfig(bytes.NewReader(thumbnail))
	if err != nil || format != "jpeg" || config.Width > 512 || config.Height > 512 {
		t.Fatal("invalid thumbnail")
	}
	runtime.activeTurns["thread-1"] = "turn-active"
	result, err := imageTestCommand(t, runtime, "phone-1", "turn.start", map[string]any{"threadId": "thread-1", "text": "", "imageIds": []string{"image-1"}})
	if err != nil || result["queued"] != true || len(runtime.queues["thread-1"][0].ImagePaths) != 1 {
		t.Fatalf("image-only queued message lost its attachment: %#v, %v", result, err)
	}
}

func TestImageChunksFitExistingRelayMessageLimit(t *testing.T) {
	_, private, _ := protocol.NewKeyPair()
	signer, _ := protocol.NewSigner("phone-test", private)
	chunk := base64.RawURLEncoding.EncodeToString(make([]byte, imageChunkBytes))
	envelope, err := signer.Sign("host-test", "command", map[string]any{"action": "image.chunk", "data": map[string]any{"id": "image-1", "threadId": "thread-1", "offset": 0, "bytes": chunk}})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(envelope)
	if len(raw) >= 2<<20 {
		t.Fatalf("image chunk exceeds the relay's 2 MiB frame limit: %d", len(raw))
	}
}
