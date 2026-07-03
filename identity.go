package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"lanlink/discovery"
	"lanlink/models"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// IdentityConfig stores persistent device identity
type IdentityConfig struct {
	DeviceID    string `json:"deviceId"`
	DisplayName string `json:"displayName"`
	Created     string `json:"created"`
}

// ChatStore holds all persisted chat data
type ChatStore struct {
	Messages  []models.Message        `json:"messages"`
	History   []models.TransferRecord `json:"history"`
	Groups    []models.Group          `json:"groups"`
	Paired    []PairedPeer            `json:"paired"`
	Updated   string                  `json:"updated"`
}

// PairedPeer stores a previously connected peer for auto-reconnect
type PairedPeer struct {
	PeerID string `json:"peerId"`
	Name   string `json:"name"`
}

func (a *App) pairedPath() string {
	return filepath.Join(a.homeDir, ".lanlink", "paired.json")
}

// addPairedPeer persists a peer for auto-reconnect (dedup by PeerID)
func (a *App) addPairedPeer(peerID string, name string) {
	if peerID == "" {
		return
	}
	exists := false
	for i := range a.paired {
		if a.paired[i].PeerID == peerID {
			a.paired[i].Name = name
			exists = true
			break
		}
	}
	if !exists {
		a.paired = append(a.paired, PairedPeer{PeerID: peerID, Name: name})
	}
	a.savePaired()
}

func (a *App) savePaired() {
	data, _ := json.MarshalIndent(a.paired, "", "  ")
	os.WriteFile(a.pairedPath(), data, 0600)
}

func (a *App) loadPaired() {
	data, err := os.ReadFile(a.pairedPath())
	if err != nil {
		return
	}
	json.Unmarshal(data, &a.paired)
}

func (a *App) identityPath() string {
	return filepath.Join(a.homeDir, ".lanlink", "identity.json")
}

func (a *App) chatStorePath() string {
	return filepath.Join(a.homeDir, ".lanlink", "chat_data.json")
}

// loadOrCreateIdentity loads saved identity or derives one from the MAC address.
// The MAC address is used as the stable device ID (doesn't change across reinstalls).
func (a *App) loadOrCreateIdentity() {
	ipath := a.identityPath()
	os.MkdirAll(filepath.Dir(ipath), 0700)

	// MAC address is the source of truth for identity
	mac := getPrimaryMAC()
	if mac == "" {
		mac = generateFallbackID() // only if no NIC has a MAC (extremely rare)
	}
	a.deviceID = mac

	// Load saved display name if present
	if data, err := os.ReadFile(ipath); err == nil {
		var cfg IdentityConfig
		if json.Unmarshal(data, &cfg) == nil && cfg.DisplayName != "" {
			a.deviceName = cfg.DisplayName
		}
	}

	if a.deviceName == "" {
		a.deviceName, _ = os.Hostname()
	}

	// Persist (deviceID derived from MAC each launch, but we store for record)
	cfg := IdentityConfig{
		DeviceID:    a.deviceID,
		DisplayName: a.deviceName,
		Created:     time.Now().Format(time.RFC3339),
	}
	if data, err := json.MarshalIndent(cfg, "", "  "); err == nil {
		os.WriteFile(ipath, data, 0600)
	}
}

// getPrimaryMAC returns the MAC address of the best physical network interface.
// Fallback chain:
//  1. Match the LAN IP (from discovery.GetLocalIP) to its interface MAC
//  2. Pick the first non-loopback, non-virtual interface with a valid MAC
//  3. Use hostname as a stable ID
//  4. Last resort: random hex
func getPrimaryMAC() string {
	primaryIP := getLocalIPForMAC()

	interfaces, err := net.Interfaces()
	if err != nil {
		return fallbackMAC()
	}

	// Pass 1: exact IP match
	if primaryIP != "" {
		if mac := findMACByIP(interfaces, primaryIP); mac != "" {
			return mac
		}
	}

	// Pass 2: first non-virtual interface with a valid MAC
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		hw := iface.HardwareAddr.String()
		if hw == "" || len(iface.HardwareAddr) < 6 {
			continue
		}
		if isVirtualAdapterName(iface.Name) {
			continue
		}
		return hw
	}

	return fallbackMAC()
}

func findMACByIP(interfaces []net.Interface, targetIP string) string {
	ip := net.ParseIP(targetIP)
	if ip == nil {
		return ""
	}
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		hw := iface.HardwareAddr.String()
		if hw == "" || len(iface.HardwareAddr) < 6 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.Equal(ip) {
				return hw
			}
		}
	}
	return ""
}

// fallbackMAC returns a stable fallback when no physical MAC is available.
// Prefers hostname (stable across restarts) over random hex.
func fallbackMAC() string {
	if host, err := os.Hostname(); err == nil && host != "" {
		return "host:" + host
	}
	return "rnd:" + generateFallbackID()
}

