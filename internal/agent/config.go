package agent

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Config struct {
	Version      int                      `json:"version"`
	HostID       string                   `json:"hostId"`
	RelayURL     string                   `json:"relayUrl"`
	CodexCommand string                   `json:"codexCommand"`
	Projects     map[string]string        `json:"projects"`
	Clients      map[string]string        `json:"clients"`
	Pending      map[string]PendingClient `json:"pendingClients,omitempty"`
	Pairing      *Pairing                 `json:"pairing,omitempty"`
}

type PendingClient struct {
	PublicKey string    `json:"publicKey"`
	SAS       string    `json:"sas"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type Pairing struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type Store struct {
	Dir        string
	ConfigPath string
	KeyPath    string
}

func DefaultStore() (*Store, error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		userConfig, err := os.UserConfigDir()
		if err != nil {
			return nil, err
		}
		base = userConfig
	}
	dir := filepath.Join(base, "CodexRemote")
	return &Store{Dir: dir, ConfigPath: filepath.Join(dir, "config.json"), KeyPath: filepath.Join(dir, "identity.bin")}, nil
}

func (s *Store) Initialize(relayURL string) (*Config, ed25519.PrivateKey, error) {
	if !strings.HasPrefix(relayURL, "ws://") && !strings.HasPrefix(relayURL, "wss://") {
		return nil, nil, errors.New("relay URL must start with ws:// or wss://")
	}
	if _, err := os.Stat(s.ConfigPath); err == nil {
		return nil, nil, fmt.Errorf("configuration already exists at %s", s.ConfigPath)
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return nil, nil, err
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	protected, err := protectSecret(private)
	if err != nil {
		return nil, nil, fmt.Errorf("protect identity: %w", err)
	}
	if err := os.WriteFile(s.KeyPath, protected, 0o600); err != nil {
		return nil, nil, err
	}
	hostID, err := randomID("host")
	if err != nil {
		return nil, nil, err
	}
	config := &Config{Version: 1, HostID: hostID, RelayURL: relayURL, CodexCommand: "codex", Projects: map[string]string{}, Clients: map[string]string{}, Pending: map[string]PendingClient{}}
	if err := s.Save(config); err != nil {
		return nil, nil, err
	}
	return config, private, nil
}

func (s *Store) Load() (*Config, ed25519.PrivateKey, error) {
	config, err := s.LoadConfig()
	if err != nil {
		return nil, nil, err
	}
	protected, err := os.ReadFile(s.KeyPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read identity: %w", err)
	}
	plain, err := unprotectSecret(protected)
	if err != nil {
		return nil, nil, fmt.Errorf("unprotect identity: %w", err)
	}
	if len(plain) != ed25519.PrivateKeySize {
		return nil, nil, errors.New("invalid private key size")
	}
	return config, ed25519.PrivateKey(plain), nil
}

func (s *Store) LoadConfig() (*Config, error) {
	raw, err := os.ReadFile(s.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var config Config
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if config.Version != 1 || config.HostID == "" || config.RelayURL == "" {
		return nil, errors.New("invalid configuration")
	}
	if config.Projects == nil {
		config.Projects = map[string]string{}
	}
	if config.Clients == nil {
		config.Clients = map[string]string{}
	}
	if config.Pending == nil {
		config.Pending = map[string]PendingClient{}
	}
	return &config, nil
}

func (s *Store) Save(config *Config) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	temporary := s.ConfigPath + ".tmp"
	if err := os.WriteFile(temporary, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, s.ConfigPath)
}

func (s *Store) AddProject(id, path string) (*Config, error) {
	config, _, err := s.Load()
	if err != nil {
		return nil, err
	}
	if id == "" || strings.ContainsAny(id, " \\/?#&") {
		return nil, errors.New("project id must be a simple non-empty token")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("project path is not a directory: %s", absolute)
	}
	config.Projects[id] = filepath.Clean(absolute)
	return config, s.Save(config)
}

func randomID(prefix string) (string, error) {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return prefix + "-" + base64.RawURLEncoding.EncodeToString(raw), nil
}
