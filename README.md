# Codex Remote

Codex Remote lets an Android phone control Codex running on a Windows computer. The Windows Agent owns the Codex process and project files. A small public Relay forwards signed WebSocket frames between the phone and the Agent.

> [!WARNING]
> This MVP intentionally uses plaintext `ws://` transport. Ed25519 signatures prevent command forgery, modification, and replay, but they do not hide prompts, source code, paths, or output. Do not use it with sensitive repositories. Do not expose the Relay as a long-term production service without adding TLS.

## Components

- `cmd/relay`: stateless Go WebSocket relay for a public Linux server.
- `cmd/agent`: Windows CLI/Agent that runs `codex app-server --stdio`.
- `android-app`: Android 12+ Compose client.
- `internal/protocol`: signed envelope and replay protection shared by the Go services.

The Agent supports Codex CLI `0.156.x` and `0.161.x`. Its adapter was checked against their generated app-server schemas; it fails fast on unsupported versions.

## Build

Prerequisites:

- Go 1.23+
- JDK 17 (Android Studio's JBR works)
- Android SDK Platform 35 and Build Tools 35.0.0

```powershell
$env:GOCACHE="$PWD\.cache\go-build"
$env:GOPATH="$PWD\.cache\gopath"
go test ./...
go build -o bin/codex-relay.exe ./cmd/relay
go build -o bin/codex-remote.exe ./cmd/agent

$env:JAVA_HOME='C:\Program Files\Android\Android Studio\jbr'
./gradlew.bat :android-app:testDebugUnitTest :android-app:assembleDebug
```

The debug APK is written to `android-app/build/outputs/apk/debug/android-app-debug.apk`.

## Deploy the Relay

On the public Linux server, copy this repository or build the image from it:

```bash
cd deploy/relay
docker compose up -d --build
curl http://127.0.0.1:8080/healthz
```

Open TCP port `8080` in the server firewall/security group. The Relay does not persist messages, keys, prompts, or Codex output. It logs connection metadata only.

For the planned plaintext version, the Android endpoint is:

```text
ws://PUBLIC_IP:8080/ws
```

## Configure Windows

Install Codex CLI `0.156.x` or `0.161.x`, sign in normally, and verify `codex --version` works in the same Windows account that will run the Agent.

```powershell
bin\codex-remote.exe init -relay ws://PUBLIC_IP:8080/ws
bin\codex-remote.exe project add -id my-project -path D:\projects\my-project
bin\codex-remote.exe run
```

Keep `run` active during initial setup. In a second terminal, start pairing:

```powershell
bin\codex-remote.exe pair
```

Scan the QR code in the Android app. The phone and terminal display a six-digit verification code. Enter the phone's code in the Windows terminal only when both values match. This confirmation is required because the bootstrap code travels over a plaintext network.

After verifying normal operation, register the Agent for startup after Windows login:

```powershell
bin\codex-remote.exe install
```

Other management commands:

```powershell
bin\codex-remote.exe status
bin\codex-remote.exe project list
bin\codex-remote.exe project remove -id my-project
bin\codex-remote.exe device list
bin\codex-remote.exe device revoke PHONE_ID
```

Device revocation and project changes take effect on the next phone message without restarting the Agent.

## Runtime Behavior

- The phone can only list and open threads whose working directory exactly matches a Windows project allowlist entry.
- Codex credentials never leave Windows.
- The Agent inherits the existing Codex sandbox and approval policy.
- Approval prompts are sent to the phone and resolved against the original app-server request ID.
- A second message sent during an active turn is queued by the Agent. The **Steer** action explicitly redirects the active turn.
- The Agent buffers the latest 500 Codex events in memory. A reconnecting phone asks for events after its last event ID.
- The Relay rejects invalid signatures, timestamps outside the allowed window, and duplicate/out-of-order sequence numbers.
- Private keys are protected with Windows DPAPI and an Android Keystore AES key. Message bodies remain plaintext by design in this version.

## Image attachments

Update both the Android app and Windows Agent to `0.1.8`. The existing Relay container can continue running.

In a loaded conversation, use the image button beside the message field to choose JPEG or PNG pictures. A message can contain up to four images, with or without text. Selected images have previews and a remove button. Large images are resized to a maximum edge of 2560 pixels; each processed image must be at most 8 MiB.

The phone uploads one acknowledged 192 KiB chunk at a time. Failed uploads retain the draft and resume from the last confirmed offset when retried. The Agent verifies the completed image's size, SHA-256, format, and dimensions, then submits its absolute path as a Codex `localImage` input. Images are preserved when a message is queued or sent with Steer.

Completed images and 512-pixel previews are stored under `%LOCALAPPDATA%\CodexRemote\images` on Windows, separate from project files. History includes attachment identifiers, so the phone can retrieve previews after reopening a session. Images remain on disk while that history references them. This version does not automatically delete completed images or provide full-resolution downloads to the phone. Images use the same transport as messages, including its existing plaintext limitation.

## Protocol

Each WebSocket frame is a JSON envelope:

```json
{
  "version": 1,
  "senderId": "phone-...",
  "targetId": "host-...",
  "sessionId": "...",
  "sequence": 12,
  "timestamp": 1790140800000,
  "nonce": "...",
  "kind": "command",
  "payload": "BASE64URL_OF_PLAINTEXT_JSON",
  "signature": "BASE64URL_ED25519_SIGNATURE"
}
```

The signature covers routing fields, timestamp, nonce, kind, and a SHA-256 digest of the encoded payload. Payload encoding is not encryption.

Supported phone commands include project/thread listing, thread start/resume, turn start/steer/interrupt, approval response, and event resynchronization.
