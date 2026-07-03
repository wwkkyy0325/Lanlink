# Changelog

## 2026-07-03 — Transport modes, device dedup, bare TCP relay, LAN IP fix

### Added

- **Transport mode selector** in Settings UI: Auto (LAN-first, relay fallback) vs LAN-only (P2P disabled). Modes apply immediately on switch and persist across restarts. (`identity.go`, `app.go`, `App.vue`)

- **Cross-mode device dedup** — when the same physical machine is reachable via both LAN broadcast and P2P relay, `GetDevices()` merges them into a single entry. LAN devices now broadcast their P2P PeerID via `DiscoveryPacket.P2PID` so receivers can correlate entries. LAN entries are preferred (keeps the direct LAN IP). (`models/models.go`, `discovery/discovery.go`, `app.go`)

- **Bare TCP relay server** (`cmd/relay/main.go`) — standalone libp2p circuit relay v2 binary that listens on raw TCP (no WebSocket, no TLS, no SNI). Users deploy this on a VPS and add its multiaddr as a custom relay in Lanlink settings. The Noise protocol provides encryption; TLS is unnecessary overhead that creates identifiable handshake patterns. Build with `go build -o relay ./cmd/relay`.

### Fixed

- **LAN IP detection with VPN adapters** — `getLocalIP()` (discovery) and `getLocalIPForMAC()` (identity) previously used `net.Dial("udp", "8.8.8.8:80")`, which can route through a VPN virtual adapter (Radmin, WireGuard, etc.) and return an unreachable virtual IP. Replaced with `bestLANIP()` / `pickBestLANIP()` that iterates all interfaces, filters out known virtual/VPN adapter name patterns, and prefers private LAN ranges (192.168.x.x > 10.x.x.x > 172.16-31.x.x). Falls back to the old Dial method if no LAN IP is found. (`discovery/discovery.go`, `identity.go`)

### Changed

- Discovery service now exposes `SetP2PID()` to accept the libp2p PeerID after P2P starts. This PeerID is included in every broadcast packet. (`discovery/discovery.go`)

- `AppSettings` gains `TransportMode` field; load/save/GetSettings all updated. `NewApp()` defaults to `"auto"`. (`identity.go`, `app.go`)

- `GoOnline()` and `startup()` respect transport mode — P2P only starts in `"auto"` mode. (`app.go`)

- README rewritten from default Wails template to full project documentation: features, tech stack, transport modes, GFW bypass, bare TCP relay setup, project structure. (`README.md`)

- Project knowledge base created: `docs/llmwiki/design.md` (architecture, transport layers, identity model, key decisions), `docs/llmwiki/changelog.md` (this file). (`docs/llmwiki/`)

### Models

- `Device` struct: added `P2PID string` field for cross-mode dedup.
- `DiscoveryPacket` struct: added `P2PID string` field.
- `AppSettings` struct: added `TransportMode string` field.

---

## 2026-07-02 — Initial commit (pre-changelog)

- LAN device discovery via UDP broadcast
- HTTP file transfer with request/accept workflow
- Text chat (direct + encrypted groups)
- libp2p P2P with AutoNAT, DCUtR, circuit relay v2, UPnP
- DoH relay domain resolution for GFW bypass
- Custom relay node configuration
- Radmin VPN / manual IP device entry
- Paired peer persistence and auto-reconnect
- Chinese + English i18n
