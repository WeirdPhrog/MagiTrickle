package subscriptions

import "testing"

func TestSubnetValidation(t *testing.T) {
	for _, pattern := range []string{"::", "::1", "2001:db8::1/64", "2001:db8:1:2:3:4:5:6/128", "::ffff:192.0.2.1", "::/0"} {
		if !isValidSubnet6(pattern) {
			t.Errorf("rejected IPv6 %q", pattern)
		}
	}
	for _, pattern := range []string{":", "1::2::3", "12345::1", "1:2:3:4:5:6:7", "1:2:3:4:5:6:7:8::", "::1/", "::1/+64", "::1/064", "::1/129", "::1/18446744073709551616", "fe80::1%eth0", "::ffff:999.1.1.1", "::1\n"} {
		if isValidSubnet6(pattern) {
			t.Errorf("accepted invalid IPv6 %q", pattern)
		}
	}
	for _, pattern := range []string{"0.0.0.0/0", "192.0.2.1", "255.255.255.255/32"} {
		if !isValidSubnet(pattern) {
			t.Errorf("rejected IPv4 %q", pattern)
		}
	}
	for _, pattern := range []string{"256.0.0.1", "192.168.001.1", "1.2.3.4/33", "1.2.3.4/", "1.2.3.4/01", "1.2.3", "1.2.3.4\n"} {
		if isValidSubnet(pattern) {
			t.Errorf("accepted invalid IPv4 %q", pattern)
		}
	}
}
