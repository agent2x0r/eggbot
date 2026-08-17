// Package dcc parses and dials incoming DCC CHAT offers (partyline over DCC).
package dcc

import (
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

// ParseCHAT parses "CHAT chat <ip> <port>" from a CTCP DCC argument.
func ParseCHAT(arg string) (ip net.IP, port int, err error) {
	fields := strings.Fields(arg)
	if len(fields) < 4 {
		return nil, 0, fmt.Errorf("short dcc chat")
	}
	if !strings.EqualFold(fields[0], "CHAT") {
		return nil, 0, fmt.Errorf("not dcc chat")
	}
	ip, err = decodeIP(fields[2])
	if err != nil {
		return nil, 0, err
	}
	if err := ValidateTarget(ip); err != nil {
		return nil, 0, err
	}
	port, err = strconv.Atoi(fields[3])
	if err != nil || port <= 0 || port > 65535 {
		return nil, 0, fmt.Errorf("bad dcc port")
	}
	return ip, port, nil
}

func decodeIP(s string) (net.IP, error) {
	if ip := net.ParseIP(s); ip != nil {
		return ip, nil
	}
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return nil, fmt.Errorf("bad dcc ip")
	}
	var b [4]byte
	binary.BigEndian.PutUint32(b[:], uint32(n))
	return net.IPv4(b[0], b[1], b[2], b[3]), nil
}

func EncodeIPv4(ip net.IP) uint32 {
	v4 := ip.To4()
	if v4 == nil {
		return 0
	}
	return binary.BigEndian.Uint32(v4)
}

// ValidateTarget blocks DCC offers from turning the bot into a connector to
// loopback, LAN, link-local, or carrier-grade NAT services.
func ValidateTarget(ip net.IP) error {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
		return fmt.Errorf("unsafe dcc target")
	}
	if _, cgnat, err := net.ParseCIDR("100.64.0.0/10"); err == nil && cgnat.Contains(ip) {
		return fmt.Errorf("unsafe dcc target")
	}
	return nil
}

// DialCHAT connects to an incoming DCC CHAT offer.
func DialCHAT(ip net.IP, port int) (net.Conn, error) {
	if err := ValidateTarget(ip); err != nil {
		return nil, err
	}
	addr := net.JoinHostPort(ip.String(), strconv.Itoa(port))
	d := net.Dialer{Timeout: 15 * time.Second}
	return d.Dial("tcp", addr)
}

// FormatOffer builds the CTCP DCC CHAT payload (without \001).
func FormatOffer(ip net.IP, port int) string {
	return fmt.Sprintf("DCC CHAT chat %d %d", EncodeIPv4(ip), port)
}
