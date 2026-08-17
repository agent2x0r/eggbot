package dcc

import (
	"net"
	"testing"
)

func TestParseErrorsAndDialReject(t *testing.T) {
	if _, _, err := ParseCHAT("short"); err == nil {
		t.Fatal("short")
	}
	if _, _, err := ParseCHAT("SEND file 1.2.3.4 1"); err == nil {
		t.Fatal("not chat")
	}
	if _, _, err := ParseCHAT("CHAT chat nope 1"); err == nil {
		t.Fatal("bad ip")
	}
	if _, _, err := ParseCHAT("CHAT chat 1.2.3.4 99999"); err == nil {
		t.Fatal("port")
	}
	if EncodeIPv4(net.ParseIP("::1")) != 0 {
		t.Fatal("v6")
	}
	if _, err := DialCHAT(net.IPv4(127, 0, 0, 1), 1); err == nil {
		t.Fatal("loopback dial")
	}
}
