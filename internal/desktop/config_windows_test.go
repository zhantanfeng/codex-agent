package desktop

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"codexremote/internal/agent"
)

func newConfigurationTestApp(t *testing.T) (*App, *agent.Config) {
	t.Helper()
	dir := t.TempDir()
	store := &agent.Store{Dir: dir, ConfigPath: filepath.Join(dir, "config.json"), KeyPath: filepath.Join(dir, "identity.bin")}
	config := &agent.Config{
		Version: 1, HostID: "existing-host", RelayURL: "wss://old.example/ws", CodexCommand: "custom-codex.exe",
		Projects: map[string]string{"work": `D:\work`}, Clients: map[string]string{"phone": "existing-public-key"},
		Pending: map[string]agent.PendingClient{"pending-phone": {PublicKey: "pending-key"}},
		Pairing: &agent.Pairing{Code: "old-code", ExpiresAt: time.Now().Add(time.Minute)},
	}
	if err := store.Save(config); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.KeyPath, []byte("existing-protected-identity"), 0600); err != nil {
		t.Fatal(err)
	}
	return &App{store: store}, config
}

func TestConfigureChangesRelayAndPreservesExistingAuthorization(t *testing.T) {
	a, want := newConfigurationTestApp(t)
	identityBefore, err := os.ReadFile(a.store.KeyPath)
	if err != nil {
		t.Fatal(err)
	}
	a.lastError = "old connection error"
	if err := a.Configure("  ws://43.155.135.230:8080/ws  "); err != nil {
		t.Fatal(err)
	}
	got, err := a.store.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	want.RelayURL, want.Pairing, want.Pending = "ws://43.155.135.230:8080/ws", nil, map[string]agent.PendingClient{}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("configuration changed unexpectedly: got %#v, want %#v", got, want)
	}
	identityAfter, err := os.ReadFile(a.store.KeyPath)
	if err != nil || !bytes.Equal(identityAfter, identityBefore) {
		t.Fatalf("existing identity was changed: %v", err)
	}
	if a.cancel != nil || a.lastError != "" {
		t.Fatal("saving a stopped configuration should keep the background stopped and clear the previous error")
	}
}

func TestConfigureInvalidRelayDoesNotStopOrChangeConfiguration(t *testing.T) {
	for _, relayURL := range []string{"", "https://relay.example/ws", "ws://", "wss:///ws", "ws://relay.example:bad/ws", "ws://relay.example/ws#fragment"} {
		t.Run(relayURL, func(t *testing.T) {
			a, _ := newConfigurationTestApp(t)
			before, err := os.ReadFile(a.store.ConfigPath)
			if err != nil {
				t.Fatal(err)
			}
			stopped := false
			a.cancel = func() { stopped = true }
			if err := a.Configure(relayURL); err == nil {
				t.Fatal("invalid relay address was accepted")
			}
			after, err := os.ReadFile(a.store.ConfigPath)
			if err != nil || !bytes.Equal(before, after) || stopped {
				t.Fatalf("invalid input changed or stopped the existing configuration: %v", err)
			}
		})
	}
}

func TestConfigureUnchangedRelayDoesNotInterruptBackground(t *testing.T) {
	a, config := newConfigurationTestApp(t)
	stopped := false
	a.cancel = func() { stopped = true }
	if err := a.Configure(config.RelayURL); err != nil {
		t.Fatal(err)
	}
	if stopped {
		t.Fatal("saving the same address stopped the background")
	}
}

func TestConfigureRunningBackgroundStopsBeforeSavingAndRestarts(t *testing.T) {
	a, _ := newConfigurationTestApp(t)
	// Generate a protected identity in this test's own directory for start's validation.
	identityDir := t.TempDir()
	identityStore := &agent.Store{Dir: identityDir, ConfigPath: filepath.Join(identityDir, "config.json"), KeyPath: filepath.Join(identityDir, "identity.bin")}
	if _, _, err := identityStore.Initialize("wss://old.example/ws"); err != nil {
		t.Fatal(err)
	}
	identity, err := os.ReadFile(identityStore.KeyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.store.KeyPath, identity, 0600); err != nil {
		t.Fatal(err)
	}
	a.power = newPowerKeeper()
	defer a.power.close()
	// A cancelled parent allows restart to finish without launching Codex or using the network.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	a.ctx = ctx
	oldDone := make(chan struct{})
	a.done = oldDone
	stopped := false
	a.cancel = func() {
		config, err := a.store.LoadConfig()
		if err != nil || config.RelayURL != "wss://old.example/ws" {
			t.Fatal("relay configuration was changed before the old background stopped")
		}
		// Simulate a final config update made by the stopping Agent.
		config.Clients["last-paired-phone"] = "last-public-key"
		if err := a.store.Save(config); err != nil {
			t.Fatal(err)
		}
		stopped = true
		a.mu.Lock()
		a.cancel, a.stopping = nil, false
		a.mu.Unlock()
		close(oldDone)
	}
	if err := a.Configure("wss://new.example/ws"); err != nil {
		t.Fatal(err)
	}
	if !stopped || a.done == oldDone {
		t.Fatal("changing a running configuration must stop and restart the background")
	}
	select {
	case <-a.done:
	case <-time.After(2 * time.Second):
		t.Fatal("restarted background did not shut down with its cancelled parent")
	}
	config, err := a.store.LoadConfig()
	if err != nil || config.RelayURL != "wss://new.example/ws" || config.Clients["last-paired-phone"] != "last-public-key" {
		t.Fatalf("new relay or final Agent updates were not saved: %#v, %v", config, err)
	}
}

func TestConfigureDoesNotOverwriteBrokenConfiguration(t *testing.T) {
	a, _ := newConfigurationTestApp(t)
	broken := []byte("not valid JSON")
	if err := os.WriteFile(a.store.ConfigPath, broken, 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.Configure("wss://new.example/ws"); err == nil {
		t.Fatal("broken existing configuration was accepted")
	}
	after, err := os.ReadFile(a.store.ConfigPath)
	if err != nil || !bytes.Equal(after, broken) {
		t.Fatalf("broken configuration was overwritten: %v", err)
	}
}
