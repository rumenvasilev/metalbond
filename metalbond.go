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
	Routes map[Destination]NextHop
}

type MetalBond struct {
	routeTables      map[VNI]RouteTable
	mtxSubscriptions sync.RWMutex                    // this locks a bit much (all VNIs). We could create a mutex for every VNI instead.
	subscriptions    map[VNI]map[*MetalBondPeer]bool // HashMap of HashSet

	peers             map[string]*MetalBondPeer
	peerMtx           sync.Mutex
	keepaliveInterval uint32
	shuttingDown      bool

	installRoutes      bool
	tunDevice          netlink.Link
	kernelRouteTableID int

	lis *net.Listener
}

func NewMetalBond(keepaliveInterval uint32) *MetalBond {
	m := MetalBond{
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

func (m *MetalBond) AnnounceRoute(vni uint, dest Destination, hop NextHop) error {
	return nil
}

func (m *MetalBond) StartServer(listenAddress string) error {
	lis, err := net.Listen("tcp", listenAddress)
	m.lis = &lis
	if err != nil {
		return fmt.Errorf("Cannot open TCP port: %v", err)
	}

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
