package models

import "time"

// Device represents a discovered device on the LAN or via P2P
type Device struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	IP       string    `json:"ip"`
	Port     int       `json:"port"`
	LastSeen time.Time `json:"lastSeen"`
	Online   bool      `json:"online"`
	Source   string    `json:"source"` // "lan" or "p2p"
	Groups   []string  `json:"groups"` // group IDs this device belongs to
}

// DiscoveryPacket is broadcast via UDP for device discovery
type DiscoveryPacket struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	IP       string   `json:"ip"`
	Port     int      `json:"port"`
	GroupIDs []string `json:"groupIds,omitempty"` // groups this device is in
}

// TransferRecord stores history of file transfers
type TransferRecord struct {
	ID         string    `json:"id"`
	DeviceID   string    `json:"deviceId"`
	DeviceName string    `json:"deviceName"`
	FileName   string    `json:"fileName"`
	FileSize   int64     `json:"fileSize"`
	Direction  string    `json:"direction"` // "sent" or "received"
	Status     string    `json:"status"`    // "pending", "transferring", "completed", "failed"
	Time       time.Time `json:"time"`
}

// Message represents a chat message between devices
type Message struct {
	ID         string    `json:"id"`
	DeviceID   string    `json:"deviceId"`
	DeviceName string    `json:"deviceName"`
	Content    string    `json:"content"`
	Time       time.Time `json:"time"`
	Direction  string    `json:"direction"` // "sent" or "received"
}

// FileMeta is sent before the actual file transfer
type FileMeta struct {
	FileName string `json:"fileName"`
	FileSize int64  `json:"fileSize"`
	SenderID string `json:"senderId"`
	SenderIP string `json:"senderIP"`
	Type     string `json:"type"` // "file" or "message"
}

// P2PPeerInfo holds a saved P2P peer's info for reconnection
type P2PPeerInfo struct {
	PeerID    string `json:"peerId"`
	Name      string `json:"name"`
	Multiaddr string `json:"multiaddr"`
}

// Group represents a device group / room
type Group struct {
	ID        string    `json:"id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Members   []string  `json:"members"`
	Encrypted bool      `json:"encrypted"`
	Key       string    `json:"key"` // AES-256 key (never broadcast, only in local DB + invite string)
	Created   time.Time `json:"created"`
}

// GroupMember pairs a device with its group membership context
type GroupMember struct {
	DeviceID   string `json:"deviceId"`
	DeviceName string `json:"deviceName"`
	IP         string `json:"ip"`
	Online     bool   `json:"online"`
}

// TransferRequest is sent before the actual file to ask for confirmation
type TransferRequest struct {
	ID         string `json:"id"`
	SenderID   string `json:"senderId"`
	SenderName string `json:"senderName"`
	SenderIP   string `json:"senderIP"`
	FileName   string `json:"fileName"`
	FileSize   int64  `json:"fileSize"`
	GroupID    string `json:"groupId,omitempty"`
}

// TransferResponse is sent back to accept or reject
type TransferResponse struct {
	RequestID string `json:"requestId"`
	Accepted  bool   `json:"accepted"`
}

// SharedFile represents a file shared for download
type SharedFile struct {
	ID        string `json:"id"`
	ShareID   string `json:"shareId"`
	FileName  string `json:"fileName"`
	FilePath  string `json:"-"` // local path, never serialized
	FileSize  int64  `json:"fileSize"`
	SenderID  string `json:"senderId"`
	SenderIP  string `json:"senderIP"`
	SenderName string `json:"senderName"`
}
