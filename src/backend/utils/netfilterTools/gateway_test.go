package netfilterTools

import (
	"net"
	"testing"

	"github.com/vishvananda/netlink"
)

func TestGatewayIgnoresSpecificRoutesAndUsesDefaultMetric(t *testing.T) {
	for _, addresses := range [][3]string{{"192.0.2.99", "192.0.2.2", "192.0.2.1"}, {"2001:db8::99", "2001:db8::2", "2001:db8::1"}} {
		bits := 128
		if net.ParseIP(addresses[0]).To4() != nil {
			bits = 32
		}
		routes := []netlink.Route{
			{Dst: &net.IPNet{IP: net.ParseIP(addresses[0]), Mask: net.CIDRMask(bits, bits)}, Gw: net.ParseIP(addresses[0])},
			{Gw: net.ParseIP(addresses[1]), Priority: 100},
			{Gw: net.ParseIP(addresses[2]), Priority: 10},
		}
		if got := defaultGateway(routes); !got.Equal(net.ParseIP(addresses[2])) {
			t.Fatalf("selected %v, want default gateway %s", got, addresses[2])
		}
		if got := defaultGateway(routes[:1]); got != nil {
			t.Fatalf("specific route gateway incorrectly used as default: %v", got)
		}
	}
}
