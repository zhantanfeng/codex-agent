# Relay 0.1.11 update package

This package contains a statically linked Linux amd64 Relay, its Docker image recipe, Compose configuration, and an update script. The image builds locally without downloading a Go toolchain or base image. It preserves port 8080 and the existing Compose project and service names.

Upload and extract `relay-0.1.11.zip` on the Linux amd64 server, then run from the extracted folder:

```sh
sh update-prebuilt.sh
curl -fsS http://127.0.0.1:8080/healthz
```

The script detects the running Compose service named `relay` that publishes port 8080 and updates that service in place. It stops if port 8080 belongs to another service or multiple containers. Other services and Docker images are preserved.

After the server update, restart the Windows Agent using the new `codex-remote.exe run`, then enter its six-digit connection code in dcode 0.1.11. Existing paired phones retain their authorization. A code expires after 10 minutes and is consumed after the first phone is paired.
