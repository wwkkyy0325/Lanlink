package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"lanlink/discovery"
	"lanlink/models"
	"lanlink/p2p"
	"lanlink/transfer"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	transferPort = 20000
	p2pPort      = 20001
)

// App is the main application struct. All public methods are bound
// to the frontend via Wails runtime.
type App struct {
	ctx            context.Context
	discovery      *discovery.Service
	transferServer *transfer.Server
	p2pNode        *p2p.Node
	p2pMu          sync.Mutex
	p2pStarted     bool
	history        []models.TransferRecord
	messages       []models.Message
	groups         []models.Group
	deviceID       string
	deviceName     string
	paired         []PairedPeer
	manualDevices  []models.Device
	knownDevices   []models.Device
	homeDir        string
	downloadDir    string
	askSaveLocation bool
}

// NewApp creates a new App
func NewApp() *App {
	return &App{}
}

// startup is called when the app starts
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.homeDir, _ = os.UserHomeDir()
	a.downloadDir = filepath.Join(a.homeDir, "Downloads", "Lanlink")

	// Load persistent identity (device ID + display name)
	a.deviceName, _ = os.Hostname()
	a.loadOrCreateIdentity()

	// Load persisted chat data and paired peers
	a.loadChatData()
	a.loadPaired()
	a.loadSettings()
	a.loadManualDevices()
	a.loadKnownDevices()

	// Auto-start P2P in background (non-blocking)
	go func() {
		if err := a.startP2PInBackground(); err != nil {
			runtime.LogErrorf(a.ctx, "Auto P2P start failed: %v", err)
		}
	}()

	// Create transfer server
	a.transferServer = transfer.NewServer(
		transferPort,
		a.downloadDir,
		func() models.Device { return a.getLocalDevice() },
		func(r models.TransferRecord) {
			a.history = append(a.history, r)
			runtime.EventsEmit(a.ctx, "file-received", r)
		},
		func(m models.Message) {
			a.messages = append(a.messages, m)
			runtime.EventsEmit(a.ctx, "message-received", m)
		},
			func(req models.TransferRequest) {
				runtime.EventsEmit(a.ctx, "transfer-request", req)
			},
	)
	if err := a.transferServer.Start(); err != nil {
		runtime.LogErrorf(a.ctx, "Failed to start transfer server: %v", err)
	}

	// Create discovery service
	a.discovery = discovery.NewService(a.deviceID, a.deviceName, func(devices []models.Device) {
		runtime.EventsEmit(a.ctx, "devices-changed", a.GetDevices())
	})
	// Sync after identity load (name may have been overridden)
	a.discovery.SetDeviceName(a.deviceName)
	a.discovery.SetGroupIDs(groupIDs(a.groups))
	if err := a.discovery.Start(transferPort); err != nil {
		runtime.LogErrorf(a.ctx, "Failed to start discovery: %v", err)
	}

	runtime.LogInfof(a.ctx, "Lanlink started. Device: %s", a.deviceName)
}

// shutdown is called when the app is closing
func (a *App) shutdown(_ context.Context) {
	a.saveChatData()
	if a.discovery != nil {
		a.discovery.Stop()
	}
	if a.transferServer != nil {
		a.transferServer.Stop()
	}
	if a.p2pNode != nil {
		p2p.CleanupUPnP(p2pPort)
		a.p2pNode.Close()
	}
}

// ============================================================
// General
// ============================================================

// GetDevices returns all known devices. Online status is determined live from
// current discovery/P2P/manual lists, but offline devices are still shown
// (they were connected before and are persisted).
func (a *App) GetDevices() []models.Device {
	// Build a set of currently-online devices keyed by ID
	liveOnline := map[string]models.Device{}
	if a.discovery != nil {
		for _, d := range a.discovery.GetDevices() {
			liveOnline[d.ID] = d
		}
	}
	if a.p2pNode != nil {
		for _, d := range a.p2pNode.GetPeers() {
			liveOnline[d.ID] = d
		}
	}
	for _, d := range a.manualDevices {
		liveOnline[d.ID] = d
	}

	// Sync knownDevices with live data: update info for online ones,
	// add newly seen ones, mark others offline.
	a.syncKnownDevices(liveOnline)

	// Build result: local first, then all known (online first, offline after)
	result := []models.Device{a.getLocalDevice()}
	var online, offline []models.Device
	for _, d := range a.knownDevices {
		if d.ID == a.getLocalDevice().ID {
			continue
		}
		if d.Online {
			online = append(online, d)
		} else {
			offline = append(offline, d)
		}
	}
	result = append(result, online...)
	result = append(result, offline...)
	return result
}

