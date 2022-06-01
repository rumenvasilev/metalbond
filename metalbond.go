package metalbond

import (
	"fmt"
	"net"
	"sync"

	"github.com/sirupsen/logrus"
	"github.com/vishvananda/netlink"
)

type MetalBond struct {
	routeTable routeTable

	myAnnouncements    routeTable
	mtxMySubscriptions sync.RWMutex
	mySubscriptions    map[VNI]bool

	mtxSubscriptions sync.RWMutex                    // this locks a bit much (all VNIs). We could create a mutex for every VNI instead.
	subscriptions    map[VNI]map[*metalBondPeer]bool // HashMap of HashSet

	peers             map[string]*metalBondPeer
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
		routeTable:        newRouteTable(),
		myAnnouncements:   newRouteTable(),
		mySubscriptions:   make(map[VNI]bool),
		subscriptions:     make(map[VNI]map[*metalBondPeer]bool),
		keepaliveInterval: keepaliveInterval,
		peers:             map[string]*metalBondPeer{},
	}

	return &m
}

func (m *MetalBond) StartHTTPServer(listen string) error {
	go serveJsonRouteTable(m, listen)

	return nil
}

func (m *MetalBond) AddPeer(addr string) error {
	m.peerMtx.Lock()
	defer m.peerMtx.Unlock()

	m.log().Infof("Adding peer %s", addr)
	if _, exists := m.peers[addr]; exists {
		return fmt.Errorf("Peer already registered")
	}

	m.peers[addr] = newMetalBondPeer(
		nil,
		addr,
		m.keepaliveInterval,
		OUTGOING,
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
	m.log().Errorf("Unsubscibe not implemented (VNI %d)", vni)
	return nil
}

func (m *MetalBond) AnnounceRoute(vni VNI, dest Destination, hop NextHop) error {
	m.log().Infof("Announcing VNI %d: %s via %s", vni, dest, hop)

	err := m.myAnnouncements.AddNextHop(vni, dest, hop, nil)
	if err != nil {
		return fmt.Errorf("Cannot announce route: %v", err)
	}

	m.peerMtx.RLock()
	defer m.peerMtx.RUnlock()

	err = m.distributeRouteToPeers(ADD, vni, dest, hop, nil)
	if err != nil {
		m.log().Errorf("Could not distribute route to peers: %v", err)
	}

	return nil
}

func (m *MetalBond) WithdrawRoute(vni VNI, dest Destination, hop NextHop) error {
	return nil
}

func (m *MetalBond) getMyAnnouncements() *routeTable {
	return &m.myAnnouncements
}

func (m *MetalBond) distributeRouteToPeers(action UpdateAction, vni VNI, dest Destination, hop NextHop, fromPeer *metalBondPeer) error {
	m.mtxSubscriptions.RLock()
	defer m.mtxSubscriptions.RUnlock()
	if _, exists := m.subscriptions[vni]; !exists {
		return nil
	}

	for p := range m.subscriptions[vni] {
		if p == fromPeer {
			//m.log().WithField("peer", p).Debugf("Received the route from this peer. Skipping redistribution.")
			continue
		}

		if fromPeer.isServer && p.isServer {
			//m.log().WithField("peer", p).Debugf("Do not redistribute route received from another server. Skipping redistribution.")
			continue
		}

		upd := msgUpdate{
			Action:      action,
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

func (m *MetalBond) addReceivedRoute(fromPeer *metalBondPeer, vni VNI, dest Destination, hop NextHop) error {
	err := m.routeTable.AddNextHop(vni, dest, hop, fromPeer)
	if err != nil {
		return fmt.Errorf("Cannot add route to route table: %v", err)
	}

	m.log().Infof("Received Route: VNI %d, Prefix: %s, NextHop: %s", vni, dest, hop)

	m.distributeRouteToPeers(ADD, vni, dest, hop, fromPeer)

	return nil
}

func (m *MetalBond) removeReceivedRoute(fromPeer *metalBondPeer, vni VNI, dest Destination, hop NextHop) error {
	err, remaining := m.routeTable.RemoveNextHop(vni, dest, hop, fromPeer)
	if err != nil {
		return fmt.Errorf("Cannot remove route from route table: %v", err)
	}

	m.log().Infof("Removed Received Route: VNI %d, Prefix: %s, NextHop: %s", vni, dest, hop)

	if remaining == 0 {
		m.distributeRouteToPeers(REMOVE, vni, dest, hop, fromPeer)
	}

	return nil
}

// addSubscriber is called by metalBondPeer when an SUBSCRIBE message has been received from the peer.
// Route updates belonging to the specified VNI will be sent to the peer afterwards.
func (m *MetalBond) addSubscriber(peer *metalBondPeer, vni VNI) error {
	m.log().Infof("addSubscriber(%s, %d)", peer, vni)
	m.mtxSubscriptions.Lock()

	if _, exists := m.subscriptions[vni]; !exists {
		m.subscriptions[vni] = make(map[*metalBondPeer]bool)
	}

	if _, exists := m.subscriptions[vni][peer]; exists {
		return fmt.Errorf("Peer is already subscribed!")
	}

	m.subscriptions[vni][peer] = true
	m.mtxSubscriptions.Unlock()

	m.log().Infof("Peer %s added Subscription to VNI %d", peer, vni)

	for dest, hops := range m.routeTable.GetDestinationsByVNI(vni) {
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

// removeSubscriber is called by metalBondPeer when an UNSUBSCRIBE message has been received from the peer.
func (m *MetalBond) removeSubscriber(peer *metalBondPeer, vni VNI) error {
	return fmt.Errorf("NOT IMPLEMENTED")
}

// StartServer starts the MetalBond server asynchronously.
// To stop the server again, call Shutdown().
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

			m.peers[conn.RemoteAddr().String()] = newMetalBondPeer(
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

// Shutdown stops the MetalBond server.
func (m *MetalBond) Shutdown() {
	m.log().Infof("Shutting down MetalBond...")
	m.shuttingDown = true
	if m.lis != nil {
		(*m.lis).Close()
	}

	for p := range m.peers {
		m.RemovePeer(p)
	}
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
