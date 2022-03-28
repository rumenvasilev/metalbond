package metalbond

import (
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/onmetal/metalbond/pb"
	"github.com/sirupsen/logrus"
	log "github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type MetalBondPeer struct {
	PeerType            PeerType
	conn                net.Conn
	remoteAddr          string
	txChan              chan []byte
	direction           ConnectionDirection
	state               ConnectionState
	stateLock           sync.RWMutex
	database            *MetalBondDatabase
	keepaliveInterval   uint32
	keepaliveTimer      *time.Timer
	mySubscriptions     map[uint32]bool
	mySubscriptionsLock sync.Mutex
}

func NewMetalBondPeer(
	pconn *net.Conn,
	remoteAddr string,
	keepaliveInterval uint32,
	direction ConnectionDirection,
	database *MetalBondDatabase) *MetalBondPeer {

	// outgoing connections still need to be established. pconn is nil.
	for pconn == nil {
		conn, err := net.Dial("tcp", remoteAddr)
		if err != nil {
			retryInterval := time.Duration(5 * time.Second)
			log.Warningf("Cannot connect to server - %v - retry in %v", err, retryInterval)
			time.Sleep(retryInterval)
			continue
		}

		pconn = &conn

		log.WithField("peer", remoteAddr).Infof("Connected")
	}

	peer := MetalBondPeer{
		conn:              *pconn,
		remoteAddr:        remoteAddr,
		direction:         direction,
		state:             CONNECTING,
		keepaliveInterval: keepaliveInterval,
		database:          database,
	}

	go peer.Handle()

	return &peer
}

func (p *MetalBondPeer) GetState() ConnectionState {
	p.stateLock.RLock()
	state := p.state
	p.stateLock.RUnlock()
	return state
}

func (p *MetalBondPeer) setState(state ConnectionState) {
	p.stateLock.Lock()
	p.state = state
	p.stateLock.Unlock()
}

func (p *MetalBondPeer) log() *logrus.Entry {
	var state string
	switch p.GetState() {
	case HELLO_RECEIVED:
		state = "HELLO_RECEIVED"
	case HELLO_SENT:
		state = "HELLO_SENT"
	case ESTABLISHED:
		state = "ESTABLISHED"
	case RETRY:
		state = "RETRY"
	case CLOSED:
		state = "CLOSED"
	default:
		state = "INVALID"
	}

	return log.WithField("peer", p.remoteAddr).WithField("state", state)
}

func (p *MetalBondPeer) Handle() {
	p.txChan = make(chan []byte)
	go p.rxLoop()
	go p.txLoop()

	if p.direction == OUTGOING {
		helloMsg := pb.Hello{
			//IsReflector:       p.database.Reflector,
			KeepaliveInterval: p.keepaliveInterval,
		}

		p.sendMessage(HELLO, &helloMsg)
		p.setState(HELLO_SENT)
	}

	p.log().Infof("Peer connected")
}

func (p *MetalBondPeer) Close() {
	if p.GetState() == CLOSED {
		p.log().Errorf("Connection Close() called twice.")
		return
	}

	p.log().Infof("Closing peer connection")
	p.setState(CLOSED)

	// Sending zero length msg to txchan to indicate connection termination
	// txLoop will close connection, rxLoop will notice that.
	p.txChan <- []byte{}

	// If the connection was OUTGOING, try to reconnect!
	if p.direction == OUTGOING {
		p.log().Infof("Trying to reconnect...")

		// TODO: Potential memory leak. This memory address is maintained nowhere. It's a zombie thread, doing stuff.
		NewMetalBondPeer(nil, p.remoteAddr, p.keepaliveInterval, OUTGOING, p.database)
	}
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
			p.log().Errorf("Could not transmit message completely: %v", err)
			p.Close()
		}
	}
}

func (p *MetalBondPeer) startKeepaliveTimer() {
	timeout := time.Duration(p.keepaliveInterval) * time.Second * 5 / 2
	p.log().Infof("Timeout duration: %v", timeout)
	p.keepaliveTimer = time.NewTimer(timeout)
	go func() {
		<-p.keepaliveTimer.C
		if p.GetState() != CLOSED {
			p.log().Infof("Connection timed out. Closing.")
			p.Close()
		}
	}()

	if p.direction == OUTGOING {
		interval := time.Duration(p.keepaliveInterval) * time.Second
		p.log().Infof("Starting KEEPALIVE interval sender (%v)", interval)
		for {
			state := p.GetState()
			switch state {
			case HELLO_RECEIVED:
				p.sendMessage(KEEPALIVE, nil)
			case ESTABLISHED:
				p.sendMessage(KEEPALIVE, nil)
			default:
				p.log().Debugf("Shutting down keepalive timer as peer is in wrong state (%v).", state)
				return
			}

			time.Sleep(interval)
		}
	}
}

func (p *MetalBondPeer) resetKeepaliveTimeout() {
	p.keepaliveTimer.Reset(time.Duration(p.keepaliveInterval) * time.Second * 5 / 2)
}

