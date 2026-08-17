package dcc

import (
	"net"
	"testing"
)

func TestParseAndEncode(t *testing.T) {
	ip := net.IPv4(1, 2, 3, 4)
	n := EncodeIPv4(ip)
	if n != 0x01020304 {
		t.Fatalf("%x", n)
	}
	got, port, err := ParseCHAT("CHAT chat 16909060 4000")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Equal(ip) || port != 4000 {
		t.Fatalf("%v %d", got, port)
	}
	if FormatOffer(ip, 4000) == "" {
		t.Fatal("offer")
	}
}

func TestParseRejectsInternalTargets(t *testing.T) {
	for _, offer := range []string{
		"CHAT chat 127.0.0.1 4000",
		"CHAT chat 10.0.0.1 4000",
		"CHAT chat 169.254.169.254 80",
		"CHAT chat 100.64.0.1 4000",
		"CHAT chat ::1 4000",
	} {
		if _, _, err := ParseCHAT(offer); err == nil {
			t.Fatalf("expected unsafe target rejection: %q", offer)
		}
	}
}
