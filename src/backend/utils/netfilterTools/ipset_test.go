package netfilterTools

import (
	"net"
	"testing"
)

func TestIPv6SubnetString(t *testing.T) {
	for _, test := range []struct {
		address string
		cidr    uint8
		want    string
	}{
		{"2001:db8::1234", 128, "2001:db8::1234/128"},
		{"2001:db8::", 32, "2001:db8::/32"},
		{"::1", 0, "::1"},
	} {
		subnet := IPv6Subnet{Address: [16]byte(net.ParseIP(test.address).To16()), CIDR: test.cidr}
		if got := subnet.String(); got != test.want {
			t.Errorf("IPv6 subnet = %q, want %q", got, test.want)
		}
	}
}
