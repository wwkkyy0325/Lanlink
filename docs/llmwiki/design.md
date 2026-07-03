# Lanlink Design

## Overview

Lanlink is a dual-stack desktop application for LAN file transfer & chat, built with Wails v2 (Go backend + Vue 3 / TypeScript frontend). It operates two independent transport layers that share a unified device list.

## Architecture

```
┌─────────────────────────────────────────────────────┐
│  Frontend (Vue 3 + TS + Vite)                        │
│  App.vue → DeviceList.vue, ChatPanel.vue             │
│  Wails IPC (auto-generated bindings)                 │
├─────────────────────────────────────────────────────┤
│  Backend (Go)                                        │
│                                                      │
│  ┌──────────────┐  ┌────────────┐  ┌─────────────┐  │
│  │ discovery     │  │ transfer   │  │ p2p          │  │
│  │ UDP:19999     │  │ HTTP:20000 │  │ libp2p:20001 │  │
│  │ broadcast     │  │ req/upload │  │ TCP+QUIC     │  │
│  │ 3s interval   │  │ /download  │  │ Noise+TLS    │  │
│  │ 10s stale     │  │ /message   │  │ relay v2     │  │
│  └──────┬───────┘  └─────┬──────┘  └──────┬──────┘  │
│         │                │                │          │
│         └────────────────┼────────────────┘          │
│                          │                           │
│              ┌───────────┴───────────┐               │
│              │   GetDevices()        │               │
│              │   unified list +      │               │
│              │   P2PID-based dedup   │               │
│              └───────────────────────┘               │
│                                                      │
│  Persistence: ~/.lanlink/*.json                      │
│  Identity: MAC-derived device ID + Ed25519 P2P key   │
└─────────────────────────────────────────────────────┘
```

## Transport Layers

### Layer 1: LAN (UDP discovery + HTTP transfer)

**Discovery** (`discovery/`): UDP broadcast on port 19999 every 3 seconds. Each device announces its ID (MAC-derived), name, IP, port, group memberships, and P2P PeerID (if P2P is running). Receivers capture the actual UDP source address (to fix cross-network IP mismatches). Devices stale for 10s are marked offline; 60s of silence = removed.

**Transfer** (`transfer/`): HTTP server on port 20000 with endpoints:
- `POST /request` — sender asks permission (15s timeout for user response)
- `POST /upload` — multipart file upload (2 GB limit)
- `POST /message` — JSON text message with 3-retry backoff
- `GET /download/{shareID}` — shared file download with Range support
- `GET /ping` — health check

The server fixes cross-network sender IPs by patching share messages with the TCP connection's actual remote address (`overrideShareIP`).

### Layer 2: P2P (libp2p)

**Node** (`p2p/node.go`): libp2p host on port 20001 with:
- **Transports**: TCP (`tcp.NewTCPTransport`) + WebSocket (`websocket.New`)
- **Security**: Noise protocol
- **Identity**: Ed25519 key persisted to `~/.lanlink/p2p_key`

**Protocols** (`p2p/protocol.go`):
- `/lanlink/message/1.0.0` — JSON-encoded text messages over libp2p streams
- `/lanlink/file/1.0.0` — JSON header (name, size) followed by raw file bytes, with accept/reject handshake

**NAT Traversal** (stack configured in `NewNode`):
1. **AutoNAT** — detects public address + NAT type (polled 5s interval, 60s duration)
2. **DCUtR hole punching** — upgrades relayed connections to direct
3. **Circuit Relay v2** — fallback relay routing
4. **AutoRelay** — auto-discovers relay candidates (3 public relays + user custom relays)
5. **UPnP** — async best-effort IGDv1/v2 port mapping (TCP+UDP)

**GFW countermeasures**:
- `resolveDoH()` — Google DoH resolution for relay domains, bypasses DNS pollution
- `diagnoseRelays()` — TCP reachability test to each relay on startup
- Custom relays with bare TCP support (`/ip4/x.x.x.x/tcp/PORT/p2p/...` — no WSS/TLS)
- Bare TCP relay server binary (`cmd/relay/`) for self-hosted deployment

## Device Identity & Dedup

### Device ID

Two identity systems coexist:
- **LAN ID**: derived from the primary NIC's MAC address (`getPrimaryMAC()`), stable across reinstalls
- **P2P PeerID**: derived from a persistent Ed25519 key (`loadOrCreateIdentity()`)

### Cross-Mode Dedup

When P2P starts, the PeerID is pushed to the discovery service via `SetP2PID()`. LAN broadcast packets carry this PeerID, so receivers can match LAN-discovered devices to their P2P counterparts.

`GetDevices()` runs `dedupDevices()` which merges entries by:
1. Same non-empty `P2PID` (LAN entry has PeerID from broadcast; P2P entry has it as ID)
2. Same IP address (fallback for devices that haven't set P2PID)

LAN entries are always preferred when merging (keeps the LAN IP for direct connections).

## Transport Mode

Defined in `AppSettings.TransportMode`:

| Mode | P2P Enabled | LAN Discovery | Description |
|---|---|---|---|
| `auto` (default) | Yes | Yes | LAN preferred, P2P for remote |
| `lan-only` | No | Yes | Local subnet only, zero internet |

Switching modes at runtime calls `StopP2P()` or `startP2PInBackground()` immediately.

## Settings Persistence

All settings stored as JSON in `~/.lanlink/`:

| File | Contents |
|---|---|
| `identity.json` | Device ID (MAC), display name |
| `settings.json` | Download dir, ask-save, custom relays, DoH, transport mode |
| `chat_data.json` | Message history (max 500), transfer history (max 200), groups |
| `paired.json` | Previously connected P2P peers for auto-reconnect |
| `known_devices.json` | All devices ever seen (online/offline tracking) |
| `manual_devices.json` | Manually added IP devices |
| `p2p_key` | Ed25519 private key (hex-encoded) |

## Frontend

Vue 3 Composition API with TypeScript. Two main components:

- **DeviceList** — sidebar showing local device, groups, online/offline devices, P2P controls
- **ChatPanel** — message display + input + file drag-and-drop

i18n: `zh.ts` (Chinese) and `en.ts` (English), toggleable at runtime.

Device sending routes by source tag:
- `source === 'p2p'` → `SendP2PMessage` / `SendP2PFile`
- `source === 'lan'` or `'manual'` → `SendMessage` / `SendFile` (HTTP to device IP)

## Key Decisions

1. **Why HTTP for LAN transfer instead of libp2p?** Simplicity. On a local subnet with no NAT, HTTP is zero-config and fast. libp2p adds overhead for a problem that doesn't exist on LAN.

2. **Why both MAC-derived ID and Ed25519 PeerID?** The MAC-based ID is stable and human-recognizable on LAN. The Ed25519 key is needed for libp2p's security model. Linking them via P2PID bridges the two worlds.

3. **Why Noise + optional TLS?** Noise is the mandatory libp2p encryption layer. WSS relays add TLS on top (for firewall friendliness), but the bare TCP relay proves TLS is optional — Noise alone is sufficient.

4. **UDP broadcast vs mDNS?** Custom UDP broadcast was chosen for simplicity and control. mDNS (via libp2p's zeroconf) would add another dependency; the custom protocol is 50 lines and does exactly what's needed.
