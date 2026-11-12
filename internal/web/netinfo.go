// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

package web

import (
	"net"
	"sort"
	"strings"
)

// Address is a usable address on one of this machine's network interfaces.
type Address struct {
	Interface string
	IP        string
}

// LocalAddresses lists the local interface addresses that can be listened on,
// skipping loopback, link-local and down interfaces.
func LocalAddresses() []Address {
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

	var addresses []Address
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}

			ip := ipNet.IP
			if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
				continue
			}
			if !ip.IsGlobalUnicast() {
				continue
			}

			addresses = append(addresses, Address{Interface: iface.Name, IP: ip.String()})
		}
	}

	sort.Slice(addresses, func(i, j int) bool {
		if addresses[i].Interface != addresses[j].Interface {
			return addresses[i].Interface < addresses[j].Interface
		}
		return addresses[i].IP < addresses[j].IP
	})
	return addresses
}

// IsTailscale reports whether the address falls inside the 100.64.0.0/10 range
// used by Tailscale.
func IsTailscale(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}

	v4 := parsed.To4()
	if v4 == nil {
		return false
	}
	return v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127
}

// physicalPrefixes name the interfaces a phone on the same Wi-Fi can reach.
// macOS uses en0/en1, Linux en*/eth*/wl*, Windows reports friendly names.
var physicalPrefixes = []string{"en", "eth", "wl", "wi-fi", "wifi", "ethernet", "以太网"}

// virtualPrefixes are tunnels, bridges and container links. They are up and
// may even carry a private address, but the LAN cannot reach them.
var virtualPrefixes = []string{
	"utun", "awdl", "llw", "bridge", "vmnet", "vmenet", "docker", "veth",
	"br-", "tap", "tun", "wg", "zt", "ham", "tailscale", "gif", "stf", "anpi",
}

// PreferredAddress returns the address other devices on the network should use:
// the best private IPv4 address on a physical interface. It returns "" when the
// machine has no such address.
func PreferredAddress() string {
	interfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	best, bestScore := "", -1<<30
	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}

			ip := ipNet.IP.To4()
			if ip == nil || !ip.IsGlobalUnicast() || ip.IsLinkLocalUnicast() {
				continue
			}

			if score := scoreAddress(iface.Name, ip); score > bestScore {
				best, bestScore = ip.String(), score
			}
		}
	}
	return best
}

// scoreAddress ranks candidates. A private address on a physical interface wins,
// which is the one a phone on the same network can actually open.
func scoreAddress(name string, ip net.IP) int {
	score := 0
	if ip.IsPrivate() {
		score += 4
	}

	lower := strings.ToLower(name)
	switch {
	case hasAnyPrefix(lower, physicalPrefixes):
		score += 3
	case hasAnyPrefix(lower, virtualPrefixes):
		score -= 3
	}

	if IsTailscale(ip.String()) {
		score -= 4
	}
	return score
}

func hasAnyPrefix(name string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
