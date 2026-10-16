package netfilterTools

import (
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"

	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
	"golang.org/x/sys/unix"
)

type IPv4Subnet struct {
	Address [4]byte
	CIDR    uint8
}

func (subnet IPv4Subnet) String() string {
	if subnet.CIDR == 0 {
		return fmt.Sprintf("%d.%d.%d.%d", subnet.Address[0], subnet.Address[1], subnet.Address[2], subnet.Address[3])
	} else {
		return fmt.Sprintf("%d.%d.%d.%d/%d", subnet.Address[0], subnet.Address[1], subnet.Address[2], subnet.Address[3], subnet.CIDR)
	}
}

type IPv6Subnet struct {
	Address [16]byte
	CIDR    uint8
}

func (subnet IPv6Subnet) String() string {
	if subnet.CIDR == 0 {
		return net.IP(subnet.Address[:]).String()
	} else {
		return fmt.Sprintf("%s/%d", net.IP(subnet.Address[:]), subnet.CIDR)
	}
}

type IPSetTimeout *uint32

var zeroTimeout = IPSetTimeout(new(uint32))

type IPSet struct {
	enabled atomic.Bool
	locker  sync.Mutex

	ipsetName string
}

func (r *IPSet) AddIPv4Subnet(subnet IPv4Subnet, timeout IPSetTimeout) error {
	r.locker.Lock()
	defer r.locker.Unlock()

	if !r.enabled.Load() {
		return nil
	}

	if timeout == nil {
		timeout = zeroTimeout
	}

	err := netlink.IpsetAdd(r.ipsetName+"_4", &netlink.IPSetEntry{
		IP:      subnet.Address[:],
		CIDR:    subnet.CIDR,
		Timeout: timeout,
		Replace: true,
	})
	if err != nil {
		return fmt.Errorf("failed to add address: %w", err)
	}

	return nil
}

func (r *IPSet) AddIPv6Subnet(subnet IPv6Subnet, timeout IPSetTimeout) error {
	r.locker.Lock()
	defer r.locker.Unlock()

	if !r.enabled.Load() {
		return nil
	}

	if timeout == nil {
		timeout = zeroTimeout
	}

	err := netlink.IpsetAdd(r.ipsetName+"_6", &netlink.IPSetEntry{
		IP:      subnet.Address[:],
		CIDR:    subnet.CIDR,
		Timeout: timeout,
		Replace: true,
	})
	if err != nil {
		return fmt.Errorf("failed to add address: %w", err)
	}

	return nil
}

func (r *IPSet) DelIPv4Subnet(subnet IPv4Subnet) error {
	r.locker.Lock()
	defer r.locker.Unlock()

	if !r.enabled.Load() {
		return nil
	}

	err := netlink.IpsetDel(r.ipsetName+"_4", &netlink.IPSetEntry{
		IP:   subnet.Address[:],
		CIDR: subnet.CIDR,
	})
	if err != nil && !errors.Is(err, nl.IPSetError(nl.IPSET_ERR_EXIST)) {
		return fmt.Errorf("failed to delete address: %w", err)
	}

	return nil
}

func (r *IPSet) DelIPv6Subnet(subnet IPv6Subnet) error {
	r.locker.Lock()
	defer r.locker.Unlock()

	if !r.enabled.Load() {
		return nil
	}

	err := netlink.IpsetDel(r.ipsetName+"_6", &netlink.IPSetEntry{
		IP:   subnet.Address[:],
		CIDR: subnet.CIDR,
	})
	if err != nil && !errors.Is(err, nl.IPSetError(nl.IPSET_ERR_EXIST)) {
		return fmt.Errorf("failed to delete address: %w", err)
	}

	return nil
}

func (r *IPSet) ListIPv4Subnets() (map[IPv4Subnet]IPSetTimeout, error) {
	r.locker.Lock()
	defer r.locker.Unlock()

	if !r.enabled.Load() {
		return nil, nil
	}

	addresses := make(map[IPv4Subnet]IPSetTimeout)

	list, err := netlink.IpsetList(r.ipsetName + "_4")
	if err != nil {
		return nil, err
	}
	for _, entry := range list.Entries {
		if len(entry.IP) != net.IPv4len {
			continue
		}
		subnet := IPv4Subnet{
			Address: [4]byte(entry.IP),
			CIDR:    entry.CIDR,
		}
		if entry.Timeout != nil && *entry.Timeout == 0 {
			addresses[subnet] = nil
		} else {
			addresses[subnet] = entry.Timeout
		}
	}

	return addresses, nil
}

func (r *IPSet) ListIPv6Subnets() (map[IPv6Subnet]IPSetTimeout, error) {
	r.locker.Lock()
	defer r.locker.Unlock()

	if !r.enabled.Load() {
		return nil, nil
	}

	addresses := make(map[IPv6Subnet]IPSetTimeout)

	list, err := netlink.IpsetList(r.ipsetName + "_6")
	if err != nil {
		return nil, err
	}
	for _, entry := range list.Entries {
		if len(entry.IP) != net.IPv6len {
			continue
		}
		subnet := IPv6Subnet{
			Address: [16]byte(entry.IP),
			CIDR:    entry.CIDR,
		}
		if entry.Timeout != nil && *entry.Timeout == 0 {
			addresses[subnet] = nil
		} else {
			addresses[subnet] = entry.Timeout
		}
	}

	return addresses, nil
}

