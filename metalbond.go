package metalbond

import (
	"fmt"
	"net"
	"sync"

	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
)

type RouteTable struct {
	VNI    VNI
	Routes map[Destination][]NextHop
}

type MetalBond struct {
	mtxRouteTables sync.RWMutex
	routeTables    map[VNI]RouteTable

	mtxMyAnnouncements sync.RWMutex
	myAnnouncements    map[VNI]RouteTable
	mtxMySubscriptions sync.RWMutex
	mySubscriptions    map[VNI]bool

	mtxSubscriptions sync.RWMutex                    // this locks a bit much (all VNIs). We could create a mutex for every VNI instead.
	subscriptions    map[VNI]map[*MetalBondPeer]bool // HashMap of HashSet

	peers             map[string]*MetalBondPeer
	peerMtx           sync.RWMutex
	keepaliveInterval uint32
	shuttingDown      bool

	installRoutes      bool
	tunDevice          netlink.Link
	kernelRouteTableID int

	lis      *net.Listener // for server only
	isServer bool
}

func NewMetalBond(keepaliveInterval uint32) *MetalBond {
	m := MetalBond{
		routeTables:       map[VNI]RouteTable{},
		myAnnouncements:   make(map[VNI]RouteTable),
		mySubscriptions:   make(map[VNI]bool),
		subscriptions:     make(map[VNI]map[*MetalBondPeer]bool),
		keepaliveInterval: keepaliveInterval,
		peers:             map[string]*MetalBondPeer{},
	}

	return &m
}

func (m *MetalBond) AddPeer(addr string, direction ConnectionDirection) error {
	m.peerMtx.Lock()
	defer m.peerMtx.Unlock()

	m.log().Infof("Adding peer %s", addr)
	if _, exists := m.peers[addr]; exists {
		return fmt.Errorf("Peer already registered")
	}

	m.peers[addr] = NewMetalBondPeer(
		nil,
		addr,
		m.keepaliveInterval,
		direction,
		m)

	return nil
}

func (m *MetalBond) RemovePeer(addr string) error {
	m.peerMtx.Lock()
	defer m.peerMtx.Unlock()

	m.log().Infof("Removing peer %s", addr)
	if _, exists := m.peers[addr]; !exists {
		m.log().Errorf("Peer %s does not exist", addr)
		return nil
	}

	m.peers[addr].Close()

	delete(m.peers, addr)
	return nil
}

func (m *MetalBond) Subscribe(vni VNI) error {
	m.mtxMySubscriptions.Lock()
	defer m.mtxMySubscriptions.Unlock()

	if _, exists := m.mySubscriptions[vni]; exists {
		return fmt.Errorf("Already subscribed to VNI %d", vni)
	}

	m.mySubscriptions[vni] = true

	for _, p := range m.peers {
		p.Subscribe(vni)
	}

	return nil
}

func (m *MetalBond) Unsubscribe(vni VNI) error {
	return nil
}

func (m *MetalBond) AnnounceRoute(vni VNI, dest Destination, hop NextHop) error {
	m.log().Infof("Announcing VNI %d: %s via %s", vni, dest, hop)

	m.mtxMyAnnouncements.Lock()

	if _, exists := m.myAnnouncements[vni]; !exists {
		m.myAnnouncements[vni] = RouteTable{
			VNI:    vni,
			Routes: make(map[Destination][]NextHop),
		}
	}

	if _, exists := m.myAnnouncements[vni].Routes[dest]; !exists {
		m.myAnnouncements[vni].Routes[dest] = []NextHop{hop}
	}
	m.mtxMyAnnouncements.Unlock()

	m.peerMtx.RLock()
	defer m.peerMtx.RUnlock()

	err := m.distributeRouteToPeers(nil, vni, dest, hop)
	if err != nil {
		m.log().Errorf("Could not distribute route to peers: %v", err)
	}

	return nil
}

func (m *MetalBond) WithdrawRoute(vni VNI, dest Destination, hop NextHop) error {
	return nil
}

func (m *MetalBond) distributeRouteToPeers(fromPeer *MetalBondPeer, vni VNI, dest Destination, hop NextHop) error {
	m.mtxSubscriptions.RLock()
	defer m.mtxSubscriptions.RUnlock()
	if _, exists := m.subscriptions[vni]; !exists {
		return nil
	}

	for p := range m.subscriptions[vni] {
		if p == fromPeer {
			m.log().WithField("peer", p).Debugf("Received the route from this peer. Skipping redistribution.")
			continue
		}

		if m.isServer && p.isServer {
			m.log().WithField("peer", p).Debugf("Do not redistribute route received from another server. Skipping redistribution.")
			continue
		}

		upd := msgUpdate{
			VNI:         vni,
			Destination: dest,
			NextHop:     hop,
		}

		err := p.SendUpdate(upd)
		if err != nil {
			m.log().WithField("peer", p).Debugf("Could not send update to peer: %v", err)
		}
	}

	return nil
}

