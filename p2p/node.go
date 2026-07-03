package p2p

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"lanlink/models"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/host/autorelay"
	"github.com/libp2p/go-libp2p/p2p/protocol/holepunch"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	"github.com/libp2p/go-libp2p/p2p/transport/websocket"

	ma "github.com/multiformats/go-multiaddr"
)

// Known public relay nodes (must NOT contain /p2p-circuit — that's appended per-target)
var publicRelays = []string{
	"/dns4/relay.libp2p.io/tcp/443/wss/p2p/12D3KooWEgQe6j6MYCy2ZE5P8NnYPjWuDrgMfmYKyGgBtPK4aPku",
	"/dns4/ams-1.relay.libp2p.io/tcp/443/wss/p2p/12D3KooWDQXRjCBv4oQe4FSqtWF4HjSkJ4kDP6R9kG5w6EmqKwUt",
	"/dns4/relay.devp2p.io/tcp/443/wss/p2p/12D3KooWNmzYgXmb4rRQVqJvrV72BsKcXBq1Q3xfqJs6Nnf5GJWu",
}

// Node wraps a libp2p host with Lanlink-specific functionality
type Node struct {
	ctx    context.Context
	cancel context.CancelFunc

	Host host.Host
	ID   peer.ID

	mu    sync.RWMutex
	peers map[peer.ID]*PeerState
	ps    *nodePubSub

	// NAT info (populated by AutoNAT)
	natInfo NatInfo

	onPeerConnected    func(peer.AddrInfo)
	onPeerDisconnected func(peer.ID)
	onMessage          func(models.Message)
	onFileReceived     func(models.TransferRecord)

	keyPath string
}

// PeerState tracks a P2P peer
type PeerState struct {
	Info      peer.AddrInfo
	Name      string
	Connected bool
}

// NatInfo holds NAT traversal status
type NatInfo struct {
	PublicAddr   string `json:"publicAddr"`
	NATType      string `json:"natType"`      // "unknown", "public", "private"
	Reachability string `json:"reachability"` // how others can reach us
	HasRelay     bool   `json:"hasRelay"`
	RelayAddrs   int    `json:"relayAddrs"`
}

// NewNode creates a libp2p Node with full NAT traversal stack:
//   - TCP + QUIC transports
//   - Noise encryption
//   - AutoNAT (detect public address + NAT type)
//   - DCUtR hole punching
//   - Circuit relay v2 (for fallback)
//   - AutoRelay (auto-discover + use relay nodes)
func NewNode(listenPort int, keyPath string) (*Node, error) {
	ctx, cancel := context.WithCancel(context.Background())

	privKey, err := loadOrCreateIdentity(keyPath)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("identity: %w", err)
	}

	peerID, err := peer.IDFromPrivateKey(privKey)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("peer ID: %w", err)
	}

	// Resolve relay addresses
	var relayInfos []peer.AddrInfo
	for _, rs := range publicRelays {
		maddr, err := ma.NewMultiaddr(rs)
		if err != nil {
			continue
		}
		info, err := peer.AddrInfoFromP2pAddr(maddr)
		if err != nil {
			continue
		}
		relayInfos = append(relayInfos, *info)
	}

	listenAddrs := []string{
		fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", listenPort),
		fmt.Sprintf("/ip4/0.0.0.0/udp/%d/quic-v1", listenPort),
	}

	h, err := libp2p.New(
		libp2p.Identity(privKey),
		libp2p.ListenAddrStrings(listenAddrs...),
		libp2p.Security(noise.ID, noise.New),
		libp2p.Transport(tcp.NewTCPTransport),
		libp2p.Transport(websocket.New),
		// NAT traversal stack
		libp2p.EnableNATService(),           // AutoNAT: detect public addr + NAT type
		libp2p.EnableHolePunching(),         // DCUtR: direct connection upgrade
		libp2p.ForceReachabilityPrivate(),   // assume behind NAT until AutoNAT confirms
		libp2p.EnableRelay(),                // circuit relay client
		// AutoRelay: use public relays when direct connection fails
		libp2p.EnableAutoRelayWithStaticRelays(
			relayInfos,
			autorelay.WithMaxCandidates(3),
			autorelay.WithMinInterval(30*time.Second),
		),
		// Circuit relay transport for fallback connections
		libp2p.EnableRelayService(),
	)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create host: %w", err)
	}

	node := &Node{
		ctx:     ctx,
		cancel:  cancel,
		Host:    h,
		ID:      peerID,
		peers:   make(map[peer.ID]*PeerState),
		keyPath: keyPath,
		natInfo: NatInfo{NATType: "detecting..."},
	}

	log.Printf("[P2P] Node started. PeerID: %s", peerID)
	log.Printf("[P2P] Listen addrs: %v", listenAddrs)
	log.Printf("[P2P] Configured %d public relays", len(relayInfos))

	// Track connections
	h.Network().Notify(&network.NotifyBundle{
		ConnectedF: func(n network.Network, c network.Conn) {
			pid := c.RemotePeer()
			node.mu.Lock()
			if ps, ok := node.peers[pid]; ok {
				ps.Connected = true
			}
			node.mu.Unlock()
			if node.onPeerConnected != nil {
				node.onPeerConnected(node.peerAddrInfo(pid))
			}
		},
		DisconnectedF: func(n network.Network, c network.Conn) {
			pid := c.RemotePeer()
			node.mu.Lock()
			if ps, ok := node.peers[pid]; ok {
				ps.Connected = false
			}
			node.mu.Unlock()
			if node.onPeerDisconnected != nil {
				node.onPeerDisconnected(c.RemotePeer())
			}
		},
	})

	// Monitor NAT status changes
	go node.monitorNAT()

	// Diagnose relay reachability (helps identify network/firewall issues)
	go node.diagnoseRelays()

	// Listen for hole punch events
	go node.monitorHolePunch()

	return node, nil
}

