package main

import (
	"fmt"
	"net"

	mnl "github.com/onmetal/metalbond/netlink/nl"

	log "github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
	"github.com/vishvananda/netlink/nl"
)

// We want to do the following with Go code:
//
// # ip link add overlay-tun mtu 1500 type ip6tnl mode any external
// # ip link set up dev overlay-tun
// # ip addr add 192.0.2.42/32 dev overlay-tun
// # ip addr add 2001:db8:dead:beef::/128 dev overlay-tun
//
// # ip route add 192.88.99.0/24 encap ip6 dst 2a10:afc0:e010:: dev overlay-tun
// # ip -6 route add 2001:db8::1/128 encap ip6 dst 2a10:afc0:e010:: dev overlay-tun
//
// The problem is the `route add` part. I think vishvananda/netlink is missing the encap netlink messages for ip6tnl.

func main() {
	linkName := "overlay-tun"

	attrs := netlink.NewLinkAttrs()
	attrs.Name = linkName
	attrs.MTU = 1500

	if err := netlink.LinkAdd(&netlink.Ip6tnl{
		LinkAttrs: attrs,
	}); err != nil {
		log.Warnf("Cannot add link: %v", err)
	}

	link, err := netlink.LinkByName(linkName)
	if err != nil {
		log.Fatalf("Cannot find link '%s': %v", linkName, err)
	}

	if err := netlink.LinkSetUp(link); err != nil {
		log.Fatalf("Cannot bring up link: %v", err)
	}

	v4addr, _ := netlink.ParseAddr("192.0.2.42/32")
	if err := netlink.AddrAdd(link, v4addr); err != nil {
		log.Warnf("Cannot add IPv4 address to link: %v", err)
	}

	v6addr, _ := netlink.ParseAddr("2001:db8:dead:beef::/128")
	if err := netlink.AddrAdd(link, v6addr); err != nil {
		log.Warnf("Cannot add IPv6 address to link: %v", err)
	}

	_, dst, err := net.ParseCIDR("192.88.99.0/24")
	if err != nil {
		log.Fatalf("cannot parse destination prefix: %v", err)
	}

	tnl := IP6Tnl{
		Dst: net.ParseIP("2a10:afc0:e010::"),
		Src: net.ParseIP("::"),
	}
	log.Infof("adding tunnel: %s", tnl)

	route := &netlink.Route{
		LinkIndex: link.Attrs().Index,
		Dst:       dst,
		Encap:     tnl,
	}

	if err := netlink.RouteAdd(route); err != nil {
		log.Fatalf("Cannot add route: %v", err)
	}
}

type IP6Tnl struct {
	ID       uint
	Src      net.IP
	Dst      net.IP
	Hoplimit uint8
	TC       uint8
	Flags    uint16
}

func (t IP6Tnl) Type() int {
	return 4
}

func (t IP6Tnl) Decode(input []byte) error {
	return nil
}

func (t IP6Tnl) Encode() ([]byte, error) {
	native := nl.NativeEndian()
	resID := make([]byte, 12)
	native.PutUint16(resID, 12) // msg length
	native.PutUint16(resID[2:], mnl.LWTUNNEL_IP6_ID)
	native.PutUint64(resID[4:], 0)

	resDst := make([]byte, 4)
	native.PutUint16(resDst, 20)
	native.PutUint16(resDst[2:], mnl.LWTUNNEL_IP6_DST)
	resDst = append(resDst, t.Dst...)

	resSrc := make([]byte, 4)
	native.PutUint16(resSrc, 20)
	native.PutUint16(resSrc[2:], mnl.LWTUNNEL_IP6_SRC)
	resSrc = append(resSrc, t.Src...)

	resTc := make([]byte, 5)
	native.PutUint16(resTc, 5)
	native.PutUint16(resTc[2:], mnl.LWTUNNEL_IP6_TC)
	resTc[5] = t.TC

	resHops := make([]byte, 5)
	native.PutUint16(resHops, 5)
	native.PutUint16(resHops[2:], mnl.LWTUNNEL_IP6_HOPLIMIT)
	resHops[5] = t.Hoplimit

	resFlags := make([]byte, 6)
	native.PutUint16(resFlags, 6)
	native.PutUint16(resFlags[2:], mnl.LWTUNNEL_IP6_FLAGS)
	native.PutUint16(resFlags[2:], t.Flags)

	res := []byte{}
	res = append(res, resID...)
	res = append(res, resDst...)
	res = append(res, resSrc...)
	res = append(res, resTc...)
	res = append(res, resHops...)
	res = append(res, resFlags...)

	return res, nil
}

func (t IP6Tnl) String() string {
	return fmt.Sprintf("id %d src %s dst %s hoplimit %d tc %d flags 0x%.4x", t.ID, t.Src, t.Dst, t.Hoplimit, t.TC, t.Flags)
}

func (t IP6Tnl) Equal(other netlink.Encap) bool {
	switch x := other.(type) {
	case IP6Tnl:
		return t.ID == x.ID && t.Src.Equal(x.Src) && t.Dst.Equal(x.Dst)
	default:
		return false
	}
}
