package p2p

import (
	"fmt"
	"net"
	"sync"

	"github.com/huin/goupnp/dcps/internetgateway1"
	"github.com/huin/goupnp/dcps/internetgateway2"
)

// UPnPResult holds the result of UPnP port mapping
type UPnPResult struct {
	Enabled      bool   `json:"enabled"`
	Protocol     string `json:"protocol"`
	InternalPort int    `json:"internalPort"`
	ExternalPort int    `json:"externalPort"`
	ExternalIP   string `json:"externalIP"`
	LocalIP      string `json:"localIP"`
	Error        string `json:"error,omitempty"`
}

var upnpResult *UPnPResult
var upnpMu sync.Mutex

// upnpClient wraps v1 and v2 IGD clients
type upnpClient struct {
	v1 *internetgateway1.WANIPConnection1
	v2 *internetgateway2.WANIPConnection2
}

func (c *upnpClient) GetExternalIP() (string, error) {
	if c.v2 != nil {
		return c.v2.GetExternalIPAddress()
	}
	return c.v1.GetExternalIPAddress()
}

func (c *upnpClient) AddPortMapping(localIP string, port int, protocol string) error {
	if c.v2 != nil {
		return c.v2.AddPortMapping("", uint16(port), protocol, uint16(port), localIP, true, "Lanlink P2P", 0)
	}
	return c.v1.AddPortMapping("", uint16(port), protocol, uint16(port), localIP, true, "Lanlink P2P", 0)
}

func (c *upnpClient) DeletePortMapping(port int, protocol string) {
	if c.v2 != nil {
		c.v2.DeletePortMapping("", uint16(port), protocol)
	} else {
		c.v1.DeletePortMapping("", uint16(port), protocol)
	}
}

func discoverUPnP() (*upnpClient, string, error) {
	clients2, _, err2 := internetgateway2.NewWANIPConnection2Clients()
	if err2 == nil && len(clients2) > 0 {
		return &upnpClient{v2: clients2[0]}, "IGDv2", nil
	}
	clients1, _, err1 := internetgateway1.NewWANIPConnection1Clients()
	if err1 == nil && len(clients1) > 0 {
		return &upnpClient{v1: clients1[0]}, "IGDv1", nil
	}
	if err2 != nil {
		return nil, "", fmt.Errorf("UPnP discovery failed: v2=%v, v1=%v", err2, err1)
	}
	return nil, "", fmt.Errorf("no UPnP IGD device found, router may not support UPnP")
}

// TryUPnPMapping attempts to map a port via UPnP on the router
func TryUPnPMapping(internalPort int) UPnPResult {
	upnpMu.Lock()
	defer upnpMu.Unlock()

	result := UPnPResult{
		InternalPort: internalPort,
		ExternalPort: internalPort,
	}

	// Get local IP first (useful even if UPnP fails)
	result.LocalIP = getLocalIP()

	client, protocol, err := discoverUPnP()
	if err != nil {
		result.Error = err.Error()
		upnpResult = &result
		return result
	}
	result.Protocol = protocol

	extIP, err := client.GetExternalIP()
	if err != nil {
		result.Error = fmt.Sprintf("got IGD device but failed to get external IP: %v", err)
		upnpResult = &result
		return result
	}
	result.ExternalIP = extIP

	client.DeletePortMapping(internalPort, "TCP")
	client.DeletePortMapping(internalPort, "UDP")

	if err := client.AddPortMapping(result.LocalIP, internalPort, "TCP"); err != nil {
		result.Error = fmt.Sprintf("TCP port mapping failed: %v", err)
		upnpResult = &result
		return result
	}
	_ = client.AddPortMapping(result.LocalIP, internalPort, "UDP")

	result.Enabled = true
	upnpResult = &result
	return result
}

// CleanupUPnP removes the UPnP port mapping
func CleanupUPnP(internalPort int) {
	client, _, err := discoverUPnP()
	if err != nil {
		return
	}
	client.DeletePortMapping(internalPort, "TCP")
	client.DeletePortMapping(internalPort, "UDP")
}

// GetUPnPResult returns the last UPnP mapping result
func GetUPnPResult() *UPnPResult {
	upnpMu.Lock()
	defer upnpMu.Unlock()
	if upnpResult == nil {
		return nil
	}
	c := *upnpResult
	return &c
}

func getLocalIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		addrs, err := net.InterfaceAddrs()
		if err != nil {
			return ""
		}
		for _, addr := range addrs {
			if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
		return ""
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}