// syncKnownDevices merges live device info into the persisted knownDevices list
func (a *App) syncKnownDevices(live map[string]models.Device) {
	now := time.Now()
	changed := false

	// Update existing + add new
	for id, d := range live {
		d.Online = true
		d.LastSeen = now
		found := false
		for i := range a.knownDevices {
			if a.knownDevices[i].ID == id {
				a.knownDevices[i] = d
				found = true
				break
			}
		}
		if !found {
			a.knownDevices = append(a.knownDevices, d)
			changed = true
		}
	}

	// Mark offline if not in live
	for i := range a.knownDevices {
		if _, ok := live[a.knownDevices[i].ID]; !ok {
			if a.knownDevices[i].Online {
				a.knownDevices[i].Online = false
			}
		}
	}

	if changed {
		a.saveKnownDevices()
	}
}

// GetLocalDevice returns info about this device
func (a *App) GetLocalDevice() models.Device {
	return a.getLocalDevice()
}

// GetDownloadDir returns the download directory path
func (a *App) GetDownloadDir() string {
	return a.downloadDir
}

// OpenDownloadFolder opens the download directory
func (a *App) OpenDownloadFolder() {
	runtime.BrowserOpenURL(a.ctx, "file:///"+a.downloadDir)
}

// GetHistory returns the transfer history
func (a *App) GetHistory() []models.TransferRecord {
	return a.transferServer.GetHistory()
}

// GetMessages returns chat message history (sent + received, persisted)
func (a *App) GetMessages() []models.Message {
	return a.messages
}

func (a *App) getLocalDevice() models.Device {
	if a.discovery != nil {
		d := a.discovery.LocalDevice()
		d.ID = a.deviceID
		d.Name = a.deviceName
		return d
	}
	return models.Device{
		ID:     a.deviceID,
		Name:   a.deviceName,
		Online: true,
		Source: "lan",
	}
}

// ============================================================
// LAN Methods
// ============================================================

// SendFile opens a file dialog then sends the selected file via LAN
func (a *App) SendFile(deviceIP string, deviceID string, deviceName string) *models.TransferRecord {
	filePath, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select File to Send",
	})
	if err != nil || filePath == "" {
		return nil
	}

	record, err := transfer.SendFile(deviceIP, transferPort, deviceID, deviceName, filePath, a.getLocalDevice())
	if err != nil {
		runtime.LogErrorf(a.ctx, "SendFile error: %v", err)
	}
	if record != nil {
		a.history = append(a.history, *record)
		a.saveChatData()
		runtime.EventsEmit(a.ctx, "transfer-update", *record)
	}
	return record
}

// SendFilePath sends a file by path (for drag-and-drop)
func (a *App) SendFilePath(deviceIP string, deviceID string, deviceName string, filePath string) *models.TransferRecord {
	record, err := transfer.SendFile(deviceIP, transferPort, deviceID, deviceName, filePath, a.getLocalDevice())
	if err != nil {
		runtime.LogErrorf(a.ctx, "SendFilePath error: %v", err)
	}
	if record != nil {
		a.history = append(a.history, *record)
		a.saveChatData()
		runtime.EventsEmit(a.ctx, "transfer-update", *record)
	}
	return record
}

// SendMultipleFiles sends multiple files via LAN
func (a *App) SendMultipleFiles(deviceIP string, deviceID string, deviceName string) []models.TransferRecord {
	filePaths, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Files to Send",
	})
	if err != nil || len(filePaths) == 0 {
		return nil
	}

	records, _ := transfer.SendFiles(deviceIP, transferPort, deviceID, deviceName, filePaths, a.getLocalDevice())
	for i := range records {
		a.history = append(a.history, records[i])
		runtime.EventsEmit(a.ctx, "transfer-update", records[i])
	}
	return records
}

