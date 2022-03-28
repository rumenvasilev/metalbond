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

type MetalBondDatabase struct {
	routeTables      map[VNI]RouteTable
	mtxSubscriptions sync.RWMutex                    // this locks a bit much (all VNIs). We could create a mutex for every VNI instead.
	subscriptions    map[VNI]map[*MetalBondPeer]bool // HashMap of HashSet

	peers             map[string]*MetalBondPeer
	keepaliveInterval uint32
}

func NewMetalBond(keepaliveInterval uint32) *MetalBondDatabase {
	m := MetalBondDatabase{
		keepaliveInterval: keepaliveInterval,
		peers:             map[string]*MetalBondPeer{},
	}

	return &m
}

func (m *MetalBondDatabase) AddPeer(addr string) error {
	if _, exists := m.peers[addr]; exists {
		return fmt.Errorf("Peer already registered")
	}

	m.peers[addr] = NewMetalBondPeer(
		nil,
		addr,
		m.keepaliveInterval,
		OUTGOING,
		m)

	return nil
}

func (m *MetalBondDatabase) StartServer(listenAddress string) error {
	lis, err := net.Listen("tcp", listenAddress)
	if err != nil {
		m.log().Fatalf("Cannot open TCP port: %v", err)
	}
	defer lis.Close()

	m.log().Infof("Listening on %s", listenAddress)

	for {
		conn, err := lis.Accept()
		if err != nil {
			m.log().Errorf("Error accepting incoming connection: %v", err)
		}

		NewMetalBondPeer(
			&conn,
			conn.RemoteAddr().String(),
			m.keepaliveInterval,
			INCOMING,
			m,
		)
	}
}

func (m *MetalBondDatabase) EnableNetlink() error {
	return nil
}

func (m *MetalBondDatabase) log() *logrus.Entry {
	return logrus.WithFields(nil)
}
