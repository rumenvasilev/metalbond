package metalbond

import (
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

type MetalBondPeer struct {
	conn       net.Conn
	remoteAddr string
	direction  ConnectionDirection

	state     ConnectionState
	stateLock sync.RWMutex

	database *MetalBondDatabase

	keepaliveInterval uint32
	keepaliveTimer    *time.Timer

	mySubscriptions     map[uint32]bool
	mySubscriptionsLock sync.Mutex

	txChan        chan []byte
	rxHello       chan msgHello
	rxKeepalive   chan msgKeepalive
	rxSubscribe   chan msgSubscribe
	rxUnsubscribe chan msgUnsubscribe
	rxUpdate      chan msgUpdate
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
			logrus.Warningf("Cannot connect to server - %v - retry in %v", err, retryInterval)
			time.Sleep(retryInterval)
			continue
		}

		pconn = &conn

		logrus.WithField("peer", remoteAddr).Infof("Connected")
	}

	peer := MetalBondPeer{
		conn:              *pconn,
		remoteAddr:        remoteAddr,
		direction:         direction,
		state:             CONNECTING,
		keepaliveInterval: keepaliveInterval,
		database:          database,
	}

	go peer.handle()

	return &peer
}

func (p *MetalBondPeer) GetState() ConnectionState {
	p.stateLock.RLock()
	state := p.state
	p.stateLock.RUnlock()
	return state
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

	msg := msgSubscribe{
		VNI: vni,
	}

	return p.sendMessage(msg)
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

	msg := msgUnsubscribe{
		VNI: vni,
	}

	return p.sendMessage(msg)
}

func (p *MetalBondPeer) SendUpdate(upd msgUpdate) error {
	p.sendMessage(upd)
	return nil
}

///////////////////////////////////////////////////////////////////
//            PRIVATE METHODS BELOW                              //
///////////////////////////////////////////////////////////////////

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

	return logrus.WithField("peer", p.remoteAddr).WithField("state", state)
}

func (p *MetalBondPeer) handle() {
	p.txChan = make(chan []byte)
	p.rxHello = make(chan msgHello)
	p.rxKeepalive = make(chan msgKeepalive)
	p.rxSubscribe = make(chan msgSubscribe)
	p.rxUnsubscribe = make(chan msgUnsubscribe)
	p.rxUpdate = make(chan msgUpdate)
	go p.rxLoop()
	go p.txLoop()

	if p.direction == OUTGOING {
		helloMsg := msgHello{
			KeepaliveInterval: p.keepaliveInterval,
		}

		p.sendMessage(helloMsg)
		p.setState(HELLO_SENT)
	}

	p.log().Infof("Peer connected")

	for {
		select {
		case msg := <-p.rxHello:
			p.log().Debugf("Received HELLO message")
			p.processRxHello(msg)

		case msg := <-p.rxKeepalive:
			p.log().Debugf("Received KEEPALIVE message")
			p.processRxKeepalive(msg)

		case msg := <-p.rxSubscribe:
			p.log().Debugf("Received SUBSCRIBE message")
			p.processRxSubscribe(msg)

		case msg := <-p.rxUnsubscribe:
			p.log().Debugf("Received UNSUBSCRIBE message")
			p.processRxUnsubscribe(msg)

		case msg := <-p.rxUpdate:
			p.log().Debugf("Received UPDATE message")
			p.processRxUpdate(msg)
		}
	}
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
			logrus.Errorf("Error reading from socket: %v", err)
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
				hello, err := deserializeHelloMsg(pktBytes)
				if err != nil {
					p.log().Errorf("Cannot deserialize HELLO message: %v", err)
					p.Close()
					return
				}

				p.rxHello <- *hello

			case KEEPALIVE:
				p.rxKeepalive <- msgKeepalive{}

			case SUBSCRIBE:
				sub, err := deserializeSubscribeMsg(pktBytes)
				if err != nil {
					p.log().Errorf("Cannot deserialize SUBSCRIBE message: %v", err)
					p.Close()
					return
				}

				p.rxSubscribe <- *sub

			case UNSUBSCRIBE:
				sub, err := deserializeUnsubscribeMsg(pktBytes)
				if err != nil {
					p.log().Errorf("Cannot deserialize UNSUBSCRIBE message: %v", err)
					p.Close()
					return
				}

				p.rxUnsubscribe <- *sub

			case UPDATE:
				upd, err := deserializeUpdateMsg(pktBytes)
				if err != nil {
					p.log().Errorf("Cannot deserialize UPDATE message: %v", err)
					p.Close()
					return
				}
				p.rxUpdate <- *upd

			default:
				p.log().Errorf("Unknown Packet received. Closing connection.")
				return
			}
		default:
			p.log().Errorf("Incompatible Client version. Closing connection.")
			p.Close()
			return
		}

		if pktStart < bytesRead {
			goto parsePacket
		}
	}
}

func (p *MetalBondPeer) processRxHello(msg msgHello) {
	// Use lower Keepalive interval of both client and server as peer config
	keepaliveInterval := p.keepaliveInterval
	if msg.KeepaliveInterval < keepaliveInterval {
		keepaliveInterval = msg.KeepaliveInterval
	}
	p.keepaliveInterval = keepaliveInterval

	// state transition to HELLO_RECEIVED
	p.setState(HELLO_RECEIVED)
	p.log().Infof("HELLO message received")

	// if direction incoming, send HELLO response
	if p.direction == INCOMING {
		helloMsg := msgHello{
			KeepaliveInterval: p.keepaliveInterval,
		}

		p.sendMessage(helloMsg)
		p.setState(HELLO_SENT)
		p.log().Infof("HELLO message sent")
	}

	// start keepalive timer so keepalive messages are sent by the client
	// and client and server check if regular keepalive messages are received.
	go p.startKeepaliveTimer()
}

func (p *MetalBondPeer) processRxKeepalive(msg msgKeepalive) {
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
		p.sendMessage(msgKeepalive{})
	}
}

func (p *MetalBondPeer) processRxSubscribe(msg msgSubscribe) {
}

func (p *MetalBondPeer) processRxUnsubscribe(msg msgUnsubscribe) {
}

func (p *MetalBondPeer) processRxUpdate(msg msgUpdate) {
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
				p.sendMessage(msgKeepalive{})
			case ESTABLISHED:
				p.sendMessage(msgKeepalive{})
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

func (p *MetalBondPeer) sendMessage(msg message) error {
	var msgType MESSAGE_TYPE
	switch msg.(type) {
	case msgHello:
		msgType = HELLO
		p.log().Debugf("Sending HELLO message")
	case msgKeepalive:
		msgType = KEEPALIVE
		p.log().Debugf("Sending KEEPALIVE message")
	case msgSubscribe:
		msgType = SUBSCRIBE
		p.log().Debugf("Sending SUBSCRIBE message")
	case msgUnsubscribe:
		msgType = UNSUBSCRIBE
		p.log().Debugf("Sending UNSUBSCRIBE message")
	case msgUpdate:
		msgType = UPDATE
		p.log().Debugf("Sending UPDATE message")
	default:
		return fmt.Errorf("Unknown message type")
	}

	msgBytes, err := msg.Serialize()
	if err != nil {
		return fmt.Errorf("Could not serialize message: %v", err)
	}

	hdr := []byte{1, byte(len(msgBytes) >> 8), byte(len(msgBytes) % 256), byte(msgType)}
	pkt := append(hdr, msgBytes...)

	p.txChan <- pkt

	return nil
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
