package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"codexremote/internal/protocol"
	"github.com/gorilla/websocket"
)

type Runtime struct {
	store       *Store
	configMu    sync.RWMutex
	config      *Config
	private     ed25519.PrivateKey
	signer      *protocol.Signer
	guard       *protocol.ReplayGuard
	log         *slog.Logger
	codex       *CodexClient
	connMu      sync.Mutex
	conn        *websocket.Conn
	writeMu     sync.Mutex
	eventMu     sync.Mutex
	events      []eventPayload
	nextEventID atomic.Uint64
	approvalMu  sync.Mutex
	approvals   map[string]CodexMessage
	threadMu    sync.Mutex
	threads     map[string]bool
	activeTurns map[string]string
	queues      map[string][]queuedTurn
}

type queuedTurn struct {
	Text            string
	ClientMessageID string
}

type commandPayload struct {
	RequestID string          `json:"requestId"`
	Action    string          `json:"action"`
	Data      json.RawMessage `json:"data"`
}

type responsePayload struct {
	RequestID string `json:"requestId"`
	OK        bool   `json:"ok"`
	Data      any    `json:"data,omitempty"`
	Error     string `json:"error,omitempty"`
}

type eventPayload struct {
	EventID uint64          `json:"eventId"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type pairingRequest struct {
	Code      string `json:"code"`
	ClientID  string `json:"clientId"`
	PublicKey string `json:"publicKey"`
}

func NewRuntime(store *Store, config *Config, private ed25519.PrivateKey, log *slog.Logger) (*Runtime, error) {
	signer, err := protocol.NewSigner(config.HostID, private)
	if err != nil {
		return nil, err
	}
	runtime := &Runtime{
		store: store, config: config, private: private, signer: signer,
		guard: protocol.NewReplayGuard(5 * time.Minute), log: log,
		approvals: map[string]CodexMessage{}, threads: map[string]bool{},
		activeTurns: map[string]string{}, queues: map[string][]queuedTurn{},
	}
	runtime.nextEventID.Store(uint64(time.Now().UnixMilli()) * 1000)
	return runtime, nil
}

func (r *Runtime) Run(ctx context.Context) error {
	codex, err := StartCodex(ctx, r.currentConfig().CodexCommand, r.log, r.onCodexMessage)
	if err != nil {
		return err
	}
	r.codex = codex
	defer codex.Close()
	backoff := time.Second
	for ctx.Err() == nil {
		if err := r.connect(ctx); err != nil {
			r.log.Warn("relay connection ended", "error", err, "retry", backoff)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff):
		}
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
	return nil
}

func (r *Runtime) connect(ctx context.Context) error {
	config := r.currentConfig()
	endpoint, err := url.Parse(config.RelayURL)
	if err != nil {
		return err
	}
	query := endpoint.Query()
	query.Set("role", "agent")
	query.Set("host_id", config.HostID)
	endpoint.RawQuery = query.Encode()
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, endpoint.String(), nil)
	if err != nil {
		return err
	}
	r.connMu.Lock()
	r.conn = conn
	r.connMu.Unlock()
	defer func() {
		r.connMu.Lock()
		if r.conn == conn {
			r.conn = nil
		}
		r.connMu.Unlock()
		conn.Close()
	}()
	if err := r.sendOn(conn, "*", "hello", map[string]string{"publicKey": r.signer.PublicKeyBase64()}); err != nil {
		return err
	}
	_ = r.broadcast("host.status", map[string]any{"online": true, "hostId": config.HostID})
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		var env protocol.Envelope
		if err := json.Unmarshal(raw, &env); err != nil || env.TargetID != config.HostID {
			continue
		}
		go r.handleEnvelope(env)
	}
}

func (r *Runtime) handleEnvelope(env protocol.Envelope) {
	if env.Kind == "pair.request" {
		r.handlePairing(env)
		return
	}
	latest, err := r.store.LoadConfig()
	if err != nil {
		return
	}
	r.setConfig(latest)
	encoded, ok := latest.Clients[env.SenderID]
	if !ok {
		return
	}
	public, err := protocol.ParsePublicKey(encoded)
	if err != nil || r.guard.Accept(env, public, time.Now()) != nil {
		return
	}
	if env.Kind != "command" {
		return
	}
	var command commandPayload
	if err := protocol.DecodePayload(env, &command); err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	data, err := r.execute(ctx, command)
	response := responsePayload{RequestID: command.RequestID, OK: err == nil, Data: data}
	if err != nil {
		response.Error = err.Error()
	}
	_ = r.send(env.SenderID, "response", response)
}

func (r *Runtime) handlePairing(env protocol.Envelope) {
	var request pairingRequest
	if protocol.DecodePayload(env, &request) != nil || request.ClientID != env.SenderID {
		return
	}
	public, err := protocol.ParsePublicKey(request.PublicKey)
	if err != nil || r.guard.Accept(env, public, time.Now()) != nil {
		return
	}
	latest, err := r.store.LoadConfig()
	if err != nil {
		return
	}
	if known, ok := latest.Clients[request.ClientID]; ok {
		if known == request.PublicKey {
			_ = r.send(request.ClientID, "pair.accepted", map[string]string{"hostId": latest.HostID, "publicKey": r.signer.PublicKeyBase64()})
		}
		return
	}
	if pending, ok := latest.Pending[request.ClientID]; ok && pending.PublicKey == request.PublicKey && time.Now().Before(pending.ExpiresAt) {
		_ = r.send(request.ClientID, "pair.pending", map[string]string{"verificationCode": pending.SAS})
		return
	}
	if latest.Pairing == nil || time.Now().After(latest.Pairing.ExpiresAt) || request.Code != latest.Pairing.Code {
		return
	}
	sas := pairingSAS(r.signer.PublicKeyBase64(), request.ClientID, request.PublicKey, request.Code)
	latest.Pending[request.ClientID] = PendingClient{PublicKey: request.PublicKey, SAS: sas, ExpiresAt: latest.Pairing.ExpiresAt}
	if err := r.store.Save(latest); err != nil {
		return
	}
	r.setConfig(latest)
	_ = r.send(request.ClientID, "pair.pending", map[string]string{"verificationCode": sas})
	r.log.Info("pairing confirmation required", "client", request.ClientID, "verificationCode", sas)
}

func pairingSAS(hostPublic, clientID, clientPublic, code string) string {
	digest := sha256.Sum256([]byte(hostPublic + "|" + clientID + "|" + clientPublic + "|" + code))
	value := (uint32(digest[0])<<16 | uint32(digest[1])<<8 | uint32(digest[2])) % 1_000_000
	return fmt.Sprintf("%06d", value)
}

func (r *Runtime) execute(ctx context.Context, command commandPayload) (any, error) {
	config := r.currentConfig()
	switch command.Action {
	case "projects.list":
		return config.Projects, nil
	case "threads.list":
		paths := make([]string, 0, len(config.Projects))
		for _, path := range config.Projects {
			paths = append(paths, path)
		}
		if len(paths) == 0 {
			return map[string]any{"data": []any{}, "nextCursor": nil, "backwardsCursor": nil}, nil
		}
		var result map[string]any
		err := r.codex.Call(ctx, "thread/list", map[string]any{"limit": 100, "sortKey": "recency_at", "sortDirection": "desc", "sourceKinds": []string{"cli", "vscode", "exec", "appServer", "unknown"}, "cwd": paths}, &result)
		if err == nil {
			r.markThreads(result)
		}
		return result, err
	case "thread.start":
		var data struct {
			ProjectID string `json:"projectId"`
		}
		if err := json.Unmarshal(command.Data, &data); err != nil {
			return nil, err
		}
		path, ok := config.Projects[data.ProjectID]
		if !ok {
			return nil, errors.New("project is not in the Windows allowlist")
		}
		var result map[string]any
		err := r.codex.Call(ctx, "thread/start", map[string]any{"cwd": path, "runtimeWorkspaceRoots": []string{path}}, &result)
		if err == nil {
			r.markThreadResult(result)
		}
		return result, err
	case "thread.resume":
		var data struct {
			ThreadID string `json:"threadId"`
		}
		if err := json.Unmarshal(command.Data, &data); err != nil {
			return nil, err
		}
		if !r.threadAllowed(data.ThreadID) {
			return nil, errors.New("thread is not in an allowed project")
		}
		var result map[string]any
		if err := r.codex.Call(ctx, "thread/resume", map[string]any{"threadId": data.ThreadID, "excludeTurns": false}, &result); err != nil {
			return nil, err
		}
		if !r.resultInAllowlist(result) {
			return nil, errors.New("thread working directory is not in the allowlist")
		}
		r.markThreadResult(result)
		return result, nil
	case "turn.start":
		var data struct {
			ThreadID string `json:"threadId"`
			Text     string `json:"text"`
		}
		if err := json.Unmarshal(command.Data, &data); err != nil {
			return nil, err
		}
		if !r.threadAllowed(data.ThreadID) || strings.TrimSpace(data.Text) == "" {
			return nil, errors.New("thread is not loaded from an allowed project or message is empty")
		}
		clientMessageID := command.RequestID
		r.threadMu.Lock()
		activeTurn := r.activeTurns[data.ThreadID]
		if activeTurn != "" {
			r.queues[data.ThreadID] = append(r.queues[data.ThreadID], queuedTurn{Text: data.Text, ClientMessageID: clientMessageID})
			position := len(r.queues[data.ThreadID])
			r.threadMu.Unlock()
			return map[string]any{"queued": true, "position": position, "activeTurnId": activeTurn}, nil
		}
		r.activeTurns[data.ThreadID] = "starting:" + clientMessageID
		r.threadMu.Unlock()
		var result map[string]any
		err := r.codex.Call(ctx, "turn/start", turnStartParams(data.ThreadID, data.Text, clientMessageID), &result)
		if err != nil {
			r.threadMu.Lock()
			if r.activeTurns[data.ThreadID] == "starting:"+clientMessageID {
				delete(r.activeTurns, data.ThreadID)
			}
			r.threadMu.Unlock()
		}
		return result, err
	case "turn.steer":
		var data struct {
			ThreadID string `json:"threadId"`
			TurnID   string `json:"turnId"`
			Text     string `json:"text"`
		}
		if err := json.Unmarshal(command.Data, &data); err != nil {
			return nil, err
		}
		if !r.threadAllowed(data.ThreadID) {
			return nil, errors.New("thread is not allowed")
		}
		var result map[string]any
		err := r.codex.Call(ctx, "turn/steer", map[string]any{"threadId": data.ThreadID, "expectedTurnId": data.TurnID, "input": []any{map[string]any{"type": "text", "text": data.Text, "text_elements": []any{}}}}, &result)
		return result, err
	case "turn.interrupt":
		var data struct{ ThreadID, TurnID string }
		if err := json.Unmarshal(command.Data, &data); err != nil {
			return nil, err
		}
		if !r.threadAllowed(data.ThreadID) {
			return nil, errors.New("thread is not allowed")
		}
		var result map[string]any
		err := r.codex.Call(ctx, "turn/interrupt", map[string]any{"threadId": data.ThreadID, "turnId": data.TurnID}, &result)
		return result, err
	case "approval.respond":
		var data struct {
			RequestID string `json:"requestId"`
			Decision  string `json:"decision"`
		}
		if err := json.Unmarshal(command.Data, &data); err != nil {
			return nil, err
		}
		return nil, r.respondApproval(data.RequestID, data.Decision)
	case "events.sync":
		var data struct {
			After uint64 `json:"after"`
		}
		_ = json.Unmarshal(command.Data, &data)
		return map[string]any{"events": r.eventsAfter(data.After), "approvals": r.pendingApprovals()}, nil
	default:
		return nil, fmt.Errorf("unsupported action %q", command.Action)
	}
}

func (r *Runtime) onCodexMessage(message CodexMessage) {
	if len(message.ID) > 0 && message.Method != "" {
		id := string(message.ID)
		r.approvalMu.Lock()
		r.approvals[id] = message
		r.approvalMu.Unlock()
		_ = r.broadcast("codex.request", map[string]any{"requestId": id, "method": message.Method, "params": json.RawMessage(message.Params)})
		return
	}
	if message.Method == "" {
		return
	}
	r.trackTurnLifecycle(message)
	event := eventPayload{EventID: r.nextEventID.Add(1), Method: message.Method, Params: message.Params}
	r.eventMu.Lock()
	r.events = append(r.events, event)
	if len(r.events) > 500 {
		r.events = append([]eventPayload(nil), r.events[len(r.events)-500:]...)
	}
	r.eventMu.Unlock()
	_ = r.broadcast("codex.event", event)
}

func (r *Runtime) trackTurnLifecycle(message CodexMessage) {
	if message.Method != "turn/started" && message.Method != "turn/completed" {
		return
	}
	var params struct {
		ThreadID string `json:"threadId"`
		Turn     struct {
			ID string `json:"id"`
		} `json:"turn"`
	}
	if json.Unmarshal(message.Params, &params) != nil || params.ThreadID == "" {
		return
	}
	r.threadMu.Lock()
	if message.Method == "turn/started" {
		r.activeTurns[params.ThreadID] = params.Turn.ID
		r.threadMu.Unlock()
		return
	}
	delete(r.activeTurns, params.ThreadID)
	var next queuedTurn
	if queued := r.queues[params.ThreadID]; len(queued) > 0 {
		next = queued[0]
		r.queues[params.ThreadID] = queued[1:]
	}
	r.threadMu.Unlock()
	if next.Text != "" {
		r.threadMu.Lock()
		r.activeTurns[params.ThreadID] = "starting:" + next.ClientMessageID
		r.threadMu.Unlock()
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			var result map[string]any
			if err := r.codex.Call(ctx, "turn/start", turnStartParams(params.ThreadID, next.Text, next.ClientMessageID), &result); err != nil {
				r.log.Error("start queued turn", "thread", params.ThreadID, "error", err)
				r.threadMu.Lock()
				if r.activeTurns[params.ThreadID] == "starting:"+next.ClientMessageID {
					delete(r.activeTurns, params.ThreadID)
				}
				r.threadMu.Unlock()
			}
		}()
	}
}

func turnStartParams(threadID, text, clientMessageID string) map[string]any {
	return map[string]any{
		"threadId": threadID, "clientUserMessageId": clientMessageID,
		"input": []any{map[string]any{"type": "text", "text": text, "text_elements": []any{}}},
	}
}

func (r *Runtime) respondApproval(id, decision string) error {
	r.approvalMu.Lock()
	request, ok := r.approvals[id]
	if ok {
		delete(r.approvals, id)
	}
	r.approvalMu.Unlock()
	if !ok {
		return errors.New("approval request is no longer pending")
	}
	var result any
	switch request.Method {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		if decision != "accept" && decision != "acceptForSession" && decision != "decline" && decision != "cancel" {
			return errors.New("unsupported approval decision")
		}
		result = map[string]string{"decision": decision}
	case "execCommandApproval", "applyPatchApproval":
		legacy := map[string]string{"accept": "approved", "acceptForSession": "approved_for_session", "decline": "denied", "cancel": "abort"}[decision]
		if legacy == "" {
			return errors.New("unsupported legacy approval decision")
		}
		if legacy == "denied" {
			result = map[string]any{"decision": map[string]any{"denied": map[string]string{"rejection": "Declined from Codex Remote"}}}
		} else {
			result = map[string]string{"decision": legacy}
		}
	default:
		return fmt.Errorf("unsupported app-server request %s", request.Method)
	}
	return r.codex.Respond(request.ID, result)
}

func (r *Runtime) pendingApprovals() []map[string]any {
	r.approvalMu.Lock()
	defer r.approvalMu.Unlock()
	result := make([]map[string]any, 0, len(r.approvals))
	for id, request := range r.approvals {
		result = append(result, map[string]any{
			"requestId": id,
			"method":    request.Method,
			"params":    json.RawMessage(request.Params),
		})
	}
	return result
}

func (r *Runtime) eventsAfter(after uint64) []eventPayload {
	r.eventMu.Lock()
	defer r.eventMu.Unlock()
	result := make([]eventPayload, 0)
	for _, event := range r.events {
		if event.EventID > after {
			result = append(result, event)
		}
	}
	return result
}

func (r *Runtime) send(target, kind string, payload any) error {
	r.connMu.Lock()
	conn := r.conn
	r.connMu.Unlock()
	if conn == nil {
		return errors.New("relay is not connected")
	}
	return r.sendOn(conn, target, kind, payload)
}

func (r *Runtime) broadcast(kind string, payload any) error {
	return r.send("*", kind, payload)
}

func (r *Runtime) sendOn(conn *websocket.Conn, target, kind string, payload any) error {
	env, err := r.signer.Sign(target, kind, payload)
	if err != nil {
		return err
	}
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	return conn.WriteJSON(env)
}

func (r *Runtime) threadAllowed(id string) bool {
	r.threadMu.Lock()
	defer r.threadMu.Unlock()
	return r.threads[id]
}

func (r *Runtime) markThreads(result map[string]any) {
	data, _ := result["data"].([]any)
	r.threadMu.Lock()
	defer r.threadMu.Unlock()
	for _, raw := range data {
		if thread, ok := raw.(map[string]any); ok {
			if id, ok := thread["id"].(string); ok {
				r.threads[id] = true
			}
		}
	}
}

func (r *Runtime) markThreadResult(result map[string]any) {
	thread, _ := result["thread"].(map[string]any)
	id, _ := thread["id"].(string)
	if id != "" {
		r.threadMu.Lock()
		r.threads[id] = true
		r.threadMu.Unlock()
	}
}

func (r *Runtime) resultInAllowlist(result map[string]any) bool {
	thread, _ := result["thread"].(map[string]any)
	cwd, _ := thread["cwd"].(string)
	clean := filepath.Clean(cwd)
	for _, allowed := range r.currentConfig().Projects {
		if strings.EqualFold(clean, filepath.Clean(allowed)) {
			return true
		}
	}
	return false
}

func (r *Runtime) currentConfig() *Config {
	r.configMu.RLock()
	defer r.configMu.RUnlock()
	return r.config
}

func (r *Runtime) setConfig(config *Config) {
	r.configMu.Lock()
	r.config = config
	r.configMu.Unlock()
}
