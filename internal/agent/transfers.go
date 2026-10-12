package agent

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/gorilla/websocket"
)

const transferChunkBytes = 192 << 10
const maxTransferBytes = 32 << 20

type transferChunk struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	RequestID string `json:"requestId,omitempty"`
	Index     int    `json:"index"`
	Total     int    `json:"total"`
	Size      int    `json:"size"`
	SHA256    string `json:"sha256"`
	Bytes     string `json:"bytes"`
}

func (r *Runtime) sendPayload(conn *websocket.Conn, target, kind string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if len(raw) > maxTransferBytes {
		return errors.New("会话数据过大，请打开较短的会话")
	}
	if len(raw) <= 512<<10 {
		return r.writeEnvelope(conn, target, kind, payload)
	}
	id, err := randomID("transfer")
	if err != nil {
		return err
	}
	digest := sha256.Sum256(raw)
	requestID := ""
	if response, ok := payload.(responsePayload); ok {
		requestID = response.RequestID
	}
	total := (len(raw) + transferChunkBytes - 1) / transferChunkBytes
	for index := 0; index < total; index++ {
		start, end := index*transferChunkBytes, min((index+1)*transferChunkBytes, len(raw))
		chunk := transferChunk{ID: id, Kind: kind, RequestID: requestID, Index: index, Total: total, Size: len(raw), SHA256: hex.EncodeToString(digest[:]), Bytes: base64.RawURLEncoding.EncodeToString(raw[start:end])}
		if err := r.writeEnvelope(conn, target, "transfer.chunk", chunk); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runtime) writeEnvelope(conn *websocket.Conn, target, kind string, payload any) error {
	env, err := r.signer.Sign(target, kind, payload)
	if err != nil {
		return err
	}
	_ = conn.SetWriteDeadline(time.Now().Add(15 * time.Second))
	return conn.WriteJSON(env)
}