// Replace prepares both families before publishing either. An invalid entry or
// a full set must leave the live routing set intact, not partially emptied.
func (r *IPSet) Replace(v4 map[IPv4Subnet]IPSetTimeout, v6 map[IPv6Subnet]IPSetTimeout) error {
	r.locker.Lock()
	defer r.locker.Unlock()
	if !r.enabled.Load() {
		return fmt.Errorf("ipset is disabled")
	}
	tmp4, tmp6 := r.ipsetName+"_t4", r.ipsetName+"_t6"
	for _, name := range []string{tmp4, tmp6} {
		if err := netlink.IpsetDestroy(name); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	defer netlink.IpsetDestroy(tmp4)
	defer netlink.IpsetDestroy(tmp6)
	create := func(name string, family uint8, count int) error {
		// maxelem is a limit, not an up-front allocation. Leave room for DNS
		// hosts and allow subscriptions larger than ipset's 65536 default.
		return netlink.IpsetCreate(name, "hash:net", netlink.IpsetCreateOptions{
			Timeout: new(uint32(300)), Family: family,
			MaxElements: uint32(max(65536, count+count/4)),
		})
	}
	if err := create(tmp4, unix.AF_INET, len(v4)); err != nil {
		return err
	}
	if err := create(tmp6, unix.AF_INET6, len(v6)); err != nil {
		return err
	}
	for subnet, ttl := range v4 {
		if ttl == nil {
			ttl = zeroTimeout
		}
		if err := netlink.IpsetAdd(tmp4, &netlink.IPSetEntry{IP: subnet.Address[:], CIDR: subnet.CIDR, Timeout: ttl}); err != nil {
			return fmt.Errorf("prepare IPv4 set: %w", err)
		}
	}
	for subnet, ttl := range v6 {
		if ttl == nil {
			ttl = zeroTimeout
		}
		if err := netlink.IpsetAdd(tmp6, &netlink.IPSetEntry{IP: subnet.Address[:], CIDR: subnet.CIDR, Timeout: ttl}); err != nil {
			return fmt.Errorf("prepare IPv6 set: %w", err)
		}
	}
	if err := netlink.IpsetSwap(tmp4, r.ipsetName+"_4"); err != nil {
		return err
	}
	if err := netlink.IpsetSwap(tmp6, r.ipsetName+"_6"); err != nil {
		return errors.Join(err, netlink.IpsetSwap(tmp4, r.ipsetName+"_4"))
	}
	return nil
}

func (r *IPSet) ipsetCreate() error {
	err := netlink.IpsetCreate(r.ipsetName+"_4", "hash:net", netlink.IpsetCreateOptions{
		Timeout: func(i uint32) *uint32 { return &i }(300),
		Family:  unix.AF_INET,
	})
	if err != nil {
		return fmt.Errorf("failed to create ipset: %w", err)
	}

	err = netlink.IpsetCreate(r.ipsetName+"_6", "hash:net", netlink.IpsetCreateOptions{
		Timeout: func(i uint32) *uint32 { return &i }(300),
		Family:  unix.AF_INET6,
	})
	if err != nil {
		return fmt.Errorf("failed to create ipset: %w", err)
	}

	return nil
}

func (r *IPSet) ipsetDestroy() error {
	var errs []error
	err := netlink.IpsetDestroy(r.ipsetName + "_4")
	if err != nil && !os.IsNotExist(err) {
		errs = append(errs, err)
	}
	err = netlink.IpsetDestroy(r.ipsetName + "_6")
	if err != nil && !os.IsNotExist(err) {
		errs = append(errs, err)
	}
	if errs != nil {
		return fmt.Errorf("failed to destroy ipsets: %w", errors.Join(errs...))
	}
	return nil
}

func (r *IPSet) enable() error {
	if !r.enabled.CompareAndSwap(false, true) {
		return nil
	}

	err := r.ipsetDestroy()
	if err != nil {
		return err
	}

	err = r.ipsetCreate()
	if err != nil {
		return err
	}

	return nil
}

func (r *IPSet) Enable() error {
	r.locker.Lock()
	defer r.locker.Unlock()

	err := r.enable()
	if err != nil {
		r.disable()
	}

	return err
}

func (r *IPSet) disable() error {
	if !r.enabled.Load() {
		return nil
	}
	defer r.enabled.Store(false)

	return r.ipsetDestroy()
}

func (r *IPSet) Disable() error {
	r.locker.Lock()
	defer r.locker.Unlock()

	return r.disable()
}

func (nh *Helper) IPSet(name string) *IPSet {
	return &IPSet{
		ipsetName: nh.IpsetPrefix + name,
	}
}
