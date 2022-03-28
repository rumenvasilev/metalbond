package metalbond

import (
	"net"

	log "github.com/sirupsen/logrus"
)

type ServerConfig struct {
	ListenAddress     string
	KeepaliveInterval uint32
}

func NewServer(c ServerConfig) error {
	log.Infof("starting server...")

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
			&conn,
			conn.RemoteAddr().String(),
			c.KeepaliveInterval,
			INCOMING,
			&database,
		)
	}
}
