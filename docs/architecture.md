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
- QR pairing requires matching a six-digit short authentication string on Windows and Android. The alternative connection-code flow authorizes the first phone presenting the Relay-resolved one-time token, without a separate Windows confirmation. Its six-digit code expires after ten minutes, is consumed after pairing, and lookup attempts are rate limited.
- Sessions are restricted to exact project directories. A paired phone can browse all folders accessible to the Windows account and explicitly add a selected directory when creating a session. Browsing does not itself add a project; selected directories are persisted for future sessions and Agent restarts.

## What is not protected

- The public Relay and on-path observers can read every Base64URL-decoded payload.
- An observer can block traffic or identify when a Codex task is running.
- The Relay can disconnect or withhold messages.
- Connection-code pairing trusts the Relay to supply the host identity. Anyone who obtains the connection code before it expires can pair; this flow has no second confirmation step. Use a trusted Relay and `wss://` for confidential transport. QR pairing retains the manual identity confirmation step.
- A malicious Relay can cause denial of service by replacing a connection, but it cannot produce a signature accepted by the Windows Agent.

## Codex boundary

The Agent launches `codex app-server --stdio`, initializes its JSON-RPC protocol, and remains the only app-server client. The phone protocol never exposes arbitrary JSON-RPC passthrough. Every action is mapped to a bounded operation, and a thread must first be discovered from an allowlisted working directory or created in a directory explicitly selected by a paired phone. `directories.list` returns only one level of folder names and paths, paginated at 200 entries by default (maximum 500); it never returns file contents. Directory browsing uses the same signature, pairing, and replay checks as other phone commands, and does not alter Codex's sandbox or approval policy.

The adapter supports:

- `thread/list`, `thread/start`, `thread/resume`
- `turn/start`, `turn/steer`, `turn/interrupt`
- streamed server notifications
- command and file-change approval requests

Other app-server requests are not automatically approved.
