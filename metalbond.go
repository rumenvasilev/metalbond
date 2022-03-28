package metalbond

import (
	"fmt"
	"net"
	"sync"

	"github.com/sirupsen/logrus"
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
	keepaliveInterval uint32
}

func NewMetalBond(keepaliveInterval uint32) *MetalBond {
	m := MetalBond{
		keepaliveInterval: keepaliveInterval,
		peers:             map[string]*MetalBondPeer{},
	}

	return &m
}

func (m *MetalBond) AddPeer(addr string, direction ConnectionDirection) error {
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
	m.log().Infof("Removing peer %s", addr)
	if _, exists := m.peers[addr]; !exists {
		m.log().Errorf("Peer %s does not exist", addr)
		return nil
	}

	m.peers[addr].Close()

	delete(m.peers, addr)
	return nil
}

func (m *MetalBond) StartServer(listenAddress string) error {
	lis, err := net.Listen("tcp", listenAddress)
	if err != nil {
		return fmt.Errorf("Cannot open TCP port: %v", err)
	}

	m.log().Infof("Listening on %s", listenAddress)

	go func() {
		for {
			conn, err := lis.Accept()
			if err != nil {
				m.log().Errorf("Error accepting incoming connection: %v", err)
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

}

func (m *MetalBond) EnableNetlink() error {
	return nil
}

func (m *MetalBond) log() *logrus.Entry {
	return logrus.WithFields(nil)
}
