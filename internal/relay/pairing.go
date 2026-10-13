package relay

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"time"

	"codexremote/internal/protocol"
)

type linkOffer struct {
	Token     string `json:"token"`
	ExpiresAt int64  `json:"expiresAt"`
}

type linkRegistration struct {
	host      *peer
	token     string
	expiresAt time.Time
}

type lookupWindow struct {
	started time.Time
	count   int
}

func pairingError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func (h *Hub) registerPairing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		pairingError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	var envelope protocol.Envelope
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&envelope); err != nil || envelope.Kind != "pair.register" || envelope.TargetID != "relay" {
		pairingError(w, http.StatusBadRequest, "invalid pairing registration")
		return
	}
	h.mu.RLock()
	host := h.hosts[envelope.SenderID]
	h.mu.RUnlock()
	if host == nil {
		pairingError(w, http.StatusNotFound, "host_offline")
		return
	}
	now := time.Now()
	if err := h.pairingGuard.Accept(envelope, host.public, now); err != nil {
		pairingError(w, http.StatusForbidden, "pairing signature rejected")
		return
	}
	var offer linkOffer
	if protocol.DecodePayload(envelope, &offer) != nil {
		pairingError(w, http.StatusBadRequest, "invalid pairing offer")
		return
	}
	token, err := base64.RawURLEncoding.DecodeString(offer.Token)
	expiresAt := time.UnixMilli(offer.ExpiresAt)
	if err != nil || len(token) != 32 || !expiresAt.After(now) || expiresAt.After(now.Add(10*time.Minute+30*time.Second)) {
		pairingError(w, http.StatusBadRequest, "invalid pairing token or expiry")
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.hosts[host.id] != host {
		pairingError(w, http.StatusNotFound, "host_offline")
		return
	}
	for code, registration := range h.pairings {
		if !registration.expiresAt.After(now) {
			delete(h.pairings, code)
		} else if registration.host == host && registration.token == offer.Token {
			h.writePairingCode(w, code, registration.expiresAt)
			return
		}
	}
	for attempts := 0; attempts < 100; attempts++ {
		value, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
		if err != nil {
			break
		}
		code := fmt.Sprintf("%06d", value.Int64())
		if _, exists := h.pairings[code]; exists {
			continue
		}
		h.removeHostPairingLocked(host)
		h.pairings[code] = linkRegistration{host: host, token: offer.Token, expiresAt: expiresAt}
		h.writePairingCode(w, code, expiresAt)
		return
	}
	pairingError(w, http.StatusServiceUnavailable, "cannot allocate a connection code")
}

func (h *Hub) writePairingCode(w http.ResponseWriter, code string, expiresAt time.Time) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "expiresAt": expiresAt.UnixMilli()})
}

func (h *Hub) resolvePairing(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		pairingError(w, http.StatusMethodNotAllowed, "POST required")
		return
	}
	address, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		address = r.RemoteAddr
	}
	now := time.Now()
	h.mu.Lock()
	allowed := h.allowLookupLocked(address, now)
	h.mu.Unlock()
	if !allowed {
		w.Header().Set("Retry-After", "60")
		pairingError(w, http.StatusTooManyRequests, "尝试次数过多，请一分钟后再试")
		return
	}
	var request struct {
		Code string `json:"code"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&request) != nil || !validLinkCode(request.Code) {
		pairingError(w, http.StatusBadRequest, "请输入六位数字连接码")
		return
	}
	h.mu.RLock()
	registration, exists := h.pairings[request.Code]
	valid := exists && registration.expiresAt.After(now) && h.hosts[registration.host.id] == registration.host
	h.mu.RUnlock()
	if !valid {
		pairingError(w, http.StatusNotFound, "连接码无效或已过期，请检查电脑端显示的连接码")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	publicKey, err := protocol.EncodePublicKey(registration.host.public)
	if err != nil {
		pairingError(w, http.StatusInternalServerError, "invalid host public key")
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"version": 1, "hostId": registration.host.id,
		"hostPublicKey": publicKey,
		"code":          registration.token,
	})
}

func validLinkCode(code string) bool {
	if len(code) != 6 {
		return false
	}
	for _, char := range code {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func (h *Hub) allowLookupLocked(address string, now time.Time) bool {
	for key, window := range h.lookups {
		if now.Sub(window.started) >= time.Minute {
			delete(h.lookups, key)
		}
	}
	if now.Sub(h.globalLookup.started) >= time.Minute {
		h.globalLookup = lookupWindow{started: now}
	}
	window := h.lookups[address]
	if window.started.IsZero() {
		window.started = now
	}
	if window.count >= 5 || h.globalLookup.count >= 60 {
		return false
	}
	window.count++
	h.lookups[address] = window
	h.globalLookup.count++
	return true
}

func (h *Hub) removeHostPairingLocked(host *peer) {
	for code, registration := range h.pairings {
		if registration.host == host {
			delete(h.pairings, code)
		}
	}
}

func (h *Hub) consumePairing(host *peer, envelope protocol.Envelope) {
	var offer linkOffer
	if protocol.DecodePayload(envelope, &offer) != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for code, registration := range h.pairings {
		if registration.host == host && registration.token == offer.Token {
			delete(h.pairings, code)
		}
	}
}
