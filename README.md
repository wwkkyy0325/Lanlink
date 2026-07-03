# Lanlink

LAN file transfer & chat desktop app — discover devices on your local network, send files and messages instantly. P2P relay mode extends connectivity across the internet, with GFW-resistant bare TCP relay support.

## Features

- **Zero-config LAN discovery** — devices on the same subnet appear automatically via UDP broadcast
- **File transfer** — send single or multiple files with accept/decline handshake; share for download with HTTP Range resume
- **Text chat** — direct and group messaging with AES-256 encrypted groups
- **P2P internet mode** — libp2p with AutoNAT, DCUtR hole punching, and circuit relay v2 for NAT traversal
- **GFW-resistant relays** — DNS-over-HTTPS resolver, custom relay nodes, and bare TCP relay (no TLS/SNI fingerprint)
- **Transport mode selector** — Auto (LAN-first, relay fallback) or LAN-only for privacy
- **Radmin VPN / manual IP** — manually add devices by IP for virtual LAN setups
- **Persistent identity** — MAC-derived device ID + Ed25519 P2P key, survives reinstalls
- **Cross-platform** — Windows desktop (macOS/Linux via Wails)

## Tech Stack

| Layer | Technology |
|---|---|
| Desktop shell | [Wails v2](https://wails.io/) (Go + WebView2) |
| Frontend | Vue 3 + TypeScript + Vite |
| LAN discovery | UDP broadcast (port 19999) |
| LAN transfer | HTTP (port 20000) — request/upload/download/share |
| P2P networking | [libp2p](https://libp2p.io/) (go-libp2p v0.36) |
| P2P security | Noise protocol + Ed25519 identity |
| NAT traversal | AutoNAT · DCUtR hole punch · Circuit Relay v2 · UPnP |
| Encryption | AES-256-GCM (group chat) |

## Quick Start

### Prerequisites

- [Go](https://go.dev/dl/) 1.23+
- [Node.js](https://nodejs.org/) 18+
- [Wails CLI](https://wails.io/docs/gettingstarted/installation): `go install github.com/wailsapp/wails/v2/cmd/wails@latest`

### Development

```bash
wails dev
```

This starts the Vite dev server (hot reload for frontend) and the Go backend. Open `http://localhost:34115` in a browser for devtools access to Go bindings.

### Production Build

```bash
wails build
```

The packaged executable lands in `build/bin/`.

## Transport Modes

Lanlink supports two transport modes, switchable in Settings at runtime:

| Mode | Behavior |
|---|---|
| **Auto** (default) | LAN broadcast + P2P auto-start. LAN direct preferred when a device is reachable via both paths. |
| **LAN Only** | P2P completely disabled. Only local subnet devices appear. No internet connectivity needed. |

## P2P & GFW Bypass

The P2P layer uses public libp2p relays over WSS (`relay.libp2p.io`). In restricted network environments, two mechanisms help:

### DoH Resolution

Enable **DoH Resolution** in Settings. Relay domain names are resolved through Google DNS-over-HTTPS instead of the system resolver, bypassing DNS pollution.

### Custom Relays (including Bare TCP)

Add your own relay multiaddrs in Settings → P2P Relay Nodes. One per line:

```
/ip4/1.2.3.4/tcp/4001/p2p/12D3Koo...
/dns4/my-relay.example.com/tcp/443/wss/p2p/12D3Koo...
```

For environments where TLS/SNI fingerprinting is a concern, deploy the **bare TCP relay** (see below).

## Bare TCP Relay Server

Unlike the default WSS relays, the bare TCP relay runs on raw TCP — no TLS layer, no SNI exposure, no WebSocket upgrade fingerprint. Deploy on any VPS with a public IP.

### Build

```bash
go build -o relay ./cmd/relay
```

### Run

```bash
./relay -port 4001
```

Output:

```
=== Lanlink Bare TCP Relay ===
PeerID:   12D3KooW...
Port:     4001
Multiaddr: /ip4/<VPS_IP>/tcp/4001/p2p/12D3KooW...
```

Copy the printed multiaddr into Lanlink Settings → P2P Relay Nodes.

### How It Works

```
Default WSS relay:
  [Noise data] → WebSocket → TLS (SNI: relay.libp2p.io) → TCP → relay

Bare TCP relay:
  [Noise data] → TCP → relay
                  ↑
          No TLS, no SNI, no cert needed
```

The Noise protocol already provides transport encryption — TLS on top is redundant and adds identifiable handshake patterns. The bare TCP relay strips that layer away.

### Deployment Tips

- Works on any VPS with a public IP (no domain, no certificate needed)
- Non-standard ports (e.g. 4001) avoid port-80/443 filtering
- For mainland China users, deploy on an offshore VPS with good connectivity

## Project Structure

```
Lanlink/
├── app.go                  # Main App struct, Wails bindings
├── identity.go             # Device identity, settings persistence
├── discovery/
│   └── discovery.go        # UDP broadcast discovery service
├── transfer/
│   ├── server.go           # HTTP server (upload/download/share/message)
│   └── sender.go           # HTTP client (send files/messages)
├── p2p/
│   ├── node.go             # libp2p host, relay, NAT traversal
│   ├── protocol.go         # Message & file stream handlers
│   ├── rendezvous.go       # GossipSub pairing rooms
│   └── upnp.go             # UPnP IGD port mapping
├── models/
│   └── models.go           # Shared data types
├── cmd/relay/
│   └── main.go             # Bare TCP relay server binary
├── frontend/
│   └── src/
│       ├── App.vue          # Main UI
│       ├── components/      # DeviceList, ChatPanel
│       └── i18n/            # zh.ts, en.ts
└── wails.json              # Wails project config
```

## License

MIT