// SendMessage sends a text message via LAN
func (a *App) SendMessage(deviceIP string, deviceID string, deviceName string, content string) *models.Message {
	msg, err := transfer.SendMessage(deviceIP, transferPort, deviceID, deviceName, content, a.getLocalDevice())
	if err != nil {
		runtime.LogErrorf(a.ctx, "SendMessage error: %v", err)
	}
	if msg != nil {
		a.messages = append(a.messages, *msg)
		a.saveChatData()
		runtime.EventsEmit(a.ctx, "message-sent", *msg)
	}
	return msg
}

// ============================================================
// P2P Methods
// ============================================================

// startP2PInBackground starts P2P automatically on app launch (no UPnP blocking).
// UPnP and peer reconnection happen asynchronously afterward.
func (a *App) startP2PInBackground() error {
	a.p2pMu.Lock()
	if a.p2pNode != nil || a.p2pStarted {
		a.p2pMu.Unlock()
		return nil
	}
	a.p2pStarted = true
	a.p2pMu.Unlock()

	// Timeout: if P2P node creation takes >15s, give up (e.g. no network)
	nodeCh := make(chan *p2p.Node, 1)
	errCh := make(chan error, 1)

	go func() {
		keyPath := filepath.Join(a.homeDir, ".lanlink", "p2p_key")
		node, err := p2p.NewNode(p2pPort, keyPath)
		if err != nil {
			errCh <- err
			return
		}
		nodeCh <- node
	}()

	var node *p2p.Node
	select {
	case n := <-nodeCh:
		node = n
	case err := <-errCh:
		a.p2pMu.Lock()
		a.p2pStarted = false // allow retry
		a.p2pMu.Unlock()
		return fmt.Errorf("create P2P node: %w", err)
	case <-time.After(15 * time.Second):
		a.p2pMu.Lock()
		a.p2pStarted = false // allow retry
		a.p2pMu.Unlock()
		return fmt.Errorf("P2P node creation timed out after 15s")
	}

	node.RegisterProtocols(a.downloadDir)
	node.SetCallbacks(
		func(info peer.AddrInfo) {
			// Auto-pair on successful connection
			a.addPairedPeer(info.ID.String(), "")
			runtime.EventsEmit(a.ctx, "devices-changed", a.GetDevices())
		},
		func(id peer.ID) {
			runtime.EventsEmit(a.ctx, "devices-changed", a.GetDevices())
		},
		func(m models.Message) {
			a.messages = append(a.messages, m)
			a.saveChatData()
			runtime.EventsEmit(a.ctx, "message-received", m)
		},
		func(r models.TransferRecord) {
			a.history = append(a.history, r)
			a.saveChatData()
			runtime.EventsEmit(a.ctx, "file-received", r)
		},
	)

	a.p2pNode = node
	runtime.LogInfof(a.ctx, "P2P auto-started. PeerID: %s", node.FullPeerID())
	runtime.EventsEmit(a.ctx, "p2p-started", node.FullPeerID())
	runtime.EventsEmit(a.ctx, "devices-changed", a.GetDevices())

	// Async: try UPnP (best-effort, don't block)
	go func() {
		res := p2p.TryUPnPMapping(p2pPort)
		runtime.LogInfof(a.ctx, "UPnP: enabled=%v externalIP=%s", res.Enabled, res.ExternalIP)
		runtime.EventsEmit(a.ctx, "p2p-status-updated", nil)
	}()

	// Async: auto-reconnect all previously paired peers via relays
	go a.autoReconnectPeers()

	return nil
}

// autoReconnectPeers tries to reconnect every persisted peer via the public relays
func (a *App) autoReconnectPeers() {
	if a.p2pNode == nil || len(a.paired) == 0 {
		return
	}
	time.Sleep(5 * time.Second) // give AutoRelay time to connect to relays

	for _, p := range a.paired {
		go a.p2pNode.ReconnectViaRelays(p.PeerID, p.Name)
	}
}