func (m *MetalBond) getMyAnnouncements() []RouteTable {
	t := []RouteTable{}
	m.mtxMyAnnouncements.RLock()
	defer m.mtxMyAnnouncements.RUnlock()

	for _, x := range m.myAnnouncements {
		t = append(t, x)
	}

	return t
}

func (m *MetalBond) addReceivedRoute(fromPeer *MetalBondPeer, vni VNI, dest Destination, hop NextHop) error {
	m.mtxRouteTables.Lock()

	if _, exists := m.routeTables[vni]; !exists {
		m.routeTables[vni] = RouteTable{
			VNI:    vni,
			Routes: make(map[Destination][]NextHop),
		}
	}

	if _, exists := m.routeTables[vni].Routes[dest]; !exists {
		m.routeTables[vni].Routes[dest] = []NextHop{hop}
	}
	m.mtxRouteTables.Unlock()

	m.log().Infof("Received Route: VNI %d, Prefix: %s, NextHop: %s", vni, dest, hop)

	m.distributeRouteToPeers(fromPeer, vni, dest, hop)

	return nil
}

func (m *MetalBond) addSubscriber(peer *MetalBondPeer, vni VNI) error {
	m.log().Infof("addSubscriber(%s, %d)", peer, vni)
	m.mtxSubscriptions.Lock()

	if _, exists := m.subscriptions[vni]; !exists {
		m.subscriptions[vni] = make(map[*MetalBondPeer]bool)
	}

	if _, exists := m.subscriptions[vni][peer]; exists {
		return fmt.Errorf("Peer is already subscribed!")
	}

	m.subscriptions[vni][peer] = true
	m.mtxSubscriptions.Unlock()

	m.log().Infof("Peer %s added Subscription to VNI %d", peer, vni)

	m.mtxRouteTables.RLock()
	defer m.mtxRouteTables.RUnlock()
	if _, exists := m.routeTables[vni]; !exists {
		return nil
	}
	for dest, hops := range m.routeTables[vni].Routes {
		for _, hop := range hops {
			err := peer.SendUpdate(msgUpdate{
				Action:      ADD,
				VNI:         vni,
				Destination: dest,
				NextHop:     hop,
			})
			if err != nil {
				m.log().Errorf("Could not send UPDATE to peer: %v", err)
				peer.Reset()
			}
		}
	}

	return nil
}

func (m *MetalBond) StartServer(listenAddress string) error {
	lis, err := net.Listen("tcp", listenAddress)
	m.lis = &lis
	if err != nil {
		return fmt.Errorf("Cannot open TCP port: %v", err)
	}
	m.isServer = true

	m.log().Infof("Listening on %s", listenAddress)

	go func() {
		for {
			conn, err := lis.Accept()
			if m.shuttingDown {
				return
			} else if err != nil {
				m.log().Errorf("Error accepting incoming connection: %v", err)
				return
			}

			m.peers[conn.RemoteAddr().String()] = NewMetalBondPeer(
				&conn,
				conn.RemoteAddr().String(),
				m.keepaliveInterval,
				INCOMING,
				m,
			)
		}
	}()

	return nil
}

func (m *MetalBond) Shutdown() {
	m.log().Infof("Shutting down MetalBond...")
	m.shuttingDown = true
	if m.lis != nil {
		(*m.lis).Close()
	}

	for p := range m.peers {
		m.RemovePeer(p)
	}

	//time.Sleep(2 * time.Second)
}

func (m *MetalBond) EnableNetlink(linkName string, routeTable int) error {
	link, err := netlink.LinkByName(linkName)
	if err != nil {
		return fmt.Errorf("Cannot get link '%s': %v", linkName, err)
	}

	m.installRoutes = true
	m.tunDevice = link
	m.kernelRouteTableID = routeTable

	m.log().Infof("Enabled installing routes into route table %d via %s", m.kernelRouteTableID, m.tunDevice.Attrs().Name)

	return nil
}

func (m *MetalBond) log() *logrus.Entry {
	return logrus.WithFields(nil)
}
