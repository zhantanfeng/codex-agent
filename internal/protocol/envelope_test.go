package protocol

import (
	"crypto/ed25519"
	"testing"
	"time"
)

func TestEnvelopeSignVerifyAndReplay(t *testing.T) {
	_, private, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := NewSigner("phone-1", private)
	if err != nil {
		t.Fatal(err)
	}
	env, err := signer.Sign("host-1", "command", map[string]string{"action": "list_projects"})
	if err != nil {
		t.Fatal(err)
	}
	public, err := ParsePublicKey(signer.PublicKeyBase64())
	if err != nil {
		t.Fatal(err)
	}
	guard := NewReplayGuard(5 * time.Minute)
	if err := guard.Accept(env, public, time.Now()); err != nil {
		t.Fatalf("valid envelope rejected: %v", err)
	}
	if err := guard.Accept(env, public, time.Now()); err == nil {
		t.Fatal("replayed envelope was accepted")
	}
}

func TestEnvelopeTamperingFails(t *testing.T) {
	_, private, _ := ed25519.GenerateKey(nil)
	signer, _ := NewSigner("phone-1", private)
	env, _ := signer.Sign("host-1", "command", map[string]string{"text": "safe"})
	env.Payload += "x"
	if err := Verify(env, private.Public().(ed25519.PublicKey)); err == nil {
		t.Fatal("tampered envelope was accepted")
	}
}