// StartP2P (legacy manual trigger, kept for compatibility) returns current status
func (a *App) StartP2P() p2p.UPnPResult {
	if a.p2pNode == nil {
		a.startP2PInBackground()
		time.Sleep(1 * time.Second)
	}
	res := p2p.GetUPnPResult()
	if res != nil {
		return *res
	}
	return p2p.UPnPResult{}
}

// StopP2P stops the P2P node
func (a *App) StopP2P() {
	if a.p2pNode != nil {
		p2p.CleanupUPnP(p2pPort)
		a.p2pNode.Close()
		a.p2pNode = nil
		runtime.EventsEmit(a.ctx, "devices-changed", a.GetDevices())
	}
}

// GetP2PStatus returns the P2P node status
func (a *App) GetP2PStatus() map[string]interface{} {
	result := map[string]interface{}{
		"enabled": a.p2pNode != nil,
	}

	if a.p2pNode != nil {
		result["peerId"] = a.p2pNode.FullPeerID()
		result["peerIdShort"] = a.p2pNode.PeerIDShort()
		result["addrs"] = a.p2pNode.AllMultiaddrs()
		result["nat"] = a.p2pNode.GetNatInfo()

		// Build connection string: prefer UPnP, then NAT-detected public addr, then relay
		upnpRes := p2p.GetUPnPResult()
		extIP := ""
		extPort := p2pPort
		if upnpRes != nil && upnpRes.Enabled && upnpRes.ExternalIP != "" {
			extIP = upnpRes.ExternalIP
			extPort = upnpRes.ExternalPort
		}
		result["connectionString"] = a.p2pNode.BuildConnectionString(extIP, extPort)
		result["connectionStringLocal"] = a.p2pNode.BuildConnectionStringLocal(p2pPort)
	}

	if upnp := p2p.GetUPnPResult(); upnp != nil {
		result["upnp"] = *upnp
	}

	return result
}

// ConnectP2P connects to a peer using a connection string
func (a *App) ConnectP2P(connStr string, name string) error {
	if a.p2pNode == nil {
		return nil
	}

	if err := a.p2pNode.ConnectByAddr(connStr); err != nil {
		return err
	}

	// Save the peer with a friendly name
	info, _ := p2p.ParseConnectionString(connStr)
	if info != nil && name != "" {
		a.p2pNode.SavePeer(info.ID.String(), name, connStr)
	}

	runtime.EventsEmit(a.ctx, "devices-changed", a.GetDevices())
	return nil
}

// DisconnectP2P disconnects from a P2P peer
func (a *App) DisconnectP2P(peerID string) {
	if a.p2pNode == nil {
		return
	}
	a.p2pNode.RemovePeer(peerID)

	// Close all connections to that peer
	for _, conn := range a.p2pNode.Host.Network().ConnsToPeer(peer.ID(peerID)) {
		conn.Close()
	}

	runtime.EventsEmit(a.ctx, "devices-changed", a.GetDevices())
}

// SendP2PMessage sends a text message to a P2P peer
func (a *App) SendP2PMessage(peerID string, content string) *models.Message {
	if a.p2pNode == nil {
		return nil
	}
	msg, err := a.p2pNode.SendP2PMessage(peerID, content, a.getLocalDevice())
	if err != nil {
		runtime.LogErrorf(a.ctx, "P2P send message error: %v", err)
	}
	if msg != nil {
		a.messages = append(a.messages, *msg)
		a.saveChatData()
		runtime.EventsEmit(a.ctx, "message-sent", *msg)
	}
	return msg
}

// SendP2PFile opens a file dialog and sends to a P2P peer
func (a *App) SendP2PFile(peerID string) *models.TransferRecord {
	if a.p2pNode == nil {
		return nil
	}

	filePath, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select File to Send",
	})
	if err != nil || filePath == "" {
		return nil
	}

	record, err := a.p2pNode.SendP2PFile(peerID, filePath, a.getLocalDevice())
	if err != nil {
		runtime.LogErrorf(a.ctx, "P2P send file error: %v", err)
	}
	if record != nil {
		a.history = append(a.history, *record)
		a.saveChatData()
		runtime.EventsEmit(a.ctx, "transfer-update", *record)
	}
	return record
}

