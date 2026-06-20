package localnet

import (
	"fmt"
	"net"
)

type IPResolver interface {
	IPv4ByInterface(name string) (string, error)
}

type NetIPResolver struct{}

func (NetIPResolver) IPv4ByInterface(name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("interface name is empty")
	}
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return "", fmt.Errorf("find interface %q: %w", name, err)
	}
	addrs, err := iface.Addrs()
	if err != nil {
		return "", fmt.Errorf("list addresses for interface %q: %w", name, err)
	}
	for _, addr := range addrs {
		ip, _, err := net.ParseCIDR(addr.String())
		if err != nil {
			continue
		}
		ip4 := ip.To4()
		if ip4 == nil {
			continue
		}
		if ip4.IsLoopback() {
			continue
		}
		return ip4.String(), nil
	}
	return "", fmt.Errorf("interface %q has no non-loopback IPv4 address", name)
}
