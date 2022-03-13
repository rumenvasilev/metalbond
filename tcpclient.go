package metalbond

import (
	"fmt"
	"net"
	"os"
	"os/signal"

	"github.com/google/uuid"
	"github.com/onmetal/metalbond/pb"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type ClientConfig struct {
	Servers  []string
	NodeUUID uuid.UUID
	Hostname string
}

func NewClient(c ClientConfig) error {
	log.Infof("starting client...")

	database := MetalBondDatabase{}

	for _, server := range c.Servers {
		StartTCPClient(server, c)

		conn, err := net.Dial("tcp", server)
		if err != nil {
			log.Fatalf("Cannot connect to server %s - %v", server, err)
		}
		defer conn.Close()

		log.Infof("Connected to %s", server)

		NewMetalBondPeer(
			conn,
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

func StartTCPClient(server string, c ClientConfig) {
	log := log.WithField("server", server)
	conn, err := net.Dial("tcp", server)
	if err != nil {
		log.Fatalf("Cannot connect to server %s - %v", server, err)
	}
	defer conn.Close()

	log.Infof("Connected to %s", server)

	helloMsg := pb.Hello{
		NodeId:            c.NodeUUID[:],
		Hostname:          c.Hostname,
		KeepaliveInterval: 5,
		KeepaliveTimeout:  12,
	}

	// Sending HELLO to Server
	log.Debugf("Sending HELLO...")
	if err := sendMessage(HELLO, &helloMsg, conn); err != nil {
		log.Errorf("Could not send HELLO msg: %v", err)
		return
	}

	// Waiting for HELLO from Server
	buf, err := expectMessage(HELLO, conn)
	if err != nil {
		log.Errorf("Did not receive initial HELLO message: %v", err)
		return
	}

	var serverHello pb.Hello
	if err := proto.Unmarshal(buf, &serverHello); err != nil {
		log.Errorf("Cannot unmarshal server's HELLO message: %v", err)
		return
	}
	log = log.WithField("server-uuid", serverHello.NodeId)

	// Sending KEEPALIVE to Server
	log.Debugf("Sending first KEEPALIVE...")
	if err := sendMessage(KEEPALIVE, nil, conn); err != nil {
		log.Errorf("Could not send KEEPALIVE msg: %v", err)
		return
	}

	// Waiting for KEEPALIVE from Server
	_, err = expectMessage(KEEPALIVE, conn)
	if err != nil {
		log.Errorf("Did not receive initial KEEPALIVE message: %v", err)
		return
	}

	return
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