// SendP2PFilePath sends a file by path to a P2P peer
func (a *App) SendP2PFilePath(peerID string, filePath string) *models.TransferRecord {
	if a.p2pNode == nil {
		return nil
	}

	record, err := a.p2pNode.SendP2PFile(peerID, filePath, a.getLocalDevice())
	if err != nil {
		runtime.LogErrorf(a.ctx, "P2P send file error: %v", err)
	}
	if record != nil {
		a.history = append(a.history, *record)
		a.saveChatData()
		runtime.EventsEmit(a.ctx, "transfer-update", *record)
	}
	return record
}

// ============================================================
// Pairing
// ============================================================

// StartPairing joins a pairing-code room so peers with the same code auto-discover each other
func (a *App) StartPairing(code string) error {
	if a.p2pNode == nil {
		return fmt.Errorf("P2P not started")
	}
	return a.p2pNode.JoinPairingRoom(code, a.deviceName, func(info peer.AddrInfo, name string) {
		// Auto-pair on discovery
		a.addPairedPeer(info.ID.String(), name)
		// Best-effort connect via relays
		go a.p2pNode.ReconnectViaRelays(info.ID.String(), name)
		runtime.EventsEmit(a.ctx, "devices-changed", a.GetDevices())
	})
}

// GetPairedPeers returns the persisted paired peer list
func (a *App) GetPairedPeers() []PairedPeer {
	return a.paired
}

// Unpair removes a paired peer
func (a *App) Unpair(peerID string) {
	newList := make([]PairedPeer, 0)
	for _, p := range a.paired {
		if p.PeerID != peerID {
			newList = append(newList, p)
		}
	}
	a.paired = newList
	a.savePaired()
}

// ============================================================
// Online / Offline
// ============================================================

var isOnline bool = true

// GoOffline stops all network activity without closing the app
func (a *App) GoOffline() {
	a.p2pMu.Lock()
	defer a.p2pMu.Unlock()
	isOnline = false
	runtime.LogInfof(a.ctx, "Going offline...")
	if a.discovery != nil {
		a.discovery.Stop()
	}
	if a.p2pNode != nil {
		p2p.CleanupUPnP(p2pPort)
		a.p2pNode.Close()
		a.p2pNode = nil
	}
	a.p2pStarted = false // allow restart later
	runtime.EventsEmit(a.ctx, "devices-changed", a.GetDevices())
	runtime.EventsEmit(a.ctx, "online-status-changed", false)
}

// GoOnline re-enables LAN discovery and P2P after going offline
func (a *App) GoOnline() {
	a.p2pMu.Lock()
	defer a.p2pMu.Unlock()
	isOnline = true
	runtime.LogInfof(a.ctx, "Going online...")

	// Restart discovery
	a.discovery = discovery.NewService(a.deviceID, a.deviceName, func(devices []models.Device) {
		runtime.EventsEmit(a.ctx, "devices-changed", a.GetDevices())
	})
	a.discovery.SetDeviceName(a.deviceName)
	if len(a.groups) > 0 {
		a.discovery.SetGroupIDs(groupIDs(a.groups))
	}
	if err := a.discovery.Start(transferPort); err != nil {
		runtime.LogErrorf(a.ctx, "Failed to restart discovery: %v", err)
	}

	// Restart P2P (p2pStarted was reset to false in GoOffline)
	go func() {
		if err := a.startP2PInBackground(); err != nil {
			runtime.LogErrorf(a.ctx, "Failed to restart P2P: %v", err)
		}
	}()

	runtime.EventsEmit(a.ctx, "devices-changed", a.GetDevices())
	runtime.EventsEmit(a.ctx, "online-status-changed", true)
}

// IsOnline returns whether the device is currently online
func (a *App) IsOnline() bool {
	return isOnline
}