func (p *MetalBondPeer) rxLoop() {
	buf := make([]byte, 65535)

	for {
		// TODO: read full packets!!!!
		bytesRead, err := p.conn.Read(buf)
		if err != nil {
			if err == io.EOF {
				p.log().Debugf("Client has disconnected.")
				p.Close()
				return
			}
			log.Errorf("Error reading from socket: %v", err)
			p.Close()
			return
		}
		if bytesRead >= len(buf) {
			p.log().Warningf("too many messages in inbound queue. Closing connection.")
			p.Close()
		}

		//p.log().Debugf("Received %d bytes", bytesRead)

		pktStart := 0

	parsePacket:
		pktVersion := buf[pktStart]
		switch pktVersion {
		case 1:
			pktLen := uint16(buf[pktStart+1])<<8 + uint16(buf[pktStart+2])
			pktType := MESSAGE_TYPE(buf[pktStart+3])
			pktBytes := buf[pktStart+4 : pktStart+int(pktLen)+4]

			pktStart += 4 + int(pktLen)

			switch pktType {
			case HELLO:
				hello := &pb.Hello{}
				if err := proto.Unmarshal(pktBytes, hello); err != nil {
					log.Errorf("Cannot unmarshal received packet. Closing connection: %v", err)
					return
				}

				// Use lower Keepalive interval of both client and server as peer config
				keepaliveInterval := p.keepaliveInterval
				if hello.KeepaliveInterval < keepaliveInterval {
					keepaliveInterval = hello.KeepaliveInterval
				}
				p.keepaliveInterval = keepaliveInterval

				// state transition to HELLO_RECEIVED
				p.setState(HELLO_RECEIVED)
				p.log().Infof("HELLO message received")

				// if direction incoming, send HELLO response
				if p.direction == INCOMING {
					helloMsg := pb.Hello{
						//IsReflector:       p.database.Reflector,
						KeepaliveInterval: p.keepaliveInterval,
					}

					p.sendMessage(HELLO, &helloMsg)
					p.setState(HELLO_SENT)
					p.log().Infof("HELLO message sent")
				}

				// start keepalive timer so keepalive messages are sent by the client
				// and client and server check if regular keepalive messages are received.
				go p.startKeepaliveTimer()

			case KEEPALIVE:
				p.log().Debugf("KEEPALIVE message received")

				if p.direction == INCOMING && p.GetState() == HELLO_SENT {
					p.setState(ESTABLISHED)
				} else if p.direction == OUTGOING && p.GetState() == HELLO_RECEIVED {
					p.setState(ESTABLISHED)
				} else if p.GetState() == ESTABLISHED {
					// all good
				} else {
					p.log().Errorf("Received KEEPALIVE while in wrong state. Closing connection.")
					p.Close()
				}

				p.resetKeepaliveTimeout()

				// The server must respond incoming KEEPALIVE messages with an own KEEPALIVE message.
				if p.direction == INCOMING {
					p.sendMessage(KEEPALIVE, nil)
				}

			// TODO: implement received SUBSCRIBE message
			case SUBSCRIBE:
				log.Infof("SUBSCRIBE message received")
				msg := &pb.Subscription{}
				if err := proto.Unmarshal(pktBytes, msg); err != nil {
					log.Errorf("Cannot unmarshal received packet. Closing connection: %v", err)
					return
				}
				log.Infof("Content: %v", msg)

			// TODO: implement received UNSUBSCRIBE message
			case UNSUBSCRIBE:
				log.Infof("UNSUBSCRIBE message received")
				msg := &pb.Subscription{}
				if err := proto.Unmarshal(pktBytes, msg); err != nil {
					log.Errorf("Cannot unmarshal received packet. Closing connection: %v", err)
					return
				}
				log.Infof("Content: %v", msg)

			// TODO: implement received UPDATE message
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
			p.Close()
			return
		}

		if pktStart < bytesRead {
			goto parsePacket
		}
	}
}

func (p *MetalBondPeer) sendMessage(msgType MESSAGE_TYPE, msg protoreflect.ProtoMessage) error {
	//p.log().Debugf("Sending %v message...", msg.ProtoReflect().Type().Descriptor().Name())
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

func (p *MetalBondPeer) Subscribe(vni uint32) error {
	if p.direction == INCOMING {
		return fmt.Errorf("Cannot subscribe on incoming connection")
	}

	p.mySubscriptionsLock.Lock()
	defer p.mySubscriptionsLock.Unlock()
	if _, exists := p.mySubscriptions[vni]; exists {
		return fmt.Errorf("Already subscribed")
	}

	p.mySubscriptions[vni] = true

	msg := pb.Subscription{
		Vni: vni,
	}

	return p.sendMessage(SUBSCRIBE, &msg)
}

func (p *MetalBondPeer) Unsubscribe(vni uint32) error {
	if p.direction == INCOMING {
		return fmt.Errorf("Cannot unsubscribe on incoming connection")
	}

	p.mySubscriptionsLock.Lock()
	defer p.mySubscriptionsLock.Unlock()
	if _, exists := p.mySubscriptions[vni]; !exists {
		return fmt.Errorf("Not subscribed to table %d", vni)
	}

	delete(p.mySubscriptions, vni)

	msg := pb.Subscription{
		Vni: vni,
	}

	return p.sendMessage(UNSUBSCRIBE, &msg)
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
