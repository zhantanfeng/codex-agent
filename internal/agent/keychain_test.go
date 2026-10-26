package agent

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestKeychainIdentityRoundTripKeepsSecretOutOfArgumentsAndDisk(t *testing.T) {
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{42}, ed25519.SeedSize))
	encoded := base64.StdEncoding.EncodeToString(private)
	var account string
	write := func(input string, args ...string) ([]byte, error) {
		if !reflect.DeepEqual(args, []string{"-i"}) {
			t.Fatalf("secret must be sent over stdin, got arguments %q", args)
		}
		fields := strings.Fields(input)
		if len(fields) != 7 || fields[0] != "add-generic-password" || fields[3] != "-s" || fields[4] != keychainService || fields[5] != "-w" || fields[6] != encoded {
			t.Fatalf("unexpected Keychain write request")
		}
		account = fields[2]
		return nil, nil
	}
	protected, err := protectKeychainSecret(private, write)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(protected, private) || strings.Contains(string(protected), encoded) {
		t.Fatal("disk reference contains the private key")
	}
	read := func(input string, args ...string) ([]byte, error) {
		want := []string{"find-generic-password", "-a", account, "-s", keychainService, "-w"}
		if input != "" || !reflect.DeepEqual(args, want) {
			t.Fatalf("unexpected lookup: %q", args)
		}
		return []byte(encoded + "\n"), nil
	}
	actual, err := unprotectKeychainSecret(protected, read)
	if err != nil || !bytes.Equal(actual, private) {
		t.Fatalf("identity did not round trip: %v", err)
	}
}

func TestKeychainRejectsInvalidReferencesAndMissingOrCorruptKeys(t *testing.T) {
	for _, reference := range []string{"", "windows-dpapi", keychainReferencePrefix + "short", keychainReferencePrefix + strings.Repeat("z", 32)} {
		_, err := unprotectKeychainSecret([]byte(reference), func(string, ...string) ([]byte, error) {
			t.Fatal("invalid references must not access the Keychain")
			return nil, nil
		})
		if err == nil {
			t.Fatal("invalid identity accepted")
		}
	}
	ref := []byte(keychainReferencePrefix + strings.Repeat("a", 32))
	for _, output := range []string{"not-base64", base64.StdEncoding.EncodeToString([]byte("short"))} {
		if _, err := unprotectKeychainSecret(ref, func(string, ...string) ([]byte, error) { return []byte(output), nil }); err == nil {
			t.Fatal("corrupt key accepted")
		}
	}
	failure := errors.New("keychain locked")
	_, err := unprotectKeychainSecret(ref, func(string, ...string) ([]byte, error) { return []byte("secret output"), failure })
	if !errors.Is(err, failure) || strings.Contains(err.Error(), "secret output") {
		t.Fatalf("Keychain error lost or leaked output: %v", err)
	}
	if _, err := protectKeychainSecret(make([]byte, ed25519.PrivateKeySize), func(string, ...string) ([]byte, error) { return nil, failure }); !errors.Is(err, failure) {
		t.Fatal("failed write must not create a reference")
	}
}
