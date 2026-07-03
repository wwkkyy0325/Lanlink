package p2p

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"lanlink/models"

	"github.com/google/uuid"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
)

const (
	ProtocolMessage = "/lanlink/message/1.0.0"
	ProtocolFile    = "/lanlink/file/1.0.0"
)

// wireMessage is the JSON message sent over the wire
type wireMessage struct {
	Type       string `json:"type"` // "text", "file-header", "file-accept", "file-reject"
	ID         string `json:"id"`
	SenderID   string `json:"senderId"`
	SenderName string `json:"senderName"`
	Content    string `json:"content,omitempty"`
	FileName   string `json:"fileName,omitempty"`
	FileSize   int64  `json:"fileSize,omitempty"`
}

// RegisterProtocols sets up stream handlers for messaging and file transfer
func (n *Node) RegisterProtocols(downloadDir string) {
	// Message protocol
	n.Host.SetStreamHandler(ProtocolMessage, func(s network.Stream) {
		defer s.Close()
		n.handleMessageStream(s)
	})

	// File transfer protocol
	n.Host.SetStreamHandler(ProtocolFile, func(s network.Stream) {
		defer s.Close()
		n.handleFileStream(s, downloadDir)
	})
}

// ---- Message protocol ----

func (n *Node) handleMessageStream(s network.Stream) {
	var wm wireMessage
	if err := json.NewDecoder(bufio.NewReader(s)).Decode(&wm); err != nil {
		return
	}

	msg := models.Message{
		ID:         wm.ID,
		DeviceID:   wm.SenderID,
		DeviceName: wm.SenderName,
		Content:    wm.Content,
		Time:       time.Now(),
		Direction:  "received",
	}

	n.mu.RLock()
	cb := n.onMessage
	n.mu.RUnlock()

	if cb != nil {
		cb(msg)
	}
}

// SendP2PMessage sends a text message to a P2P peer
func (n *Node) SendP2PMessage(peerIDStr string, content string, sender models.Device) (*models.Message, error) {
	pid, err := peer.Decode(peerIDStr)
	if err != nil {
		return nil, fmt.Errorf("decode peer ID: %w", err)
	}

	msg := &models.Message{
		ID:         uuid.NewString(),
		DeviceID:   peerIDStr,
		DeviceName: "",
		Content:    content,
		Time:       time.Now(),
		Direction:  "sent",
	}

	wm := wireMessage{
		Type:       "text",
		ID:         msg.ID,
		SenderID:   sender.ID,
		SenderName: sender.Name,
		Content:    content,
	}

	s, err := n.Host.NewStream(n.ctx, pid, ProtocolMessage)
	if err != nil {
		return msg, fmt.Errorf("open stream: %w", err)
	}
	defer s.Close()

	if err := json.NewEncoder(s).Encode(wm); err != nil {
		return msg, fmt.Errorf("send: %w", err)
	}

	return msg, nil
}

// ---- File transfer protocol ----

func (n *Node) handleFileStream(s network.Stream, downloadDir string) {
	reader := bufio.NewReader(s)

	// Read header
	var wm wireMessage
	line, err := reader.ReadString('\n')
	if err != nil {
		return
	}
	if err := json.Unmarshal([]byte(line), &wm); err != nil {
		return
	}

	if wm.Type != "file-header" {
		return
	}

	// Send accept
	accept := wireMessage{Type: "file-accept"}
	json.NewEncoder(s).Encode(accept)

	// Receive file data
	savePath := uniquePath(filepath.Join(downloadDir, wm.FileName))
	out, err := os.Create(savePath)
	if err != nil {
		return
	}
	defer out.Close()

	written, err := io.CopyN(out, reader, wm.FileSize)
	if err != nil && err != io.EOF {
		return
	}

	record := models.TransferRecord{
		ID:         wm.ID,
		DeviceID:   wm.SenderID,
		DeviceName: wm.SenderName,
		FileName:   wm.FileName,
		FileSize:   written,
		Direction:  "received",
		Status:     "completed",
		Time:       time.Now(),
	}

	n.mu.RLock()
	cb := n.onFileReceived
	n.mu.RUnlock()

	if cb != nil {
		cb(record)
	}
}

// SendP2PFile sends a file to a P2P peer
func (n *Node) SendP2PFile(peerIDStr string, filePath string, sender models.Device) (*models.TransferRecord, error) {
	pid, err := peer.Decode(peerIDStr)
	if err != nil {
		return nil, fmt.Errorf("decode peer ID: %w", err)
	}

	fileName := filepath.Base(filePath)
	fileSize := getFileSize(filePath)

	record := &models.TransferRecord{
		ID:         uuid.NewString(),
		DeviceID:   peerIDStr,
		DeviceName: "",
		FileName:   fileName,
		FileSize:   fileSize,
		Direction:  "sent",
		Status:     "transferring",
		Time:       time.Now(),
	}

	file, err := os.Open(filePath)
	if err != nil {
		record.Status = "failed"
		return record, err
	}
	defer file.Close()

	s, err := n.Host.NewStream(n.ctx, pid, ProtocolFile)
	if err != nil {
		record.Status = "failed"
		return record, fmt.Errorf("open stream: %w", err)
	}
	defer s.Close()

	// Send header
	wm := wireMessage{
		Type:       "file-header",
		ID:         record.ID,
		SenderID:   sender.ID,
		SenderName: sender.Name,
		FileName:   fileName,
		FileSize:   fileSize,
	}
	headerBytes, _ := json.Marshal(wm)
	s.Write(append(headerBytes, '\n'))

	// Read accept
	var response wireMessage
	if err := json.NewDecoder(bufio.NewReader(s)).Decode(&response); err != nil {
		record.Status = "failed"
		return record, fmt.Errorf("read response: %w", err)
	}
	if response.Type != "file-accept" {
		record.Status = "failed"
		return record, fmt.Errorf("peer rejected transfer")
	}

	// Send file data
	written, err := io.Copy(s, file)
	if err != nil {
		record.Status = "failed"
		return record, fmt.Errorf("send data: %w", err)
	}

	record.FileSize = written
	record.Status = "completed"
	return record, nil
}

// ---- Helpers ----

func (n *Node) savePeerName(pid peer.ID, name string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if ps, ok := n.peers[pid]; ok {
		ps.Name = name
	}
}

func uniquePath(path string) string {
	dir := filepath.Dir(path)
	ext := filepath.Ext(path)
	name := filepath.Base(path)
	nameWithoutExt := name[:len(name)-len(ext)]
	for i := 1; ; i++ {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path
		}
		path = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", nameWithoutExt, i, ext))
	}
}

func getFileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}
