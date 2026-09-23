package protocol

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const Version = 1

var rawURL = base64.RawURLEncoding

type Envelope struct {
	Version   int    `json:"version"`
	SenderID  string `json:"senderId"`
	TargetID  string `json:"targetId"`
	SessionID string `json:"sessionId"`
	Sequence  uint64 `json:"sequence"`
	Timestamp int64  `json:"timestamp"`
	Nonce     string `json:"nonce"`
	Kind      string `json:"kind"`
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}

type Signer struct {
	ID        string
	private   ed25519.PrivateKey
	publicDER []byte
	mu        sync.Mutex
	sequence  uint64
	sessionID string
}

func NewKeyPair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}

func NewSigner(id string, private ed25519.PrivateKey) (*Signer, error) {
	if err := validateToken(id, "sender id"); err != nil {
		return nil, err
	}
	publicDER, err := x509.MarshalPKIXPublicKey(private.Public())
	if err != nil {
		return nil, fmt.Errorf("marshal public key: %w", err)
	}
	session, err := randomToken(18)
	if err != nil {
		return nil, err
	}
	return &Signer{ID: id, private: private, publicDER: publicDER, sessionID: session}, nil
}

func (s *Signer) PublicKeyBase64() string {
	return rawURL.EncodeToString(s.publicDER)
}

func (s *Signer) Sign(target, kind string, payload any) (Envelope, error) {
	if err := validateToken(target, "target id"); err != nil {
		return Envelope{}, err
	}
	if err := validateToken(kind, "kind"); err != nil {
		return Envelope{}, err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, fmt.Errorf("marshal payload: %w", err)
	}
	nonce, err := randomToken(18)
	if err != nil {
		return Envelope{}, err
	}
	s.mu.Lock()
	s.sequence++
	env := Envelope{
		Version: Version, SenderID: s.ID, TargetID: target, SessionID: s.sessionID,
		Sequence: s.sequence, Timestamp: time.Now().UnixMilli(), Nonce: nonce,
		Kind: kind, Payload: rawURL.EncodeToString(body),
	}
	env.Signature = rawURL.EncodeToString(ed25519.Sign(s.private, canonical(env)))
	s.mu.Unlock()
	return env, nil
}

func ParsePublicKey(encoded string) (ed25519.PublicKey, error) {
	der, err := rawURL.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode public key: %w", err)
	}
	parsed, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}
	public, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("public key is not Ed25519")
	}
	return public, nil
}

func EncodePublicKey(public ed25519.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(public)
	if err != nil {
		return "", err
	}
	return rawURL.EncodeToString(der), nil
}

func Verify(env Envelope, public ed25519.PublicKey) error {
	if env.Version != Version {
		return fmt.Errorf("unsupported protocol version %d", env.Version)
	}
	for label, value := range map[string]string{
		"sender id": env.SenderID, "target id": env.TargetID, "session id": env.SessionID,
		"nonce": env.Nonce, "kind": env.Kind,
	} {
		if err := validateToken(value, label); err != nil {
			return err
		}
	}
	if env.Sequence == 0 {
		return errors.New("sequence must be positive")
	}
	sig, err := rawURL.DecodeString(env.Signature)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	if !ed25519.Verify(public, canonical(env), sig) {
		return errors.New("invalid signature")
	}
	return nil
}

func canonical(env Envelope) []byte {
	digest := sha256.Sum256([]byte(env.Payload))
	return []byte(fmt.Sprintf("%d\n%s\n%s\n%s\n%d\n%d\n%s\n%s\n%s",
		env.Version, env.SenderID, env.TargetID, env.SessionID, env.Sequence,
		env.Timestamp, env.Nonce, env.Kind, hex.EncodeToString(digest[:])))
}

func DecodePayload(env Envelope, target any) error {
	body, err := rawURL.DecodeString(env.Payload)
	if err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("unmarshal payload: %w", err)
	}
	return nil
}

func validateToken(value, label string) error {
	if value == "" || strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("invalid %s", label)
	}
	if len(value) > 256 {
		return fmt.Errorf("%s is too long", label)
	}
	return nil
}

func randomToken(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}
	return rawURL.EncodeToString(value), nil
}

type ReplayGuard struct {
	mu       sync.Mutex
	sessions map[string]replaySession
	MaxAge   time.Duration
}

type replaySession struct {
	lastSequence uint64
	lastSeen     time.Time
}

func NewReplayGuard(maxAge time.Duration) *ReplayGuard {
	return &ReplayGuard{sessions: make(map[string]replaySession), MaxAge: maxAge}
}

func (g *ReplayGuard) Accept(env Envelope, public ed25519.PublicKey, now time.Time) error {
	if err := Verify(env, public); err != nil {
		return err
	}
	messageTime := time.UnixMilli(env.Timestamp)
	if messageTime.Before(now.Add(-g.MaxAge)) || messageTime.After(now.Add(30*time.Second)) {
		return errors.New("message timestamp is outside the accepted window")
	}
	key := env.SenderID + ":" + env.SessionID
	g.mu.Lock()
	defer g.mu.Unlock()
	if previous, ok := g.sessions[key]; ok && env.Sequence <= previous.lastSequence {
		return errors.New("replayed or out-of-order message")
	}
	g.sessions[key] = replaySession{lastSequence: env.Sequence, lastSeen: now}
	if len(g.sessions) > 4096 {
		cutoff := now.Add(-2 * g.MaxAge)
		for id, state := range g.sessions {
			if state.lastSeen.Before(cutoff) {
				delete(g.sessions, id)
			}
		}
	}
	return nil
}
