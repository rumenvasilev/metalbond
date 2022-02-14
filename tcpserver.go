package metalbond

import (
	"net"

	"github.com/onmetal/metalbond/pb"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

type MESSAGE_TYPE uint8

const (
	HELLO       MESSAGE_TYPE = 0
	KEEPALIVE                = 1
	SUBSCRIBE                = 2
	UNSUBSCRIBE              = 3
	UPDATE                   = 4
)

func StartTCPServer(c ServerConfig) error {
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

		go handleClientConnection(conn)
	}
}

func handleClientConnection(conn net.Conn) {
	buf := make([]byte, 65535)

	// TODO: read full packets!!!!
	reqLen, err := conn.Read(buf)
	if err != nil {
		log.Errorf("Error reading from socket: %v", err)
	}

	log.Infof("Received %d bytes", reqLen)

	pktVersion := buf[0]
	switch pktVersion {
	case 1:
		pktLen := uint16(buf[1])<<8 + uint16(buf[2])
		pktType := MESSAGE_TYPE(buf[3])
		pktBytes := buf[4 : pktLen+4]

		switch pktType {
		case HELLO:
			log.Infof("HELLO message received")
			msg := &pb.Hello{}
			if err := proto.Unmarshal(pktBytes, msg); err != nil {
				log.Errorf("Cannot unmarshal received packet. Closing connection: %v", err)
				return
			}
			log.Infof("Content: %v", msg)

		case KEEPALIVE:
			log.Infof("KEEPALIVE message received")
			// NO CONTENT

		case SUBSCRIBE:
			log.Infof("SUBSCRIBE message received")
			msg := &pb.Subscription{}
			if err := proto.Unmarshal(pktBytes, msg); err != nil {
				log.Errorf("Cannot unmarshal received packet. Closing connection: %v", err)
				return
			}
			log.Infof("Content: %v", msg)

		case UNSUBSCRIBE:
			log.Infof("UNSUBSCRIBE message received")
			msg := &pb.Subscription{}
			if err := proto.Unmarshal(pktBytes, msg); err != nil {
				log.Errorf("Cannot unmarshal received packet. Closing connection: %v", err)
				return
			}
			log.Infof("Content: %v", msg)

		case UPDATE:
			log.Infof("UPDATE message received")
			msg := &pb.Update{}
			if err := proto.Unmarshal(pktBytes, msg); err != nil {
				log.Errorf("Cannot unmarshal received packet. Closing connection: %v", err)
				return
			}
			log.Infof("Content: %v", msg)

		default:
			log.Errorf("Unknown Packet received. Closing connection.")
			return
		}
	default:
		log.Errorf("Incompatible Client version. Closing connection.")
		conn.Close()
		return
	}
}