// diagnoseRelays tests TCP reachability to each public relay host:443.
// This identifies whether failures are due to network/firewall blocking
// (common in mainland China) vs. libp2p configuration issues.
func (n *Node) diagnoseRelays() {
	time.Sleep(3 * time.Second)
	hosts := []string{"relay.libp2p.io:443", "ams-1.relay.libp2p.io:443", "relay.devp2p.io:443"}
	for _, h := range hosts {
		go func(addr string) {
			conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
			if err != nil {
				log.Printf("[P2P] ✗ Cannot reach %s: %v", addr, err)
			} else {
				log.Printf("[P2P] ✓ Reachable: %s (TCP connected)", addr)
				conn.Close()
			}
		}(h)
	}
}

// monitorNAT watches for AutoNAT status changes
func (n *Node) monitorNAT() {
	// Poll reachability every 5 seconds for the first minute
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	deadline := time.After(60 * time.Second)

	for {
		select {
		case <-n.ctx.Done():
			return
		case <-deadline:
			return
		case <-ticker.C:
			n.refreshNatInfo()
		}
	}
}

func (n *Node) monitorHolePunch() {
	// Subscribe to hole punch service events if available
	// The holepunch service emits events through the host event bus
	sub, err := n.Host.EventBus().Subscribe(new(holepunch.Event))
	if err != nil {
		return
	}
	defer sub.Close()

	for {
		select {
		case <-n.ctx.Done():
			return
		case evt := <-sub.Out():
			switch e := evt.(type) {
			case holepunch.Event:
				_ = e
				n.refreshNatInfo()
			}
		}
	}
}

func (n *Node) refreshNatInfo() {
	addrs := n.Host.Addrs()
	n.natInfo.RelayAddrs = 0

	var publicAddr string
	hasPublic := false
	hasRelay := false

	for _, addr := range addrs {
		addrStr := addr.String()
		// Check for relay addresses
		if contains(addrStr, "p2p-circuit") {
			hasRelay = true
			n.natInfo.RelayAddrs++
			continue
		}
		// Check for public addresses
		if !contains(addrStr, "192.168.") &&
			!contains(addrStr, "10.") &&
			!contains(addrStr, "172.16.") &&
			!contains(addrStr, "127.0.0.1") {
			hasPublic = true
			publicAddr = addrStr
		}
	}

	n.natInfo.PublicAddr = publicAddr
	n.natInfo.HasRelay = hasRelay

	if hasPublic {
		n.natInfo.Reachability = "public"
		n.natInfo.NATType = "open"
	} else if hasRelay {
		n.natInfo.Reachability = "relay"
		n.natInfo.NATType = "restricted (relay available)"
	} else {
		n.natInfo.Reachability = "private"
		n.natInfo.NATType = "restricted (no relay)"
	}
	log.Printf("[P2P] NAT: reachability=%s, publicAddr=%s, relays=%d", n.natInfo.Reachability, publicAddr, n.natInfo.RelayAddrs)
}