func isVirtualAdapterName(name string) bool {
	n := strings.ToLower(name)
	for _, kw := range []string{
		"vmware", "virtualbox", "vbox", "vpn", "tap", "tunnel",
		"pseudo", "docker", "wsl", "hyper-v", "bluetooth",
		"utun", "vmnet", "bridge", "gif", "stf", "awdl", "llw",
		"veth", "virbr", "tun", "radmin", "wireguard", "wg",
		"zerotier", "zt", "tailscale", "ts", "hamachi",
		"ppp", "pptp", "l2tp", "sstp", "openvpn", "nordlynx",
		"loopback", "software loopback",
	} {
		if strings.Contains(n, kw) {
			return true
		}
	}
	return false
}

func getLocalIPForMAC() string {
	return discovery.GetLocalIP()
}

func generateFallbackID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// saveIdentity updates the display name
func (a *App) saveIdentity() {
	cfg := IdentityConfig{
		DeviceID:    a.deviceID,
		DisplayName: a.deviceName,
		Created:     time.Now().Format(time.RFC3339),
	}
	data, _ := json.MarshalIndent(cfg, "", "  ")
	os.WriteFile(a.identityPath(), data, 0600)
}

// SetDisplayName allows the user to change their display name
func (a *App) SetDisplayName(name string) {
	a.deviceName = name
	a.saveIdentity()
	if a.discovery != nil {
		a.discovery.SetDeviceName(name)
	}
	runtime.EventsEmit(a.ctx, "devices-changed", a.GetDevices())
}

// loadChatData loads persisted messages, history, and groups
func (a *App) loadChatData() {
	cpath := a.chatStorePath()
	data, err := os.ReadFile(cpath)
	if err != nil {
		return
	}
	var store ChatStore
	if json.Unmarshal(data, &store) != nil {
		return
	}
	if len(store.Messages) > 0 {
		a.messages = store.Messages
	}
	if len(store.History) > 0 {
		a.history = store.History
	}
	if len(store.Groups) > 0 {
		a.groups = store.Groups
	}
}

// saveChatData persists messages, history, and groups to disk
func (a *App) saveChatData() {
	store := ChatStore{
		Messages: a.messages,
		History:  a.history,
		Groups:   a.groups,
		Updated:  time.Now().Format(time.RFC3339),
	}
	// Keep max 500 messages and 200 history entries
	if len(store.Messages) > 500 {
		store.Messages = store.Messages[len(store.Messages)-500:]
	}
	if len(store.History) > 200 {
		store.History = store.History[len(store.History)-200:]
	}
	data, _ := json.MarshalIndent(store, "", "  ")
	os.WriteFile(a.chatStorePath(), data, 0600)
}

// settings.json
type AppSettings struct {
	DownloadDir     string   `json:"downloadDir"`
	AskSaveLocation bool     `json:"askSaveLocation"`
	CustomRelays    []string `json:"customRelays"`    // user-configured relay nodes
	UseDoH          bool     `json:"useDoH"`          // try DNS-over-HTTPS to bypass pollution
	TransportMode   string   `json:"transportMode"`   // "auto" (default) or "lan-only"
}

func (a *App) settingsPath() string {
	return filepath.Join(a.homeDir, ".lanlink", "settings.json")
}

func (a *App) loadSettings() {
	data, err := os.ReadFile(a.settingsPath())
	if err != nil {
		return
	}
	var s AppSettings
	if json.Unmarshal(data, &s) == nil {
		if s.DownloadDir != "" {
			a.downloadDir = s.DownloadDir
		}
		a.askSaveLocation = s.AskSaveLocation
		a.customRelays = s.CustomRelays
		a.useDoH = s.UseDoH
		if s.TransportMode != "" {
			a.transportMode = s.TransportMode
		}
	}
}

func (a *App) saveSettings() {
	s := AppSettings{DownloadDir: a.downloadDir, AskSaveLocation: a.askSaveLocation, CustomRelays: a.customRelays, UseDoH: a.useDoH, TransportMode: a.transportMode}
	data, _ := json.MarshalIndent(s, "", "  ")
	os.WriteFile(a.settingsPath(), data, 0600)
}

// GetSettings returns current app settings
func (a *App) GetSettings() AppSettings {
	return AppSettings{DownloadDir: a.downloadDir, AskSaveLocation: a.askSaveLocation, CustomRelays: a.customRelays, UseDoH: a.useDoH, TransportMode: a.transportMode}
}

// SetCustomRelays updates user-configured relay nodes
func (a *App) SetCustomRelays(relays []string) {
	a.customRelays = relays
	a.saveSettings()
}

// SetUseDoH toggles DNS-over-HTTPS
func (a *App) SetUseDoH(on bool) {
	a.useDoH = on
	a.saveSettings()
}

