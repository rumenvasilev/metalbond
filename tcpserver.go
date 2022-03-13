package metalbond

import (
	"net"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
)

type ServerConfig struct {
	ListenAddress string
	NodeUUID      uuid.UUID
	Hostname      string
}

func NewServer(c ServerConfig) error {
	log.Infof("starting server...")

	StartTCPServer(c)

	return nil
}

func StartTCPServer(c ServerConfig) error {
	database := MetalBondDatabase{}

	lis, err := net.Listen("tcp", c.ListenAddress)
	if err != nil {
		log.Fatalf("Cannot open TCP port: %v", err)
	}
	defer lis.Close()

	log.Infof("Listening on %s", c.ListenAddress)

	for {
		conn, err := lis.Accept()
		if err != nil {
			log.Errorf("Error accepting incoming connection: %v", err)
		}

		NewMetalBondPeer(
			conn,
			INCOMING,
			&database,
		)
	}
}