// GetNatInfo returns current NAT status
func (n *Node) GetNatInfo() NatInfo {
	return n.natInfo
}

// Close shuts down the libp2p node
func (n *Node) Close() error {
	n.cancel()
	return n.Host.Close()
}

// PeerIDShort returns shortened PeerID
func (n *Node) PeerIDShort() string {
	s := n.ID.String()
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

// FullPeerID returns the full PeerID
func (n *Node) FullPeerID() string {
	return n.ID.String()
}

// AllMultiaddrs returns all addresses (direct + relay) for connection sharing
func (n *Node) AllMultiaddrs() []string {
	addrs := n.Host.Addrs()
	var result []string
	for _, addr := range addrs {
		result = append(result, fmt.Sprintf("%s/p2p/%s", addr.String(), n.ID))
	}
	return result
}

// BuildConnectionString creates the best connection string for sharing
func (n *Node) BuildConnectionString(externalIP string, externalPort int) string {
	// Prefer direct connection if we have public address or UPnP
	if externalIP != "" {
		return fmt.Sprintf("/ip4/%s/tcp/%d/p2p/%s", externalIP, externalPort, n.ID)
	}
	// Include relay options
	addrs := n.AllMultiaddrs()
	for _, a := range addrs {
		// Skip loopback
		if !contains(a, "127.0.0.1") && !contains(a, "192.168.") {
			return a
		}
	}
	// Fallback to any address
	if len(addrs) > 0 {
		return addrs[0]
	}
	return fmt.Sprintf("/ip4/127.0.0.1/tcp/%d/p2p/%s", externalPort, n.ID)
}

// BuildConnectionStringLocal builds a LAN-local connection string
func (n *Node) BuildConnectionStringLocal(localPort int) string {
	// Return all LAN-accessible addresses
	addrs := n.Host.Addrs()
	for _, a := range addrs {
		addrStr := a.String()
		if !contains(addrStr, "p2p-circuit") && !contains(addrStr, "127.0.0.1") {
			return fmt.Sprintf("%s/p2p/%s", addrStr, n.ID)
		}
	}
	return fmt.Sprintf("/ip4/127.0.0.1/tcp/%d/p2p/%s", localPort, n.ID)
}

// SetCallbacks registers P2P event callbacks
func (n *Node) SetCallbacks(
	onPeerConnected func(peer.AddrInfo),
	onPeerDisconnected func(peer.ID),
	onMessage func(models.Message),
	onFileReceived func(models.TransferRecord),
) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.onPeerConnected = onPeerConnected
	n.onPeerDisconnected = onPeerDisconnected
	n.onMessage = onMessage
	n.onFileReceived = onFileReceived
}

// GetPeers returns known P2P peers as Device structs
func (n *Node) GetPeers() []models.Device {
	n.mu.RLock()
	defer n.mu.RUnlock()
	devices := make([]models.Device, 0, len(n.peers))
	for id, ps := range n.peers {
		addr := ""
		if len(ps.Info.Addrs) > 0 {
			addr = ps.Info.Addrs[0].String()
		}
		name := ps.Name
		if name == "" {
			name = id.String()[:12]
		}
		devices = append(devices, models.Device{
			ID:     id.String(),
			Name:   name,
			IP:     addr,
			Online: ps.Connected,
			Source: "p2p",
		})
	}
	return devices
}

// ConnectByAddr connects to a peer by multiaddr string
// ConnectByAddr connects to a peer. Accepts either:
//   - a full multiaddr: /ip4/x.x.x.x/tcp/20001/p2p/12D3Koo...
//   - a bare PeerID: 12D3Koo...  (will try connecting via known public relays)
func (n *Node) ConnectByAddr(addrStr string) error {
	addrStr = strings.TrimSpace(addrStr)

	// Bare PeerID case: no leading "/" → connect via relays
	if !strings.HasPrefix(addrStr, "/") {
		pid, err := peer.Decode(addrStr)
		if err != nil {
			return fmt.Errorf("invalid peer ID or multiaddr: %w", err)
		}
		if pid == n.ID {
			return fmt.Errorf("cannot connect to self")
		}
		// Try relays synchronously so caller knows outcome
		n.ReconnectViaRelays(addrStr, "")
		// Check if it actually connected
		if n.Host.Network().Connectedness(pid) != network.Connected {
			return fmt.Errorf("could not reach peer via any relay (peer may be offline or behind symmetric NAT)")
		}
		return nil
	}

	// Full multiaddr case
	maddr, err := ma.NewMultiaddr(addrStr)
	if err != nil {
		return fmt.Errorf("invalid multiaddr: %w", err)
	}
	info, err := peer.AddrInfoFromP2pAddr(maddr)
	if err != nil {
		return fmt.Errorf("parse peer info: %w", err)
	}
	if info.ID == n.ID {
		return fmt.Errorf("cannot connect to self")
	}

	// libp2p will try: direct → hole punch → relay (auto)
	if err := n.Host.Connect(n.ctx, *info); err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	n.mu.Lock()
	n.peers[info.ID] = &PeerState{Info: *info, Connected: true}
	n.mu.Unlock()

	return nil
}

