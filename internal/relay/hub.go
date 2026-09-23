package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"codexremote/internal/protocol"
	"github.com/gorilla/websocket"
)

const maxMessageSize = 2 << 20

type Hub struct {
	log      *slog.Logger
	upgrader websocket.Upgrader
	guard    *protocol.ReplayGuard
	mu       sync.RWMutex
	hosts    map[string]*peer
	clients  map[string]map[string]*peer
}

type peer struct {
	id       string
	hostID   string
	role     string
	conn     *websocket.Conn
	public   []byte
	writeMu  sync.Mutex
	lastSeen time.Time
}

type helloPayload struct {
	PublicKey string `json:"publicKey"`
}

func NewHub(log *slog.Logger) *Hub {
	return &Hub{
		log: log,
		upgrader: websocket.Upgrader{
			HandshakeTimeout: 10 * time.Second,
			CheckOrigin:      func(*http.Request) bool { return true },
		},
		guard: protocol.NewReplayGuard(5 * time.Minute),
		hosts: make(map[string]*peer), clients: make(map[string]map[string]*peer),
	}
}

func (h *Hub) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/ws", h.serveWebSocket)
	return mux
}

func (h *Hub) serveWebSocket(w http.ResponseWriter, r *http.Request) {
	role := r.URL.Query().Get("role")
	hostID := r.URL.Query().Get("host_id")
	peerID := r.URL.Query().Get("client_id")
	if role == "agent" {
		peerID = hostID
	}
	if (role != "agent" && role != "client") || !validID(hostID) || !validID(peerID) {
		http.Error(w, "invalid role or peer identifier", http.StatusBadRequest)
		return
	}
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	conn.SetReadLimit(maxMessageSize)
	_ = conn.SetReadDeadline(time.Now().Add(15 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		return
	}
	var hello protocol.Envelope
	if err := json.Unmarshal(raw, &hello); err != nil || hello.Kind != "hello" || hello.SenderID != peerID {
		h.closeWith(conn, websocket.ClosePolicyViolation, "invalid hello")
		return
	}
	var payload helloPayload
	if err := protocol.DecodePayload(hello, &payload); err != nil {
		h.closeWith(conn, websocket.ClosePolicyViolation, "invalid hello payload")
		return
	}
	public, err := protocol.ParsePublicKey(payload.PublicKey)
	if err != nil || h.guard.Accept(hello, public, time.Now()) != nil {
		h.closeWith(conn, websocket.ClosePolicyViolation, "hello signature rejected")
		return
	}
	p := &peer{id: peerID, hostID: hostID, role: role, conn: conn, public: public, lastSeen: time.Now()}
	h.add(p)
	defer h.remove(p)
	_ = conn.SetReadDeadline(time.Time{})
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(75 * time.Second))
		return nil
	})
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go h.ping(ctx, p)
	h.log.Info("peer connected", "role", role, "host", hostID, "peer", peerID)
	for {
		_, raw, err = conn.ReadMessage()
		if err != nil {
			break
		}
		var env protocol.Envelope
		if err := json.Unmarshal(raw, &env); err != nil {
			h.closeWith(conn, websocket.CloseUnsupportedData, "invalid JSON")
			break
		}
		if env.SenderID != p.id || h.guard.Accept(env, public, time.Now()) != nil {
			h.closeWith(conn, websocket.ClosePolicyViolation, "signature or replay check failed")
			break
		}
		if err := h.route(p, raw, env); err != nil {
			h.log.Debug("route failed", "error", err, "from", p.id, "target", env.TargetID)
		}
	}
}

func (h *Hub) route(from *peer, raw []byte, env protocol.Envelope) error {
	if from.role == "client" {
		if env.TargetID != from.hostID {
			return errors.New("client target does not match its host")
		}
		h.mu.RLock()
		target := h.hosts[from.hostID]
		h.mu.RUnlock()
		if target == nil {
			return errors.New("host is offline")
		}
		return target.write(raw)
	}
	if env.TargetID == "*" {
		h.mu.RLock()
		peers := make([]*peer, 0, len(h.clients[from.hostID]))
		for _, candidate := range h.clients[from.hostID] {
			peers = append(peers, candidate)
		}
		h.mu.RUnlock()
		for _, candidate := range peers {
			_ = candidate.write(raw)
		}
		return nil
	}
	h.mu.RLock()
	target := h.clients[from.hostID][env.TargetID]
	h.mu.RUnlock()
	if target == nil {
		return errors.New("client is offline")
	}
	return target.write(raw)
}

func (h *Hub) add(p *peer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if p.role == "agent" {
		if existing := h.hosts[p.hostID]; existing != nil {
			h.closeWith(existing.conn, websocket.CloseNormalClosure, "replaced by a newer connection")
		}
		h.hosts[p.hostID] = p
		return
	}
	if h.clients[p.hostID] == nil {
		h.clients[p.hostID] = make(map[string]*peer)
	}
	if existing := h.clients[p.hostID][p.id]; existing != nil {
		h.closeWith(existing.conn, websocket.CloseNormalClosure, "replaced by a newer connection")
	}
	h.clients[p.hostID][p.id] = p
}

func (h *Hub) remove(p *peer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if p.role == "agent" {
		if h.hosts[p.hostID] == p {
			delete(h.hosts, p.hostID)
		}
	} else if h.clients[p.hostID][p.id] == p {
		delete(h.clients[p.hostID], p.id)
	}
	h.log.Info("peer disconnected", "role", p.role, "host", p.hostID, "peer", p.id)
}

func (h *Hub) ping(ctx context.Context, p *peer) {
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.writeMu.Lock()
			err := p.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second))
			p.writeMu.Unlock()
			if err != nil {
				return
			}
		}
	}
}

func (p *peer) write(raw []byte) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	_ = p.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return p.conn.WriteMessage(websocket.TextMessage, raw)
}

func (h *Hub) closeWith(conn *websocket.Conn, code int, reason string) {
	_ = conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, reason), time.Now().Add(time.Second))
}

func validID(value string) bool {
	return value != "" && len(value) <= 128 && !strings.ContainsAny(value, "\r\n/?&#")
}

func ListenAndServe(address string, log *slog.Logger) error {
	server := &http.Server{Addr: address, Handler: NewHub(log).Handler(), ReadHeaderTimeout: 10 * time.Second}
	log.Info("relay listening", "address", address)
	return fmt.Errorf("relay stopped: %w", server.ListenAndServe())
}
