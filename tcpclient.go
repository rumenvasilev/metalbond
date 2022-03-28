package metalbond

import (
	"os"
	"os/signal"

	log "github.com/sirupsen/logrus"
)

type ClientConfig struct {
	Servers           []string
	KeepaliveInterval uint32
	Netlink           bool // whether to install routes via netlink into the Linux Kernel
}

func NewClient(c ClientConfig) error {
	log.Infof("starting client...")

	database := MetalBondDatabase{}

	for _, server := range c.Servers {
		NewMetalBondPeer(
			nil,
			server,
			c.KeepaliveInterval,
			OUTGOING,
			&database,
		)
	}

	// Wait for SIGINTs
	cint := make(chan os.Signal, 1)
	signal.Notify(cint, os.Interrupt)
	<-cint

	// TODO implement graceful shutdown
	log.Infof("SIGINT received. Shutting down MetalBond client...")

	return nil
}
