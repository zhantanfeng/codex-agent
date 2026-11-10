package agent

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

const keychainService = "dev.dcode.agent.identity"
const keychainReferencePrefix = "dcode-keychain-v1:"

type securityCommand func(input string, args ...string) ([]byte, error)

// Only an opaque Keychain reference is written to identity.bin on macOS.
// Send the secret through stdin, so it never appears in process arguments.
func protectKeychainSecret(plain []byte, run securityCommand) ([]byte, error) {
	if len(plain) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid private key size")
	}
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return nil, err
	}
	account := hex.EncodeToString(id[:])
	input := fmt.Sprintf("add-generic-password -a %s -s %s -w %s\n", account, keychainService, base64.StdEncoding.EncodeToString(plain))
	if _, err := run(input, "-i"); err != nil {
		return nil, fmt.Errorf("save identity in macOS Keychain (unlock your login keychain and retry): %w", err)
	}
	return []byte(keychainReferencePrefix + account), nil
}

func unprotectKeychainSecret(protected []byte, run securityCommand) ([]byte, error) {
	if !strings.HasPrefix(string(protected), keychainReferencePrefix) {
		return nil, errors.New("invalid macOS identity; Windows identities cannot be copied to a Mac")
	}
	account := strings.TrimPrefix(string(protected), keychainReferencePrefix)
	id, err := hex.DecodeString(account)
	if err != nil || len(id) != 16 {
		return nil, errors.New("invalid macOS Keychain reference")
	}
	output, err := run("", "find-generic-password", "-a", account, "-s", keychainService, "-w")
	if err != nil {
		return nil, fmt.Errorf("read identity from macOS Keychain (unlock your login keychain and retry): %w", err)
	}
	plain, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(output)))
	if err != nil || len(plain) != ed25519.PrivateKeySize {
		return nil, errors.New("invalid identity in macOS Keychain")
	}
	return plain, nil
}
