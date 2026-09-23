package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type CodexMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  json.RawMessage `json:"error,omitempty"`
}

type CodexClient struct {
	log       *slog.Logger
	cmd       *exec.Cmd
	stdin     io.WriteCloser
	writeMu   sync.Mutex
	pendingMu sync.Mutex
	pending   map[string]chan CodexMessage
	nextID    atomic.Uint64
	onMessage func(CodexMessage)
	done      chan error
}

const supportedCodexVersionPrefix = "codex-cli 0.156."

func StartCodex(ctx context.Context, command string, log *slog.Logger, onMessage func(CodexMessage)) (*CodexClient, error) {
	executable, err := resolveCodexExecutable(command)
	if err != nil {
		return nil, err
	}
	versionOutput, err := exec.CommandContext(ctx, executable, "--version").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("check Codex CLI version: %w: %s", err, strings.TrimSpace(string(versionOutput)))
	}
	version := strings.TrimSpace(string(versionOutput))
	if !strings.HasPrefix(version, supportedCodexVersionPrefix) {
		return nil, fmt.Errorf("unsupported Codex CLI %q; this build requires 0.156.x", version)
	}
	cmd := exec.CommandContext(ctx, executable, "app-server", "--stdio")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	client := &CodexClient{log: log, cmd: cmd, stdin: stdin, pending: make(map[string]chan CodexMessage), onMessage: onMessage, done: make(chan error, 1)}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s app-server: %w", command, err)
	}
	go client.read(stdout)
	go client.readLogs(stderr)
	go func() { client.done <- cmd.Wait() }()
	initCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	var initialized map[string]any
	if err := client.Call(initCtx, "initialize", map[string]any{
		"clientInfo":   map[string]any{"name": "codex-remote-agent", "title": "Codex Remote", "version": "0.1.0"},
		"capabilities": map[string]any{"experimentalApi": false, "requestAttestation": false},
	}, &initialized); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("initialize app-server: %w", err)
	}
	if err := client.notify("initialized", nil); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

func resolveCodexExecutable(command string) (string, error) {
	if runtime.GOOS == "windows" && filepath.Ext(command) == "" {
		for _, extension := range []string{".exe", ".cmd", ".bat"} {
			if resolved, err := exec.LookPath(command + extension); err == nil {
				return resolved, nil
			}
		}
	}
	resolved, err := exec.LookPath(command)
	if err != nil {
		return "", fmt.Errorf("find Codex CLI %q: %w", command, err)
	}
	return resolved, nil
}

func (c *CodexClient) Call(ctx context.Context, method string, params any, target any) error {
	id := c.nextID.Add(1)
	key := fmt.Sprint(id)
	response := make(chan CodexMessage, 1)
	c.pendingMu.Lock()
	c.pending[key] = response
	c.pendingMu.Unlock()
	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, key)
		c.pendingMu.Unlock()
	}()
	if err := c.write(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case message := <-response:
		if len(message.Error) > 0 && string(message.Error) != "null" {
			return fmt.Errorf("app-server %s: %s", method, message.Error)
		}
		if target != nil && len(message.Result) > 0 {
			if err := json.Unmarshal(message.Result, target); err != nil {
				return fmt.Errorf("decode %s result: %w", method, err)
			}
		}
		return nil
	}
}

func (c *CodexClient) Respond(id json.RawMessage, result any) error {
	if len(id) == 0 {
		return errors.New("missing app-server request id")
	}
	return c.write(map[string]any{"id": json.RawMessage(id), "result": result})
}

func (c *CodexClient) notify(method string, params any) error {
	message := map[string]any{"method": method}
	if params != nil {
		message["params"] = params
	}
	return c.write(message)
}

func (c *CodexClient) write(value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err = c.stdin.Write(append(raw, '\n'))
	return err
}

func (c *CodexClient) read(reader io.Reader) {
	decoder := json.NewDecoder(reader)
	for {
		var message CodexMessage
		if err := decoder.Decode(&message); err != nil {
			if !errors.Is(err, io.EOF) {
				c.log.Error("app-server output failed", "error", err)
			}
			return
		}
		if len(message.ID) > 0 && message.Method == "" {
			key := string(message.ID)
			c.pendingMu.Lock()
			waiter := c.pending[key]
			c.pendingMu.Unlock()
			if waiter != nil {
				waiter <- message
			}
			continue
		}
		if c.onMessage != nil {
			c.onMessage(message)
		}
	}
}

func (c *CodexClient) readLogs(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		c.log.Debug("app-server", "message", scanner.Text())
	}
}

func (c *CodexClient) Close() error {
	_ = c.stdin.Close()
	if c.cmd.Process != nil {
		return c.cmd.Process.Kill()
	}
	return nil
}
