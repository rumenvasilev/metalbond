// Copyright 2022 OnMetal authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package metalbond

import (
	"fmt"
	"github.com/vishvananda/netlink"
	"net"
	"sync"
)

const METALBOND_RT_PROTO netlink.RouteProtocol = 254

type NetlinkClient struct {
	config    NetlinkClientConfig
	tunDevice netlink.Link
	mtx       sync.Mutex
}

type NetlinkClientConfig struct {
	VNITableMap map[VNI]int
	LinkName    string
}

func NewNetlinkClient(config NetlinkClientConfig) (*NetlinkClient, error) {
	link, err := netlink.LinkByName(config.LinkName)
	if err != nil {
		return nil, fmt.Errorf("Cannot find tun device '%s': %v", config.LinkName, err)
	}

	// TODO: Remove all routes from route tables defined in config.VNITableMap with Protocol = METALBOND_RT_PROTO
	// to clean up old, stale routes installed by a prior metalbond client instance

	return &NetlinkClient{
		config:    config,
		tunDevice: link,
	}, nil
}

func (c *NetlinkClient) AddRoute(vni VNI, dest Destination, hop NextHop) error {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	table, exists := c.config.VNITableMap[vni]
	if !exists {
		return fmt.Errorf("No route table ID known for given VNI")
	}

	_, dst, err := net.ParseCIDR(dest.Prefix.String())
	if err != nil {
		return fmt.Errorf("cannot parse destination prefix: %v", err)
	}

	encap := netlink.IP6tnlEncap{
		Dst: net.ParseIP(hop.TargetAddress.String()),
		Src: net.ParseIP("::"), // what source ip to put here? Metalbond object, m, does not contain this info yet.
	}

	route := &netlink.Route{
		LinkIndex: c.tunDevice.Attrs().Index,
		Dst:       dst,
		Encap:     &encap,
		Table:     table,
		Protocol:  METALBOND_RT_PROTO,
	} // by default, the route is already installed into the kernel table without explicite specification

	if err := netlink.RouteAdd(route); err != nil {
		return fmt.Errorf("cannot add route to %s (table %d) to kernel: %v", dest, table, err)
	}

	return nil
}

func (c *NetlinkClient) RemoveRoute(vni VNI, dest Destination, hop NextHop) error {
	c.mtx.Lock()
	defer c.mtx.Unlock()

	table, exists := c.config.VNITableMap[vni]
	if !exists {
		return fmt.Errorf("No route table ID known for given VNI")
	}

	_, dst, err := net.ParseCIDR(dest.Prefix.String())
	if err != nil {
		return fmt.Errorf("cannot parse destination prefix: %v", err)
	}

	encap := netlink.IP6tnlEncap{
		Dst: net.ParseIP(hop.TargetAddress.String()),
		Src: net.ParseIP("::"), // what source ip to put here? Metalbond object, m, does not contain this info yet.
	}

	route := &netlink.Route{
		LinkIndex: c.tunDevice.Attrs().Index,
		Dst:       dst,
		Encap:     &encap,
		Table:     table,
		Protocol:  METALBOND_RT_PROTO,
	} // by default, the route is already installed into the kernel table without explicite specification

	if err := netlink.RouteDel(route); err != nil {
		return fmt.Errorf("cannot remove route to %s (table %d) from kernel: %v", dest, table, err)
	}

	return nil
}
