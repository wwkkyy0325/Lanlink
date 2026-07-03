// Bare TCP circuit relay v2 server for Lanlink.
//
// This runs a libp2p relay on raw TCP (no WebSocket, no TLS), so relay
// connections do not expose SNI or TLS fingerprints. Deploy this on a
// VPS and add its multiaddr to Lanlink's custom relay list.
//
// Build:
//
//	go build -o relay ./cmd/relay
//
// Run:
//
//	./relay -port 4001
//
// Then copy the printed multiaddr (e.g. /ip4/1.2.3.4/tcp/4001/p2p/12D3Koo...)
// into Lanlink Settings → P2P Relay Nodes.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/p2p/security/noise"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
)

func main() {
	port := flag.Int("port", 4001, "TCP port to listen on")
	keyPath := flag.String("key", "relay_key", "path to ed25519 private key file (hex-encoded, created if missing)")
	flag.Parse()

	privKey := loadOrCreateKey(*keyPath)

	peerID, err := peer.IDFromPrivateKey(privKey)
	if err != nil {
		log.Fatalf("Failed to derive PeerID: %v", err)
	}

	h, err := libp2p.New(
		libp2p.Identity(privKey),
		libp2p.ListenAddrStrings(fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", *port)),
		libp2p.Security(noise.ID, noise.New),
		libp2p.Transport(tcp.NewTCPTransport),
		// No WebSocket transport — bare TCP only, no TLS/SNI exposure.
		libp2p.ForceReachabilityPublic(),
		libp2p.EnableRelayService(), // circuit relay v2 server
		libp2p.DisableRelay(),       // we are a relay, not a client
	)
	if err != nil {
		log.Fatalf("Failed to create relay host: %v", err)
	}
	defer h.Close()

	log.Printf("=== Lanlink Bare TCP Relay ===")
	log.Printf("PeerID:   %s", peerID)
	log.Printf("Port:     %d", *port)
	log.Printf("")

	// Print addresses
	for _, addr := range h.Addrs() {
		full := fmt.Sprintf("%s/p2p/%s", addr, peerID)
		log.Printf("Multiaddr: %s", full)
	}
	log.Printf("")
	log.Printf("Copy the multiaddr above into Lanlink Settings → P2P Relay Nodes.")
	log.Printf("Example: /ip4/<YOUR_VPS_IP>/tcp/%d/p2p/%s", *port, peerID)
	log.Printf("")
	log.Printf("Relay is running. Press Ctrl+C to stop.")

	// Block until SIGINT/SIGTERM
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Printf("Shutting down.")
}

func loadOrCreateKey(path string) crypto.PrivKey {
	if data, err := os.ReadFile(path); err == nil {
		privBytes, err := hex.DecodeString(string(data))
		if err != nil || len(privBytes) != ed25519.PrivateKeySize {
			log.Fatalf("Corrupt key file at %s — delete it and restart", path)
		}
		priv := ed25519.PrivateKey(privBytes)
		k, err := crypto.UnmarshalEd25519PrivateKey(priv)
		if err != nil {
			log.Fatalf("Failed to unmarshal key: %v", err)
		}
		log.Printf("Loaded existing key from %s", path)
		return k
	}

	// Generate new key
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		log.Fatalf("Failed to generate key: %v", err)
	}
	if err := os.WriteFile(path, []byte(hex.EncodeToString(priv)), 0600); err != nil {
		log.Fatalf("Failed to save key: %v", err)
	}
	log.Printf("Generated new key at %s", path)
	return mustUnmarshal(priv)
}

func mustUnmarshal(priv ed25519.PrivateKey) crypto.PrivKey {
	k, err := crypto.UnmarshalEd25519PrivateKey(priv)
	if err != nil {
		log.Fatalf("Failed to unmarshal key: %v", err)
	}
	return k
}
