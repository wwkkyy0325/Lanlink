# Network Enhancements — Transport Modes, Dedup, Bare TCP Relay

## Summary

Three-step improvement inspired by Pixez app's multi-mode network architecture. The goal: make Lanlink's dual-stack (LAN + P2P) transport more robust in Chinese network environments.

## Steps

- [x] **Step 1: Auto-fallback device dedup**
  - Add `P2PID` to `Device` and `DiscoveryPacket` models
  - Broadcast P2P PeerID via LAN discovery for cross-mode matching
  - `dedupDevices()` in `GetDevices()` merges duplicate LAN+P2P entries
  - LAN entries preferred (keeps direct IP for fast transfers)
  - Verify: ✅ Go compile, Wails bindings regenerated

- [x] **Step 2: Transport mode selector UI**
  - Add `TransportMode` to `AppSettings` ("auto" | "lan-only")
  - `SetTransportMode()` applies immediately (stops/starts P2P)
  - Radio group in Settings modal with i18n (zh + en)
  - Verify: ✅ Go compile, bindings include `SetTransportMode`

- [x] **Step 3: Bare TCP relay server**
  - New `cmd/relay/main.go` — standalone circuit relay v2 binary
  - Listens on raw TCP (no WSS/TLS, no SNI fingerprint)
  - Noise encryption only (TLS is redundant)
  - Users deploy on VPS, add multiaddr to custom relays
  - Verify: ✅ `go build ./cmd/relay` succeeds

- [x] **Bug fix: VPN adapter IP pollution**
  - `getLocalIP()` returned Radmin VPN IP (26.x) instead of LAN IP (192.168.x)
  - Replaced with `bestLANIP()` that filters virtual adapters and prefers private ranges
  - Same fix in `identity.go` for MAC matching
  - Verify: ✅ Manual test blocked (requires two devices on same LAN)

- [x] **Documentation**
  - README.md rewritten (features, architecture, setup, relay guide)
  - docs/llmwiki/design.md created (architecture, transport layers, key decisions)
  - docs/llmwiki/changelog.md created (append-only modification record)
  - Verify: ✅

## Files Changed

| File | Type | Lines |
|---|---|---|
| models/models.go | Modified | +2 fields |
| discovery/discovery.go | Modified | +110 (bestLANIP, SetP2PID, P2PID broadcast) |
| app.go | Modified | +80 (dedupDevices, transportMode) |
| identity.go | Modified | +90 (TransportMode, pickBestLANIP) |
| frontend/src/App.vue | Modified | +40 (transport mode UI + CSS) |
| frontend/src/i18n/zh.ts | Modified | +6 keys |
| frontend/src/i18n/en.ts | Modified | +6 keys |
| cmd/relay/main.go | **New** | 98 |
| README.md | Rewritten | 150 |
| docs/llmwiki/design.md | **New** | 180 |
| docs/llmwiki/changelog.md | **New** | 60 |

## Known Limitations

- `dedupDevices()` relies on P2PID being set before both entries exist; if a P2P device is discovered before the LAN device broadcasts its P2PID, they won't be correlated until the next broadcast cycle (up to 3s).
- Bare TCP relay uses libp2p's standard circuit relay v2 — peers behind symmetric NAT still need the relay. No custom relay protocol improvements yet.
- `bestLANIP()` adapter name filtering uses keyword matching; unusual adapter names on non-English Windows might not be caught.
