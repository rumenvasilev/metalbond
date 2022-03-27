package metalbond

import (
	"fmt"
	"net"
	"os"
	"os/signal"

	"github.com/google/uuid"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type ClientConfig struct {
	Servers           []string
	NodeUUID          uuid.UUID
	Hostname          string
	KeepaliveInterval uint32
}

func NewClient(c ClientConfig) error {
	log.Infof("starting client...")

	database := MetalBondDatabase{
		KeepaliveInterval: c.KeepaliveInterval,
	}

	for _, server := range c.Servers {
		NewMetalBondPeer(
			nil,
			server,
			OUTGOING,
			&database,
		)
	}

	// Wait for SIGINT
	cint := make(chan os.Signal, 1)
	signal.Notify(cint, os.Interrupt)
	<-cint

	// TODO implement graceful shutdown

	return nil
}

func sendMessage(msgType MESSAGE_TYPE, msg protoreflect.ProtoMessage, conn net.Conn) error {
	msgBytes := []byte{}
	var err error
	if msg != nil {
		msgBytes, err = proto.Marshal(msg)
		if err != nil {
			return fmt.Errorf("Could not marshal message: %v", err)
		}
	}

	hdr := []byte{1, byte(len(msgBytes) >> 8), byte(len(msgBytes) % 256), byte(msgType)}
	pkt := append(hdr, msgBytes...)

	n, err := conn.Write(pkt)
	if err != nil {
		return err
	}
	if n != len(pkt) {
		return fmt.Errorf("Could not send message completely (sent %d of %d bytes)", n, len(pkt))
	}

	return nil
}

func expectMessage(msgType MESSAGE_TYPE, conn net.Conn) ([]byte, error) {

	return nil, nil
}