// SetTransportMode sets the transport mode ("auto" or "lan-only")
func (a *App) SetTransportMode(mode string) {
	if mode != "auto" && mode != "lan-only" {
		return
	}
	a.transportMode = mode
	a.saveSettings()

	// Apply immediately: stop P2P if switching to lan-only
	if mode == "lan-only" {
		a.StopP2P()
	} else if mode == "auto" {
		// Start P2P if not already running
		if a.p2pNode == nil {
			go func() {
				if err := a.startP2PInBackground(); err != nil {
					runtime.LogErrorf(a.ctx, "Transport mode switch P2P start failed: %v", err)
				}
			}()
		}
	}
	runtime.EventsEmit(a.ctx, "devices-changed", a.GetDevices())
}

// SetAskSaveLocation toggles whether to prompt for save location each download
func (a *App) SetAskSaveLocation(on bool) {
	a.askSaveLocation = on
	a.saveSettings()
}

// ChooseDownloadDir opens a folder picker and returns the chosen path
func (a *App) ChooseDownloadDir() string {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select Download Folder",
	})
	if err != nil || dir == "" {
		return ""
	}
	a.downloadDir = dir
	os.MkdirAll(dir, 0755)
	a.saveSettings()
	return dir
}

// SetDownloadDir sets the download directory directly
func (a *App) SetDownloadDir(dir string) string {
	if dir == "" {
		return a.downloadDir
	}
	a.downloadDir = dir
	os.MkdirAll(dir, 0755)
	a.saveSettings()
	return dir
}

// ============================================================
// Manual Devices (for Radmin VPN / manual IP entry)
// ============================================================

func (a *App) manualDevicesPath() string {
	return filepath.Join(a.homeDir, ".lanlink", "manual_devices.json")
}

func (a *App) loadManualDevices() {
	data, err := os.ReadFile(a.manualDevicesPath())
	if err != nil {
		return
	}
	json.Unmarshal(data, &a.manualDevices)
}

func (a *App) saveManualDevices() {
	data, _ := json.MarshalIndent(a.manualDevices, "", "  ")
	os.WriteFile(a.manualDevicesPath(), data, 0600)
}

// PingDevice tests if a device's transfer server is reachable. Returns "ok" or error.
func (a *App) PingDevice(ip string) string {
	url := fmt.Sprintf("http://%s:%d/ping", ip, transferPort)
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "unreachable: " + err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return "ok"
	}
	return "HTTP " + resp.Status
}

// AddDeviceByIP manually adds a device by IP (e.g. a Radmin VPN virtual IP).
// Pings first; returns the device if reachable, nil otherwise.
func (a *App) AddDeviceByIP(ip string, name string) *models.Device {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return nil
	}

	// Validate reachability
	if a.PingDevice(ip) != "ok" {
		return nil
	}

	if name == "" {
		name = "Device " + ip
	}

	dev := models.Device{
		ID:     "manual-" + ip,
		Name:   name,
		IP:     ip,
		Port:   transferPort,
		Online: true,
		Source: "manual",
	}

	// Dedup by ID
	for i, d := range a.manualDevices {
		if d.ID == dev.ID {
			a.manualDevices[i] = dev
			a.saveManualDevices()
			runtime.EventsEmit(a.ctx, "devices-changed", a.GetDevices())
			return &dev
		}
	}
	a.manualDevices = append(a.manualDevices, dev)
	a.saveManualDevices()
	runtime.EventsEmit(a.ctx, "devices-changed", a.GetDevices())
	return &dev
}

// RemoveManualDevice removes a manually-added device
func (a *App) RemoveManualDevice(id string) {
	newList := make([]models.Device, 0)
	for _, d := range a.manualDevices {
		if d.ID != id {
			newList = append(newList, d)
		}
	}
	a.manualDevices = newList
	a.saveManualDevices()
	runtime.EventsEmit(a.ctx, "devices-changed", a.GetDevices())
}

// ============================================================
// Known Devices (persistent device history)
// ============================================================

func (a *App) knownDevicesPath() string {
	return filepath.Join(a.homeDir, ".lanlink", "known_devices.json")
}

func (a *App) loadKnownDevices() {
	data, err := os.ReadFile(a.knownDevicesPath())
	if err != nil {
		return
	}
	json.Unmarshal(data, &a.knownDevices)
}

func (a *App) saveKnownDevices() {
	data, _ := json.MarshalIndent(a.knownDevices, "", "  ")
	os.WriteFile(a.knownDevicesPath(), data, 0600)
}

// RemoveKnownDevice removes a device from the known devices list
func (a *App) RemoveKnownDevice(id string) {
	newList := make([]models.Device, 0)
	for _, d := range a.knownDevices {
		if d.ID != id {
			newList = append(newList, d)
		}
	}
	a.knownDevices = newList
	a.saveKnownDevices()
	runtime.EventsEmit(a.ctx, "devices-changed", a.GetDevices())
}
