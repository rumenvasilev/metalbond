package metalbond

import (
	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
)

type ServerConfig struct {
	ListenAddress string
	NodeUUID      uuid.UUID
	Hostname      string
}

func StartServer(c ServerConfig) error {
	log.Infof("starting server...")

	StartTCPServer(c)

	return nil
}
