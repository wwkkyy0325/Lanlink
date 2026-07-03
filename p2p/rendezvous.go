package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"lanlink/models"

	"github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
)

// Rendezvous announcement published to a pairing-code topic
type announcement struct {
	PeerID string `json:"peerId"`
	Name   string `json:"name"`
}

// pubsub state attached to Node
type nodePubSub struct {
	ps     *pubsub.PubSub
	topics map[string]*pubsub.Topic
	subs   map[string]context.CancelFunc
}

// initPubSub sets up GossipSub on the host
func (n *Node) initPubSub() error {
	ps, err := pubsub.NewGossipSub(n.ctx, n.Host,
		pubsub.WithPeerExchange(true),
	)
	if err != nil {
		return err
	}
	n.ps = &nodePubSub{
		ps:     ps,
		topics: make(map[string]*pubsub.Topic),
		subs:   make(map[string]context.CancelFunc),
	}
	return nil
}

// JoinPairingRoom joins a pairing-code room. Announcements from other peers
// in the same room trigger automatic connection.
func (n *Node) JoinPairingRoom(code string, myName string, onPeer func(peer.AddrInfo, string)) error {
	if n.ps == nil {
		if err := n.initPubSub(); err != nil {
			return err
		}
	}

	topicName := "lanlink-pair-" + code
	log.Printf("[P2P] Joining pairing room: %s (my name: %s)", topicName, myName)

	// Leave existing room if any
	n.leaveAllRooms()

	topic, err := n.ps.ps.Join(topicName)
	if err != nil {
		return fmt.Errorf("join room: %w", err)
	}
	n.ps.topics[topicName] = topic
	log.Printf("[P2P] ✓ Joined room %s", topicName)

	sub, err := topic.Subscribe()
	if err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}

	ctx, cancel := context.WithCancel(n.ctx)
	n.ps.subs[topicName] = cancel

	// Announce ourselves repeatedly until we find a peer or timeout
	self := announcement{PeerID: n.ID.String(), Name: myName}
	data, _ := json.Marshal(self)

	// Read loop: connect to anyone who announces
	go func() {
		for {
			msg, err := sub.Next(ctx)
			if err != nil {
				return
			}
			if msg.GetFrom() == n.ID {
				continue
			}
			var ann announcement
			if json.Unmarshal(msg.GetData(), &ann) != nil {
				continue
			}
			log.Printf("[P2P] Discovered peer via pairing room: %s (%s)", ann.Name, msg.GetFrom())
			// Connect to the announcing peer
			info := peer.AddrInfo{ID: msg.GetFrom()}
			n.mu.Lock()
			n.peers[info.ID] = &PeerState{Info: info, Name: ann.Name, Connected: false}
			n.mu.Unlock()
			if onPeer != nil {
				onPeer(info, ann.Name)
			}
		}
	}()

	// Announce ourselves every 2 seconds for up to 30 seconds
	go func() {
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		deadline := time.After(30 * time.Second)
		// immediate first announce
		topic.Publish(ctx, data)
		for {
			select {
			case <-ctx.Done():
				return
			case <-deadline:
				return
			case <-ticker.C:
				topic.Publish(ctx, data)
			}
		}
	}()

	return nil
}

func (n *Node) leaveAllRooms() {
	if n.ps == nil {
		return
	}
	for name, cancel := range n.ps.subs {
		cancel()
		delete(n.ps.subs, name)
		if t, ok := n.ps.topics[name]; ok {
			t.Close()
			delete(n.ps.topics, name)
		}
	}
}

// PairingAnnounce is invoked from frontend: join room, return own info
func (n *Node) PairingAnnounce(code string, myName string) (*models.P2PPeerInfo, error) {
	if err := n.JoinPairingRoom(code, myName, nil); err != nil {
		return nil, err
	}
	return &models.P2PPeerInfo{
		PeerID: n.ID.String(),
		Name:   myName,
	}, nil
}
