package p2p

import (
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
)

// ParseConnectionString parses a multiaddr string and returns AddrInfo
func ParseConnectionString(connStr string) (*peer.AddrInfo, error) {
	maddr, err := multiaddr.NewMultiaddr(connStr)
	if err != nil {
		return nil, err
	}
	return peer.AddrInfoFromP2pAddr(maddr)
}

// SavePeer saves a peer's info for reconnection
func (n *Node) SavePeer(peerIDStr string, name string, addrStr string) {
	pid, err := peer.Decode(peerIDStr)
	if err != nil {
		return
	}
	info := peer.AddrInfo{ID: pid}
	if addrStr != "" {
		maddr, err := multiaddr.NewMultiaddr(addrStr)
		if err == nil {
			info.Addrs = []multiaddr.Multiaddr{maddr}
		}
	}
	n.mu.Lock()
	n.peers[pid] = &PeerState{
		Info:      info,
		Name:      name,
		Connected: n.Host.Network().Connectedness(pid) == 1,
	}
	n.mu.Unlock()
}

// RemovePeer removes a peer
func (n *Node) RemovePeer(peerIDStr string) {
	pid, err := peer.Decode(peerIDStr)
	if err != nil {
		return
	}
	n.mu.Lock()
	delete(n.peers, pid)
	n.mu.Unlock()
}
