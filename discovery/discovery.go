package discovery

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net"
	"strings"
	"sync"
	"time"

	"lanlink/models"
)

const (
	broadcastPort   = 19999
	announceInterval = 3 * time.Second
	staleTimeout     = 10 * time.Second
)

// Service handles device discovery via UDP broadcast
type Service struct {
	mu       sync.RWMutex
	devices  map[string]*models.Device
	localDev models.Device
	conn     *net.UDPConn
	stopCh   chan struct{}
	onChange func([]models.Device)
	p2pID    string // libp2p PeerID, set after P2P starts — broadcast for cross-mode dedup
}

// NewService creates a discovery service
func NewService(deviceID string, deviceName string, onChange func([]models.Device)) *Service {
	if deviceID == "" {
		deviceID = generateID()
	}
	return &Service{
		devices: make(map[string]*models.Device),
		localDev: models.Device{
			ID:   deviceID,
			Name: deviceName,
		},
		stopCh:   make(chan struct{}),
		onChange: onChange,
	}
}

// SetDeviceName updates this device's display name
func (s *Service) SetDeviceName(name string) {
	s.mu.Lock()
	s.localDev.Name = name
	s.mu.Unlock()
}

// Start begins broadcasting presence and listening for peers
func (s *Service) Start(transferPort int) error {
	// Bind to 0.0.0.0:19999 — receives unicast + broadcast packets
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: broadcastPort})
	if err != nil {
		return err
	}
	s.conn = conn
	s.localDev.Port = transferPort
	s.localDev.IP = getLocalIP()
	s.localDev.LastSeen = time.Now()
	s.localDev.Online = true

	// Rapid announcement burst on start (3 packets in 1s)
	for i := 0; i < 3; i++ {
		s.broadcastOnce()
		time.Sleep(300 * time.Millisecond)
	}

	go s.broadcastLoop()
	go s.listenLoop()
	go s.staleCleanupLoop()
	return nil
}

// Stop shuts down the discovery service
func (s *Service) Stop() {
	close(s.stopCh)
	if s.conn != nil {
		s.conn.Close()
	}
}

// GetDevices returns all discovered devices
func (s *Service) GetDevices() []models.Device {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]models.Device, 0, len(s.devices))
	for _, d := range s.devices {
		result = append(result, *d)
	}
	return result
}

// LocalDevice returns info about this device
func (s *Service) LocalDevice() models.Device {
	return s.localDev
}

// SetGroupIDs updates the group IDs this device belongs to
func (s *Service) SetGroupIDs(groupIDs []string) {
	s.mu.Lock()
	s.localDev.Groups = groupIDs
	s.mu.Unlock()
}

// SetP2PID sets the libp2p PeerID so it can be included in discovery broadcasts
// for cross-mode (LAN+P2P) device deduplication.
func (s *Service) SetP2PID(id string) {
	s.mu.Lock()
	s.p2pID = id
	s.localDev.P2PID = id
	s.mu.Unlock()
}

func (s *Service) broadcastOnce() {
	s.localDev.IP = getLocalIP()
	s.mu.RLock()
	p2pID := s.p2pID
	s.mu.RUnlock()
	packet := models.DiscoveryPacket{
		ID:       s.localDev.ID,
		Name:     s.localDev.Name,
		IP:       s.localDev.IP,
		Port:     s.localDev.Port,
		GroupIDs: s.localDev.Groups,
		P2PID:    p2pID,
	}
	data, _ := json.Marshal(packet)
	targets := s.buildBroadcastTargets()
	for _, dst := range targets {
		s.conn.WriteToUDP(data, dst)
	}
}

func (s *Service) broadcastLoop() {
	ticker := time.NewTicker(announceInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.broadcastOnce()
		}
	}
}
func (s *Service) buildBroadcastTargets() []*net.UDPAddr {
	var targets []*net.UDPAddr

	// Universal broadcast (255.255.255.255)
	targets = append(targets, &net.UDPAddr{
		IP:   net.IPv4bcast,
		Port: broadcastPort,
	})

	// Subnet-directed broadcasts from each interface
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagBroadcast == 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || ipnet.IP.To4() == nil || ipnet.IP.IsLoopback() {
				continue
			}
			ip4 := ipnet.IP.To4()
			mask := ipnet.Mask
			bcast := make(net.IP, 4)
			for i := 0; i < 4; i++ {
				bcast[i] = ip4[i] | ^mask[i]
			}
			targets = append(targets, &net.UDPAddr{
				IP:   bcast,
				Port: broadcastPort,
			})
		}
	}
	return targets
}

