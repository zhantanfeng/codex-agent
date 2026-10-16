# Codex Remote

Codex Remote lets an Android phone control Codex running on a Windows computer. The Windows Agent owns the Codex process and project files. A small public Relay forwards signed WebSocket frames between the phone and the Agent.

> [!WARNING]
> This MVP intentionally uses plaintext `ws://` transport. Ed25519 signatures prevent command forgery, modification, and replay, but they do not hide prompts, source code, paths, or output. Do not use it with sensitive repositories. Do not expose the Relay as a long-term production service without adding TLS.

## Components

- `cmd/relay`: stateless Go WebSocket relay for a public Linux server.
- `cmd/agent`: Windows CLI/Agent that runs `codex app-server --stdio`.
- `android-app`: Android 12+ Compose client.
- `internal/protocol`: signed envelope and replay protection shared by the Go services.

The Agent requires Codex CLI `0.156.0` or newer and uses the stable app-server API. Its adapter was checked against the generated schemas for `0.156.x`, `0.161.x`, and `0.162.1`. Newer versions are allowed to initialize instead of being rejected by a release whitelist; incompatible initialization reports the CLI version and underlying error.

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

Install Codex CLI `0.156.0` or newer, sign in normally, and verify `codex --version` works in the same Windows account that will run the Agent.

```powershell
bin\codex-remote.exe init -relay ws://PUBLIC_IP:8080/ws
bin\codex-remote.exe project add -id my-project -path D:\projects\my-project
bin\codex-remote.exe run
```

With Relay, Agent, and Android `0.1.11` or newer, `run` automatically prints a six-digit connection code after connecting to the Relay. Open **dcode**, enter the code, and tap **连接电脑**. No separate `pair` command or Windows confirmation is required. The code expires after 10 minutes and is consumed by the first successfully paired phone. Restart `run` to generate a new code. Paired phones retain their authorization and reconnect without entering a code again.

The app defaults to `ws://43.155.135.230:8080/ws`. For another Relay, use **服务器设置 / 扫码连接** to set the address. APK builders can set the default with `-PdcodeRelayUrl=wss://YOUR_RELAY/ws`. The Relay allocates codes across online Agents, rejects expired/offline registrations, and limits code lookup attempts to five per IP per minute and 60 globally per minute.

To update an existing Relay, deploy the updated source and run `docker compose -f deploy/relay/docker-compose.yml up -d --build` from the repository root. The six-digit flow requires this Relay update; previously paired phones and QR pairing still work with the previous Relay.

A Linux amd64 prebuilt deployment package can also be used; see [prebuilt update instructions](deploy/relay/PREBUILT-README.md). It updates the existing Compose Relay on port 8080 and does not require a Go build on the server.

QR pairing is still available. Keep `run` active and start it in a second terminal:

```powershell
bin\codex-remote.exe pair
```

Scan the QR code in the Android app. The phone and terminal display a six-digit verification code. Enter the phone's code in the Windows terminal only when both values match. This confirmation is required because the bootstrap code travels over a plaintext network.

After verifying normal operation, register the Agent for startup after Windows login:

```powershell
bin\codex-remote.exe install
```

## Browse computer directories

With Agent and Android `0.1.13` or newer, tap **浏览电脑目录 / 新建会话** on the phone. Browse the computer's drives and folders, go up a level, or enter an absolute Windows path. Tap **在此目录新建会话** to create a session in that folder, including an empty folder. Only folders are listed, one level at a time, with pagination; no file contents are fetched. Access uses the Windows account running the Agent. Inaccessible, missing, or disconnected directories show an error and can be retried.

**Access change:** Every paired phone can browse directories accessible to that Windows account, beyond the previously configured projects. Browsing alone does not change the session allowlist. Explicitly selecting a folder to create a session adds only that exact directory to the saved project list, so its sessions remain available after restarting the Agent. Parents and other drives are not automatically authorized for sessions. Existing pairing, approvals, and read-only occupied sessions are unchanged. The Relay does not need an update for directory browsing. Directory names and paths still traverse the configured transport; use TLS (`wss://`) to keep them private in transit.

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
- A session owned by another Codex client opens with readable history and disabled input. Retry after closing it in the other client to acquire write access. Refreshing a read-only session reads the latest persisted history without interrupting the writer.
- The Agent buffers the latest 500 Codex events in memory. A reconnecting phone asks for events after its last event ID.
- The Relay rejects invalid signatures, timestamps outside the allowed window, and duplicate/out-of-order sequence numbers.
- Private keys are protected with Windows DPAPI and an Android Keystore AES key. Message bodies remain plaintext by design in this version.

## Image attachments

Update both the Android app and Windows Agent to `0.1.11`. Image transfers work with the existing Relay; the new six-digit pairing flow requires an updated Relay.

In a loaded conversation, use the image button beside the message field to choose JPEG or PNG pictures. A message can contain up to four images, with or without text. Selected images have previews and a remove button. Large images are resized to a maximum edge of 2560 pixels; each processed image must be at most 8 MiB.

The phone uploads one acknowledged 192 KiB chunk at a time. Failed uploads retain the draft and resume from the last confirmed offset when retried. The Agent verifies the completed image's size, SHA-256, format, and dimensions, then submits its absolute path as a Codex `localImage` input. Images are preserved when a message is queued or sent with Steer.

Completed images and 512-pixel previews are stored under `%LOCALAPPDATA%\CodexRemote\images` on Windows, separate from project files. History includes attachment identifiers, so the phone can retrieve previews after reopening a session. Images remain on disk while that history references them. This version does not automatically delete completed images or provide full-resolution downloads to the phone. Images use the same transport as messages, including its existing plaintext limitation.

Large history responses and events are split into signed 192 KiB chunks and reassembled by the phone, preserving the Relay's 2 MiB per-message limit. Transfers are checked against their total size and SHA-256 and are limited to 32 MiB per payload. The conversation shows loading progress and offers reload/reconnect actions on failure. Image selection is available while history loads; sending becomes available after the session loads successfully.

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
