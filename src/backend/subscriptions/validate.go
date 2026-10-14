package subscriptions

import (
	"net/netip"
	"strings"

	"github.com/dlclark/regexp2"
)

var (
	domainCharRe   = regexp2.MustCompile(`^[a-zA-Z0-9-.]+$`, 0)
	wildcardCharRe = regexp2.MustCompile(`^[a-zA-Z0-9\-.*?]+$`, 0)
)

func isValidWildcard(pattern string) bool {
	if pattern == "" {
		return false
	}
	if strings.HasPrefix(pattern, ".") || strings.HasSuffix(pattern, ".") {
		return false
	}
	if strings.Contains(pattern, "..") || strings.Contains(pattern, "**") {
		return false
	}
	ok, _ := wildcardCharRe.MatchString(pattern)
	return ok
}

func isValidDomain(pattern string) bool {
	if pattern == "" {
		return false
	}
	if strings.HasPrefix(pattern, ".") || strings.HasSuffix(pattern, ".") {
		return false
	}
	if strings.Contains(pattern, "..") {
		return false
	}
	ok, _ := domainCharRe.MatchString(pattern)
	return ok
}

func isValidNamespace(pattern string) bool {
	return isValidDomain(pattern)
}

func isValidSubnet(pattern string) bool {
	addr, ok := parseSubnetAddress(pattern)
	return ok && addr.Is4()
}

func isValidSubnet6(pattern string) bool {
	addr, ok := parseSubnetAddress(pattern)
	return ok && addr.Is6()
}

func parseSubnetAddress(pattern string) (netip.Addr, bool) {
	if strings.Contains(pattern, "/") {
		prefix, err := netip.ParsePrefix(pattern)
		return prefix.Addr(), err == nil
	}
	addr, err := netip.ParseAddr(pattern)
	return addr, err == nil && addr.Zone() == ""
}

func isValidRegex(pattern string) bool {
	re, err := regexp2.Compile(pattern, 0)
	return err == nil && re != nil
}
