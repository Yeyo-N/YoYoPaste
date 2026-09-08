# YoYoPaste

> Copy on one device, paste on another — over your Tailscale tailnet.

[![CI](https://github.com/Yeyo-N/YoYoPaste/actions/workflows/ci.yml/badge.svg)](https://github.com/Yeyo-N/YoYoPaste/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/Yeyo-N/YoYoPaste)](https://github.com/Yeyo-N/YoYoPaste/releases)
[![Downloads](https://img.shields.io/github/downloads/Yeyo-N/YoYoPaste/total)](https://github.com/Yeyo-N/YoYoPaste/releases)

Clipboard sync for your tailnet. One static binary per device, plain HTTP over WireGuard, no central server.

<!-- demo gif -->

## Features

- Text and file clipboard sync between macOS, Windows, and iOS
- End-to-end encrypted via Tailscale WireGuard — data never leaves your tailnet
- Offline queue with auto-resume for large files
- Clipboard history with quick re-copy
- System tray + local web UI

## Install

### macOS

```bash
brew install --cask yoyopaste
```

### Windows

```powershell
winget install YeyoN.YoYoPaste
```

### From source

```bash
go install github.com/Yeyo-N/YoYoPaste/cmd/yoyopasted@latest
```

## How it works

```mermaid
sequenceDiagram
    participant CA as Clipboard (Mac)
    participant DA as yoyopasted (Mac)
    participant DB as yoyopasted (Win)
    participant CB as Clipboard (Win)

    CA->>DA: changeCount ticks (500ms poll)
    DA->>DA: hash, dedupe, store in SQLite
    DA->>DB: POST /v0/clip  (inline if <=4KiB)
    DB->>DB: store history entry
    alt payload not inline
        DB->>DA: GET /v0/blob/{id}  Range: bytes=N-
        DA-->>DB: 206 Partial Content (resumable)
    end
    opt auto-sync ON
        DB->>CB: set clipboard
    end
```

```mermaid
graph TD
    subgraph daemon["yoyopasted (Go, one static binary)"]
        TS[tailscale: peers + WhoIs]
        WATCH[clipboard watcher<br/>platform build tags]
        STORE[(SQLite + blob dir)]
        PEER[peer HTTP server :8383]
        UI[local UI server :8384]
        SYNC[sync engine + outbox]
    end
    TRAY[native tray icon] --> UI
    BROWSER[browser page] --> UI
    TS --> SYNC
    WATCH --> SYNC
    SYNC --> STORE
    SYNC --> PEER
    UI --> STORE
    PEER --> STORE
    IOS[iOS SwiftUI app<br/>share extension] -.protocol v0.-> PEER
```

## Security

Your data never leaves your tailnet. Encryption in transit is WireGuard (via Tailscale). Authentication is `WhoIs` on every peer request — only devices owned by the same tailnet user can sync. At rest, the SQLite file and blob directory are `0600` inside your OS app-support directory.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) and [docs/STYLE.md](docs/STYLE.md).
