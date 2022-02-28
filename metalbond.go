package metalbond

import (
	"github.com/google/uuid"
	"github.com/onmetal/metalbond/pb"
	log "github.com/sirupsen/logrus"
)

type ServerConfig struct {
	ListenAddress string
	NodeUUID      uuid.UUID
	Hostname      string
}

type ClientConfig struct {
	Servers  []string
	NodeUUID uuid.UUID
	Hostname string
}

type VNI uint32

type MetalBondClient struct {
	Connections      []MetalBondConnection
	OwnPrefixes      map[VNI][]pb.Route
	ReceivedPrefixes map[VNI][]pb.Route
}

type MetalBondConnection struct {
	PeerAddress string
	TxChan      chan []byte
	Direction   ConnectionDirection
	State       ConnectionState
}

func NewServer(c ServerConfig) error {
	log.Infof("starting server...")

	StartTCPServer(c)

	return nil
}

func NewClient(c ClientConfig) error {
	log.Infof("starting client...")

	for _, server := range c.Servers {
		StartTCPClient(server, c)
	}

	return nil
}
