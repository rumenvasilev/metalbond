package metalbond

import (
	"fmt"
	"io"
	"net"
	"time"

	"github.com/onmetal/metalbond/pb"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type MetalBondPeer struct {
	PeerType          PeerType
	conn              net.Conn
	txChan            chan []byte
	direction         ConnectionDirection
	State             ConnectionState
	database          *MetalBondDatabase
	keepaliveInterval uint32
}

func NewMetalBondPeer(
	conn net.Conn,
	direction ConnectionDirection,
	database *MetalBondDatabase) *MetalBondPeer {

	peer := MetalBondPeer{
		conn:      conn,
		direction: INCOMING,
		State:     CONNECTING,
		database:  database,
	}

	go peer.Handle()

	return &peer
}

func (p *MetalBondPeer) txLoop() {
	for {
		msg := <-p.txChan

		// msg length is zero when peer connection should be terminated
		if len(msg) == 0 {
			p.conn.Close()
			return
		}

		n, err := p.conn.Write(msg)
		if n != len(msg) || err != nil {
			log.Errorf("Could not transmit message completely: %v", err)
			p.Close()
		}
	}
}

func (p *MetalBondPeer) keepaliveTimer() {
	for {
		switch p.State {
		case ESTABLISHED:
			p.sendMessage(KEEPALIVE, nil)
		case CLOSED:
			return
		}

		time.Sleep(time.Duration(p.keepaliveInterval) * time.Second)
	}
}

func (p *MetalBondPeer) resetKeepaliveTimeout() {
	// TODO implement
}

func (p *MetalBondPeer) rxLoop() {
	buf := make([]byte, 65535)

	for {
		// TODO: read full packets!!!!
		bytesRead, err := p.conn.Read(buf)
		if err != nil {
			if err == io.EOF {
				log.Debugf("Client has disconnected.")
				p.Close()
				return
			}
			log.Errorf("Error reading from socket: %v", err)
			p.Close()
			return
		}

		log.Debugf("Received %d bytes", bytesRead)

		pktStart := 0

	parsePacket:
		log.Debugf("pktStart: %v", pktStart)
		pktVersion := buf[pktStart]
		switch pktVersion {
		case 1:
			pktLen := uint16(buf[pktStart+1])<<8 + uint16(buf[pktStart+2])
			pktType := MESSAGE_TYPE(buf[pktStart+3])
			pktBytes := buf[pktStart+4 : pktStart+int(pktLen)+4]

			pktStart += 4 + int(pktLen)

			switch pktType {
			case HELLO:
				log.Infof("HELLO message received")
				hello := &pb.Hello{}
				if err := proto.Unmarshal(pktBytes, hello); err != nil {
					log.Errorf("Cannot unmarshal received packet. Closing connection: %v", err)
					return
				}

				keepaliveInterval := p.database.KeepaliveInterval
				if hello.KeepaliveInterval < keepaliveInterval {
					keepaliveInterval = hello.KeepaliveInterval
				}
				p.keepaliveInterval = keepaliveInterval

			case KEEPALIVE:
				log.Infof("KEEPALIVE message received")
				if p.direction == INCOMING {
					p.sendMessage(KEEPALIVE, nil)
				}
				p.resetKeepaliveTimeout()

			case SUBSCRIBE:
				log.Infof("SUBSCRIBE message received")
				msg := &pb.Subscription{}
				if err := proto.Unmarshal(pktBytes, msg); err != nil {
					log.Errorf("Cannot unmarshal received packet. Closing connection: %v", err)
					return
				}
				log.Infof("Content: %v", msg)

				// TODO: Trigger route updates

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
			peer.Close()
			return
		}

		if pktStart < bytesRead {
			goto parsePacket
		}
	}
}

func (p *MetalBondPeer) sendMessage(msgType MESSAGE_TYPE, msg protoreflect.ProtoMessage) error {
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

	p.txChan <- pkt

	return nil
}

func (p *MetalBondPeer) Handle() {
	go p.rxLoop()
	go p.txLoop()

	helloMsg := pb.Hello{
		NodeId:            p.database.NodeUUID[:],
		Hostname:          p.database.Hostname,
		IsReflector:       p.database.Reflector,
		KeepaliveInterval: 5,
	}

	p.sendMessage(HELLO, &helloMsg)

	log := log.WithField("peer", p.conn.RemoteAddr())
	log.Infof("Peer connected")
}

func (p *MetalBondPeer) Close() {
	log.Infof("Closing peer connection to %v", p.conn.RemoteAddr())
	p.txChan <- []byte{} // Sending zero length msg to txchan to indicate connection termination
}

func (p *MetalBondPeer) SendUpdate(r RouteUpdate) error {
	msg := []byte{}
	p.txChan <- msg
	return nil
}

func (p *MetalBondPeer) processReceivedUpdate(msg pb.Update) error {
	var action UpdateAction
	switch msg.Action {
	case pb.Action_ADD:
		action = ADD
	case pb.Action_REMOVE:
		action = REMOVE
	default:
		return fmt.Errorf("invalid update action")
	}

	var ipversion IPVersion
	switch msg.Destination.IpVersion {
	case pb.IPVersion_IPv4:
		ipversion = IPV4
	case pb.IPVersion_IPv6:
		ipversion = IPV6
	default:
		return fmt.Errorf("invalid IP version")
	}

	if len(msg.Destination.Prefix) != 16 {
		return fmt.Errorf("Invalid Prefix. Must be 16 bytes long! Also for IPv4!")
	}
	if len(msg.NextHop.TargetAddress) != 16 {
		return fmt.Errorf("Invalid Prefix. Must be 16 bytes long! Also for IPv4!")
	}

	upd := RouteUpdate{
		ReceivedFrom: p,
		Action:       action,
		VNI:          VNI(msg.Vni),
		Destination: Destination{
			IPVersion:    ipversion,
			PrefixLength: uint8(msg.Destination.PrefixLength),
		},
		NextHop: NextHop{
			TargetVNI:        msg.NextHop.TargetVNI,
			NAT:              msg.NextHop.Nat,
			NATPortRangeFrom: uint16(msg.NextHop.NatPortRangeFrom),
			NATPortRangeTo:   uint16(msg.NextHop.NatPortRangeTo),
		},
	}
	copy(upd.Destination.Prefix[:], msg.Destination.Prefix[:16])
	copy(upd.NextHop.TargetAddress[:], msg.NextHop.TargetAddress[:16])

	return p.database.Update(upd)
}

func (p *MetalBondPeer) ProcessProtoSubscribeMsg(msg pb.Update, receivedFrom *MetalBondPeer) error {
	return nil
}