func (s *Service) listenLoop() {
	buf := make([]byte, 2048)
	for {
		select {
		case <-s.stopCh:
			return
		default:
		}

		s.conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, remoteAddr, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			continue
		}

		// Ignore our own broadcasts
		if remoteAddr.IP.Equal(net.ParseIP(s.localDev.IP)) {
			continue
		}

		var packet models.DiscoveryPacket
		if err := json.Unmarshal(buf[:n], &packet); err != nil {
			continue
		}
		if packet.ID == s.localDev.ID {
			continue
		}

		s.mu.Lock()
		device, exists := s.devices[packet.ID]
		if !exists {
			device = &models.Device{ID: packet.ID, Name: packet.Name}
			s.devices[packet.ID] = device
		}
		device.IP = remoteAddr.IP.String()
		device.Port = packet.Port
		device.Groups = packet.GroupIDs
		device.P2PID = packet.P2PID
		device.LastSeen = time.Now()
		device.Online = true
		s.mu.Unlock()

		if s.onChange != nil {
			s.onChange(s.GetDevices())
		}
	}
}

func (s *Service) staleCleanupLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.mu.Lock()
			changed := false
			for id, device := range s.devices {
				wasOnline := device.Online
				device.Online = time.Since(device.LastSeen) < staleTimeout
				if wasOnline && !device.Online {
					changed = true
				}
				if time.Since(device.LastSeen) > 60*time.Second {
					delete(s.devices, id)
					changed = true
				}
			}
			s.mu.Unlock()
			if changed && s.onChange != nil {
				s.onChange(s.GetDevices())
			}
		}
	}
}

// GetLocalIP returns the best LAN IPv4 address, preferring physical
// interfaces over VPN / virtual adapters. Exported so identity can
// use the same selection for MAC matching.
func GetLocalIP() string {
	return getLocalIP()
}

func getLocalIP() string {
	// Prefer the best LAN IP from a physical interface.
	// The old Dial("udp", "8.8.8.8:80") trick picks whichever interface
	// the kernel routes to the internet — which may be a VPN (Radmin,
	// WireGuard, etc.) with an unroutable virtual IP like 26.x.x.x.
	if ip := bestLANIP(); ip != "" {
		return ip
	}
	// Fallback: route-based detection
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}

// bestLANIP returns the IPv4 address of the highest-quality LAN interface,
// skipping loopback, virtual adapters (VPN, VM, Docker, WSL), and down links.
// Private ranges (192.168.x.x, 10.x.x.x, 172.16-31.x.x) are preferred.
func bestLANIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	type candidate struct {
		ip    string
		score int
	}
	var best *candidate

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		name := strings.ToLower(iface.Name)
		// Skip known virtual / VPN / tunnel adapters
		if isVirtualAdapter(name) {
			continue
		}

		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || ipnet.IP.To4() == nil || ipnet.IP.IsLoopback() {
				continue
			}
			ip4 := ipnet.IP.To4()
			score := 0
			if ip4[0] == 192 && ip4[1] == 168 {
				score = 100 // most common LAN
			} else if ip4[0] == 10 {
				score = 90 // corporate LAN
			} else if ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31 {
				score = 80 // Docker / corporate
			} else if ip4[0] == 169 && ip4[1] == 254 {
				score = -50 // APIPA (no DHCP), avoid
			} else {
				score = 0 // public / VPN / other
			}
			if best == nil || score > best.score {
				best = &candidate{ip: ip4.String(), score: score}
			}
		}
	}
	if best != nil {
		return best.ip
	}
	return ""
}

// isVirtualAdapter returns true for common virtual/VPN/tunnel interface name patterns.
func isVirtualAdapter(name string) bool {
	for _, kw := range []string{
		"vmware", "virtualbox", "vbox", "vpn", "tap", "tunnel",
		"pseudo", "docker", "wsl", "hyper-v", "bluetooth",
		"utun", "vmnet", "bridge", "gif", "stf", "awdl", "llw",
		"veth", "virbr", "tun", "radmin", "wireguard", "wg",
		"zerotier", "zt", "tailscale", "ts", "hamachi",
		"ppp", "pptp", "l2tp", "sstp", "openvpn", "nordlynx",
		"loopback", "software loopback",
	} {
		if strings.Contains(name, kw) {
			return true
		}
	}
	return false
}

func generateID() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}
