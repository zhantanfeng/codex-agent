# Architecture and Trust Boundaries

```text
Android App                         Public Relay                    Windows Agent
------------                        ------------                    -------------
Android Keystore                    in-memory peers                 Windows DPAPI
Ed25519 signing   -- ws:// -->      signature/replay check  -->     signature check
Compose UI                           route by host/client IDs        codex app-server
                                                                      stdin/stdout
```

## What is protected

- Only a paired client public key is accepted by the Windows Agent.
- Signed fields and payloads cannot be modified without detection.
- A session ID plus monotonically increasing sequence number rejects replayed frames.
- Timestamps limit captured frames to a five-minute acceptance window.
- Initial trust requires matching a six-digit short authentication string on Windows and Android.
- Windows project paths are an explicit allowlist.

## What is not protected

- The public Relay and on-path observers can read every Base64URL-decoded payload.
- An observer can block traffic or identify when a Codex task is running.
- The Relay can disconnect or withhold messages.
- A malicious Relay can cause denial of service by replacing a connection, but it cannot produce a signature accepted by the Windows Agent.

## Codex boundary

The Agent launches `codex app-server --stdio`, initializes its JSON-RPC protocol, and remains the only app-server client. The phone protocol never exposes arbitrary JSON-RPC passthrough. Every action is mapped to a bounded operation, and a thread must first be discovered from an allowlisted working directory.

The adapter supports:

- `thread/list`, `thread/start`, `thread/resume`
- `turn/start`, `turn/steer`, `turn/interrupt`
- streamed server notifications
- command and file-change approval requests

Other app-server requests are not automatically approved.

