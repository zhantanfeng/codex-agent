package main

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"codexremote/internal/agent"
	"codexremote/internal/protocol"
	qrcode "github.com/skip2/go-qrcode"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	store, err := agent.DefaultStore()
	if err != nil {
		fatal(err)
	}
	switch os.Args[1] {
	case "init":
		fs := flag.NewFlagSet("init", flag.ExitOnError)
		relayURL := fs.String("relay", "", "Relay WebSocket URL, for example ws://203.0.113.10:8080/ws")
		_ = fs.Parse(os.Args[2:])
		if *relayURL == "" {
			fatal(errors.New("-relay is required"))
		}
		config, _, err := store.Initialize(*relayURL)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("Initialized host %s in %s\n", config.HostID, store.Dir)
	case "run":
		config, private, err := store.Load()
		if err != nil {
			fatal(err)
		}
		log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
		runtime, err := agent.NewRuntime(store, config, private, log)
		if err != nil {
			fatal(err)
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := runtime.Run(ctx); err != nil {
			fatal(err)
		}
	case "project":
		project(store, os.Args[2:])
	case "pair":
		pair(store)
	case "device":
		device(store, os.Args[2:])
	case "status":
		config, _, err := store.Load()
		if err != nil {
			fatal(err)
		}
		fmt.Printf("Host: %s\nRelay: %s\nProjects: %d\nPaired clients: %d\n", config.HostID, config.RelayURL, len(config.Projects), len(config.Clients))
	case "install":
		install()
	default:
		usage()
		os.Exit(2)
	}
}

func project(store *agent.Store, args []string) {
	if len(args) == 0 {
		fatal(errors.New("project requires add, list, or remove"))
	}
	switch args[0] {
	case "add":
		fs := flag.NewFlagSet("project add", flag.ExitOnError)
		id := fs.String("id", "", "Project identifier")
		path := fs.String("path", "", "Project directory")
		_ = fs.Parse(args[1:])
		config, err := store.AddProject(*id, *path)
		if err != nil {
			fatal(err)
		}
		fmt.Printf("Added %s -> %s\n", *id, config.Projects[*id])
	case "list":
		config, _, err := store.Load()
		if err != nil {
			fatal(err)
		}
		for id, path := range config.Projects {
			fmt.Printf("%s\t%s\n", id, path)
		}
	case "remove":
		fs := flag.NewFlagSet("project remove", flag.ExitOnError)
		id := fs.String("id", "", "Project identifier")
		_ = fs.Parse(args[1:])
		config, err := store.LoadConfig()
		if err != nil {
			fatal(err)
		}
		if _, ok := config.Projects[*id]; !ok {
			fatal(fmt.Errorf("unknown project %s", *id))
		}
		delete(config.Projects, *id)
		if err := store.Save(config); err != nil {
			fatal(err)
		}
		fmt.Printf("Removed %s from the allowlist; project files were not changed.\n", *id)
	default:
		fatal(errors.New("project requires add, list, or remove"))
	}
}

func pair(store *agent.Store) {
	config, private, err := store.Load()
	if err != nil {
		fatal(err)
	}
	random := make([]byte, 6)
	if _, err := rand.Read(random); err != nil {
		fatal(err)
	}
	code := strings.ToUpper(base64.RawURLEncoding.EncodeToString(random))
	config.Pairing = &agent.Pairing{Code: code, ExpiresAt: time.Now().Add(10 * time.Minute)}
	config.Pending = map[string]agent.PendingClient{}
	if err := store.Save(config); err != nil {
		fatal(err)
	}
	signer, err := protocol.NewSigner(config.HostID, private)
	if err != nil {
		fatal(err)
	}
	payload, _ := json.Marshal(map[string]any{"version": 1, "relayUrl": config.RelayURL, "hostId": config.HostID, "hostPublicKey": signer.PublicKeyBase64(), "code": code})
	encoded := "codexremote://pair?data=" + base64.RawURLEncoding.EncodeToString(payload)
	qr, err := qrcode.New(encoded, qrcode.Medium)
	if err != nil {
		fatal(err)
	}
	fmt.Println(qr.ToSmallString(false))
	fmt.Printf("Pairing payload (valid for 10 minutes):\n%s\n\n", encoded)
	fmt.Println("Scan the code. The phone will show a six-digit verification code.")
	fmt.Println("Waiting for a pairing request...")
	deadline := time.Now().Add(10 * time.Minute)
	for time.Now().Before(deadline) {
		latest, loadErr := store.LoadConfig()
		if loadErr == nil && len(latest.Pending) > 0 {
			fmt.Println("Pending phone verification codes:")
			for clientID, pending := range latest.Pending {
				if time.Now().Before(pending.ExpiresAt) {
					fmt.Printf("  %s  %s\n", pending.SAS, clientID)
				}
			}
			fmt.Print("Enter the code shown on your phone, or press Enter to reject: ")
			entered, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			entered = strings.TrimSpace(entered)
			latest, _ = store.LoadConfig()
			var approvedID string
			var approved agent.PendingClient
			for clientID, pending := range latest.Pending {
				if entered == pending.SAS && time.Now().Before(pending.ExpiresAt) {
					approvedID, approved = clientID, pending
					break
				}
			}
			if approvedID == "" {
				fatal(errors.New("pairing rejected: verification code did not match any pending phone"))
			}
			latest.Clients[approvedID] = approved.PublicKey
			latest.Pending = map[string]agent.PendingClient{}
			latest.Pairing = nil
			if err := store.Save(latest); err != nil {
				fatal(err)
			}
			fmt.Println("Phone approved. Keep the Agent running; the phone will finish pairing automatically.")
			return
		}
		time.Sleep(time.Second)
	}
	fatal(errors.New("pairing window expired"))
}

func install() {
	executable, err := os.Executable()
	if err != nil {
		fatal(err)
	}
	executable, _ = filepath.Abs(executable)
	taskCommand := fmt.Sprintf("\"%s\" run", executable)
	cmd := exec.Command("schtasks.exe", "/Create", "/SC", "ONLOGON", "/TN", "CodexRemoteAgent", "/TR", taskCommand, "/F")
	output, err := cmd.CombinedOutput()
	if err != nil {
		fatal(fmt.Errorf("install scheduled task: %w: %s", err, output))
	}
	fmt.Print(string(output))
}

func device(store *agent.Store, args []string) {
	if len(args) == 0 {
		fatal(errors.New("device requires list or revoke"))
	}
	config, err := store.LoadConfig()
	if err != nil {
		fatal(err)
	}
	switch args[0] {
	case "list":
		for id := range config.Clients {
			fmt.Println(id)
		}
	case "revoke":
		if len(args) != 2 {
			fatal(errors.New("usage: codex-remote device revoke <client-id>"))
		}
		if _, ok := config.Clients[args[1]]; !ok {
			fatal(fmt.Errorf("unknown client %s", args[1]))
		}
		delete(config.Clients, args[1])
		if err := store.Save(config); err != nil {
			fatal(err)
		}
		fmt.Printf("Revoked %s\n", args[1])
	default:
		fatal(errors.New("device requires list or revoke"))
	}
}

func usage() {
	fmt.Println("Usage: codex-remote <init|run|project|pair|device|status|install>")
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "error:", err)
	os.Exit(1)
}