// ReconnectViaRelays tries to reach a peer (by PeerID) through each known public relay.
// libp2p will then attempt to upgrade the relayed connection to a direct/hole-punched one.
func (n *Node) ReconnectViaRelays(peerIDStr string, name string) {
	pid, err := peer.Decode(peerIDStr)
	if err != nil {
		return
	}

	// Already connected?
	if n.Host.Network().Connectedness(pid) == network.Connected {
		n.mu.Lock()
		if ps, ok := n.peers[pid]; ok {
			ps.Connected = true
			if name != "" {
				ps.Name = name
			}
		}
		n.mu.Unlock()
		return
	}

	// Try each public relay sequentially: /<relay>/p2p/<relay-id>/p2p-circuit/p2p/<target>
	for _, relayAddr := range publicRelays {
		// Normalize: strip any existing /p2p-circuit suffix (defensive)
		clean := strings.Split(relayAddr, "/p2p-circuit")[0]
		circuitStr := clean + "/p2p-circuit/p2p/" + peerIDStr

		maddr, err := ma.NewMultiaddr(circuitStr)
		if err != nil {
			continue
		}
		info, err := peer.AddrInfoFromP2pAddr(maddr)
		if err != nil {
			continue
		}

		connectCtx, cancel := context.WithTimeout(n.ctx, 20*time.Second)
		err = n.Host.Connect(connectCtx, *info)
		cancel()

		if err == nil {
			log.Printf("[P2P] ✓ Connected to %s via relay %s", peerIDStr[:12], strings.Split(relayAddr, "/p2p/")[0])
			n.mu.Lock()
			n.peers[pid] = &PeerState{Info: *info, Name: name, Connected: true}
			cb := n.onPeerConnected
			n.mu.Unlock()
			if cb != nil {
				cb(*info)
			}
			return
		}
		log.Printf("[P2P] ✗ relay %s failed: %v", strings.Split(relayAddr, "/p2p/")[0], err)
	}
	log.Printf("[P2P] ✗ All %d relays failed for %s", len(publicRelays), peerIDStr[:12])
	// All relays failed
	n.mu.Lock()
	if ps, ok := n.peers[pid]; ok {
		ps.Connected = false
	}
	n.mu.Unlock()
}

func (n *Node) peerAddrInfo(pid peer.ID) peer.AddrInfo {
	n.mu.RLock()
	defer n.mu.RUnlock()
	if ps, ok := n.peers[pid]; ok {
		return ps.Info
	}
	return peer.AddrInfo{ID: pid}
}

// ---- Identity ----

func loadOrCreateIdentity(keyPath string) (crypto.PrivKey, error) {
	dir := filepath.Dir(keyPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	if data, err := os.ReadFile(keyPath); err == nil {
		privBytes, err := hex.DecodeString(string(data))
		if err != nil || len(privBytes) != ed25519.PrivateKeySize {
			return nil, fmt.Errorf("corrupt key file")
		}
		prived := ed25519.PrivateKey(privBytes)
		libp2pPriv, err := crypto.UnmarshalEd25519PrivateKey(prived)
		if err != nil {
			return nil, fmt.Errorf("failed to load key: %w", err)
		}
		return libp2pPriv, nil
	}
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(keyPath, []byte(hex.EncodeToString(priv)), 0600); err != nil {
		return nil, fmt.Errorf("save key: %w", err)
	}
	return crypto.UnmarshalEd25519PrivateKey(priv)
}

func (n *Node) getHost() host.Host { return n.Host }

func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchString(s, sub)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
