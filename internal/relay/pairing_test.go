package relay

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codexremote/internal/protocol"
)

func registerTestHost(t *testing.T, hub *Hub, hostID string) (*peer, *protocol.Signer) {
	t.Helper()
	public, private, _ := ed25519.GenerateKey(nil)
	signer, _ := protocol.NewSigner(hostID, private)
	host := &peer{id: hostID, hostID: hostID, role: "agent", public: public}
	hub.hosts[hostID] = host
	return host, signer
}

func pairingPost(handler http.Handler, path string, value any) *httptest.ResponseRecorder {
	raw, _ := json.Marshal(value)
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	request.RemoteAddr = "192.0.2.1:1234"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

func TestLinkCodeRegistrationLookupAndConsumption(t *testing.T) {
	hub := NewHub(slog.New(slog.NewTextHandler(io.Discard, nil)))
	handler := hub.Handler()
	host, signer := registerTestHost(t, hub, "host-test")
	token := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	offer := linkOffer{Token: token, ExpiresAt: time.Now().Add(10 * time.Minute).UnixMilli()}
	envelope, _ := signer.Sign("relay", "pair.register", offer)
	response := pairingPost(handler, "/pairing/register", envelope)
	if response.Code != 200 {
		t.Fatalf("register: %d %s", response.Code, response.Body.String())
	}
	var registration struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &registration)
	if !validLinkCode(registration.Code) {
		t.Fatalf("invalid code %q", registration.Code)
	}
	lookup := pairingPost(handler, "/pairing/resolve", map[string]string{"code": registration.Code})
	var result struct {
		HostID    string `json:"hostId"`
		PublicKey string `json:"hostPublicKey"`
		Token     string `json:"code"`
	}
	_ = json.Unmarshal(lookup.Body.Bytes(), &result)
	public, err := protocol.ParsePublicKey(result.PublicKey)
	if lookup.Code != 200 || result.HostID != host.id || result.Token != token || err != nil || !bytes.Equal(public, host.public) {
		t.Fatalf("lookup: %d %s", lookup.Code, lookup.Body.String())
	}
	// A fresh registration of the same offer keeps the displayed code stable.
	envelope, _ = signer.Sign("relay", "pair.register", offer)
	stable := pairingPost(handler, "/pairing/register", envelope)
	if !strings.Contains(stable.Body.String(), registration.Code) {
		t.Fatal("registration unexpectedly changed the code")
	}
	consumed, _ := signer.Sign("*", "pair.consumed", offer)
	raw, _ := json.Marshal(consumed)
	if err := hub.route(host, raw, consumed); err != nil {
		t.Fatal(err)
	}
	if response := pairingPost(handler, "/pairing/resolve", map[string]string{"code": registration.Code}); response.Code != 404 {
		t.Fatal("consumed code can still be resolved")
	}
}

func TestLinkRegistrationRejectsSpoofedAndReplayedRequests(t *testing.T) {
	hub := NewHub(slog.New(slog.NewTextHandler(io.Discard, nil)))
	handler := hub.Handler()
	_, signer := registerTestHost(t, hub, "host-test")
	_, impostorKey, _ := ed25519.GenerateKey(nil)
	impostor, _ := protocol.NewSigner("host-test", impostorKey)
	offer := linkOffer{Token: base64.RawURLEncoding.EncodeToString(make([]byte, 32)), ExpiresAt: time.Now().Add(time.Minute).UnixMilli()}
	fake, _ := impostor.Sign("relay", "pair.register", offer)
	if pairingPost(handler, "/pairing/register", fake).Code != 403 {
		t.Fatal("accepted an impostor host")
	}
	valid, _ := signer.Sign("relay", "pair.register", offer)
	if pairingPost(handler, "/pairing/register", valid).Code != 200 {
		t.Fatal("valid registration rejected")
	}
	if pairingPost(handler, "/pairing/register", valid).Code != 403 {
		t.Fatal("replayed registration accepted")
	}
}

func TestLinkCodeExpiryDisconnectAndRateLimit(t *testing.T) {
	hub := NewHub(slog.New(slog.NewTextHandler(io.Discard, nil)))
	host, _ := registerTestHost(t, hub, "host-test")
	hub.pairings["000123"] = linkRegistration{host: host, expiresAt: time.Now().Add(-time.Second)}
	handler := hub.Handler()
	if pairingPost(handler, "/pairing/resolve", map[string]string{"code": "000123"}).Code != 404 {
		t.Fatal("expired code accepted")
	}
	hub.pairings["000123"] = linkRegistration{host: host, expiresAt: time.Now().Add(time.Minute)}
	hub.remove(host)
	if len(hub.pairings) != 0 {
		t.Fatal("offline host retained a connection code")
	}
	for i := 0; i < 4; i++ {
		pairingPost(handler, "/pairing/resolve", map[string]string{"code": "000123"})
	}
	if pairingPost(handler, "/pairing/resolve", map[string]string{"code": "000123"}).Code != 429 {
		t.Fatal("sixth lookup was not rate limited")
	}
	hub.mu.Lock()
	allowed := hub.allowLookupLocked("192.0.2.1", time.Now().Add(61*time.Second))
	hub.mu.Unlock()
	if !allowed {
		t.Fatal("rate limit did not expire")
	}
}

func TestLinkCodeKeepsLeadingZeros(t *testing.T) {
	for _, code := range []string{"000000", "000123", "999999"} {
		if !validLinkCode(code) {
			t.Fatalf("valid code rejected: %s", code)
		}
	}
	for _, code := range []string{"12345", "1234567", "１２３４５６", "12345x", " 12345"} {
		if validLinkCode(code) {
			t.Fatalf("invalid code accepted: %s", code)
		}
	}
}
