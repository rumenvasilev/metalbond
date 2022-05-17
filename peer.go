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
	conn       *net.Conn
	remoteAddr string
	direction  ConnectionDirection
	isServer   bool

	state     ConnectionState
	stateLock sync.RWMutex

	metalbond *MetalBond

	keepaliveInterval uint32
	keepaliveTimer    *time.Timer

	shutdown      chan bool
	keepaliveStop chan bool
	txChan        chan []byte
	rxHello       chan msgHello
	rxKeepalive   chan msgKeepalive
	rxSubscribe   chan msgSubscribe
	rxUnsubscribe chan msgUnsubscribe
	rxUpdate      chan msgUpdate
	wg            sync.WaitGroup
}

func NewMetalBondPeer(
	pconn *net.Conn,
	remoteAddr string,
	keepaliveInterval uint32,
	direction ConnectionDirection,
	metalbond *MetalBond) *MetalBondPeer {

	peer := MetalBondPeer{
		conn:              pconn,
		remoteAddr:        remoteAddr,
		direction:         direction,
		state:             CONNECTING,
		keepaliveInterval: keepaliveInterval,
		metalbond:         metalbond,
	}

	go peer.handle()

	return &peer
}

func (p *MetalBondPeer) String() string {
	return fmt.Sprintf("%s", p.remoteAddr)
}

func (p *MetalBondPeer) GetState() ConnectionState {
	p.stateLock.RLock()
	state := p.state
	p.stateLock.RUnlock()
	return state
}

func (p *MetalBondPeer) Subscribe(vni VNI) error {
	if p.direction == INCOMING {
		return fmt.Errorf("Cannot subscribe on incoming connection")
	}
	if p.GetState() != ESTABLISHED {
		return fmt.Errorf("Connection not ESTABLISHED")
	}

	msg := msgSubscribe{
		VNI: vni,
	}

	return p.sendMessage(msg)
}

func (p *MetalBondPeer) Unsubscribe(vni VNI) error {
	if p.direction == INCOMING {
		return fmt.Errorf("Cannot unsubscribe on incoming connection")
	}

	msg := msgUnsubscribe{
		VNI: vni,
	}

	return p.sendMessage(msg)
}

func (p *MetalBondPeer) SendUpdate(upd msgUpdate) error {
	if p.GetState() != ESTABLISHED {
		return fmt.Errorf("Connection not ESTABLISHED")
	}

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

	if state == ESTABLISHED {
		p.metalbond.mtxMySubscriptions.RLock()
		for sub := range p.metalbond.mySubscriptions {
			p.Subscribe(sub)
		}
		p.metalbond.mtxMySubscriptions.RUnlock()

		for _, rt := range p.metalbond.getMyAnnouncements() {
			for dest, hops := range rt.Routes {
				for _, hop := range hops {

					upd := msgUpdate{
						VNI:         rt.VNI,
						Destination: dest,
						NextHop:     hop,
					}

					err := p.SendUpdate(upd)
					if err != nil {
						p.log().Debugf("Could not send update to peer: %v", err)
					}
				}
			}
		}
	}
}

func (p *MetalBondPeer) log() *logrus.Entry {
	return logrus.WithField("peer", p.remoteAddr).WithField("state", p.GetState().String())
}

func (p *MetalBondPeer) handle() {
	p.wg.Add(1)
	defer p.wg.Done()

	p.txChan = make(chan []byte)
	p.shutdown = make(chan bool)
	p.keepaliveStop = make(chan bool)
	p.rxHello = make(chan msgHello)
	p.rxKeepalive = make(chan msgKeepalive)
	p.rxSubscribe = make(chan msgSubscribe)
	p.rxUnsubscribe = make(chan msgUnsubscribe)
	p.rxUpdate = make(chan msgUpdate)

	// outgoing connections still need to be established. pconn is nil.
	for p.conn == nil {
		conn, err := net.Dial("tcp", p.remoteAddr)
		if err != nil {
			retryInterval := time.Duration(5 * time.Second)
			logrus.Infof("Cannot connect to server - %v - retry in %v", err, retryInterval)
			time.Sleep(retryInterval)
			continue
		}

		p.conn = &conn
	}

	go p.rxLoop()
	go p.txLoop()

	if p.direction == OUTGOING {
		helloMsg := msgHello{
			KeepaliveInterval: p.keepaliveInterval,
		}

		p.sendMessage(helloMsg)
		p.setState(HELLO_SENT)
	}

	for {
		select {
		case msg := <-p.rxHello:
			p.log().Debugf("Received HELLO message")
			p.processRxHello(msg)

		case msg := <-p.rxKeepalive:
			p.log().Tracef("Received KEEPALIVE message")
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
		case <-p.shutdown:
			return
		}
	}
}

func (p *MetalBondPeer) rxLoop() {
	p.wg.Add(1)
	defer p.wg.Done()

	buf := make([]byte, 65535)

	for {
		// TODO: read full packets!!!!
		bytesRead, err := (*p.conn).Read(buf)
		if p.GetState() == CLOSED || p.GetState() == RETRY {
			return
		}
		if err != nil {
			if err == io.EOF {
				p.log().Infof("Connection closed by peer")
				go p.Reset()
				return
			}
			logrus.Errorf("Error reading from socket: %v", err)
			go p.Reset()
			return
		}
		if bytesRead >= len(buf) {
			p.log().Warningf("too many messages in inbound queue. Closing connection.")
			go p.Reset()
			return
		}

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
					go p.Reset()
					return
				}

				p.rxHello <- *hello

			case KEEPALIVE:
				p.rxKeepalive <- msgKeepalive{}

			case SUBSCRIBE:
				sub, err := deserializeSubscribeMsg(pktBytes)
				if err != nil {
					p.log().Errorf("Cannot deserialize SUBSCRIBE message: %v", err)
					go p.Reset()
					return
				}

				p.rxSubscribe <- *sub

			case UNSUBSCRIBE:
				sub, err := deserializeUnsubscribeMsg(pktBytes)
				if err != nil {
					p.log().Errorf("Cannot deserialize UNSUBSCRIBE message: %v", err)
					go p.Reset()
					return
				}

				p.rxUnsubscribe <- *sub

			case UPDATE:
				upd, err := deserializeUpdateMsg(pktBytes)
				if err != nil {
					p.log().Errorf("Cannot deserialize UPDATE message: %v", err)
					go p.Reset()
					return
				}
				p.rxUpdate <- *upd

			default:
				p.log().Errorf("Unknown Packet received. Closing connection.")
				return
			}
		default:
			p.log().Errorf("Incompatible Client version. Closing connection.")
			go p.Reset()
			return
		}

		if pktStart < bytesRead {
			goto parsePacket
		}
	}
}

func (p *MetalBondPeer) processRxHello(msg msgHello) {
	// Use lower Keepalive interval of both client and server as peer config
	p.isServer = msg.IsServer
	keepaliveInterval := p.keepaliveInterval
	if msg.KeepaliveInterval < keepaliveInterval {
		keepaliveInterval = msg.KeepaliveInterval
	}
	p.keepaliveInterval = keepaliveInterval

	// state transition to HELLO_RECEIVED
	p.setState(HELLO_RECEIVED)
	p.log().Infof("HELLO message received")

	go p.keepaliveLoop()

	// if direction incoming, send HELLO response
	if p.direction == INCOMING {
		helloMsg := msgHello{
			KeepaliveInterval: p.keepaliveInterval,
			IsServer:          p.metalbond.isServer,
		}

		p.sendMessage(helloMsg)
		p.setState(HELLO_SENT)
		p.log().Infof("HELLO message sent")
	}
}

func (p *MetalBondPeer) processRxKeepalive(msg msgKeepalive) {
	if p.direction == INCOMING && p.GetState() == HELLO_SENT {
		p.log().Infof("Connection established")
		p.setState(ESTABLISHED)
	} else if p.direction == OUTGOING && p.GetState() == HELLO_RECEIVED {
		p.log().Infof("Connection established")
		p.setState(ESTABLISHED)
	} else if p.GetState() == ESTABLISHED {
		// all good
	} else {
		p.log().Errorf("Received KEEPALIVE while in wrong state. Closing connection.")
		go p.Reset()
		return
	}

	p.resetKeepaliveTimeout()

	// The server must respond incoming KEEPALIVE messages with an own KEEPALIVE message.
	if p.direction == INCOMING {
		p.sendMessage(msgKeepalive{})
	}
}

func (p *MetalBondPeer) processRxSubscribe(msg msgSubscribe) {
	p.metalbond.addSubscriber(p, msg.VNI)
}

func (p *MetalBondPeer) processRxUnsubscribe(msg msgUnsubscribe) {
}

func (p *MetalBondPeer) processRxUpdate(msg msgUpdate) {
	switch msg.Action {
	case ADD:
		err := p.metalbond.addReceivedRoute(p, msg.VNI, msg.Destination, msg.NextHop)
		if err != nil {
			p.log().Errorf("Could not process received route UPDATE: %v", err)
		}
	case REMOVE:
		p.log().Errorf("TODO: Implement UPDATE REMOVE!")
	default:
		p.log().Errorf("Received UPDATE message with invalid action type!")
	}
}

func (p *MetalBondPeer) Close() {
	if p.GetState() != CLOSED {
		p.setState(CLOSED)
		p.shutdown <- true
		p.keepaliveStop <- true
		close(p.txChan)
	}
}

func (p *MetalBondPeer) Reset() {
	if p.GetState() == CLOSED {
		return
	}

	switch p.direction {
	case INCOMING:
		p.metalbond.RemovePeer(p.remoteAddr)
	case OUTGOING:
		p.log().Infof("Resetting connection...")
		p.setState(RETRY)
		close(p.txChan)
		p.shutdown <- true
		p.keepaliveStop <- true
		p.wg.Wait()

		p.conn = nil
		p.log().Infof("Closed. Waiting 3s...")

		time.Sleep(time.Duration(3 * time.Second))
		p.setState(CONNECTING)
		p.log().Infof("Reconnecting...")

		go p.handle()
	}
}

func (p *MetalBondPeer) keepaliveLoop() {
	p.wg.Add(1)
	defer p.wg.Done()

	timeout := time.Duration(p.keepaliveInterval) * time.Second * 5 / 2
	p.log().Infof("KEEPALIVE timeout: %v", timeout)
	p.keepaliveTimer = time.NewTimer(timeout)

	interval := time.Duration(p.keepaliveInterval) * time.Second
	tckr := time.NewTicker(interval)

	// Sending initial KEEPALIVE message
	if p.direction == OUTGOING {
		p.sendMessage(msgKeepalive{})
	}

	for {
		select {
		// Ticker triggers sending KEEPALIVE messages
		case <-tckr.C:
			if p.direction == OUTGOING {
				p.sendMessage(msgKeepalive{})
			}

		// Timer detects KEEPALIVE timeouts
		case <-p.keepaliveTimer.C:
			p.log().Infof("Connection timed out. Closing.")
			go p.Reset()

		// keepaliveStop chan delivers message to stop this routine
		case <-p.keepaliveStop:
			p.keepaliveTimer.Stop()
			return
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
		p.log().Tracef("Sending KEEPALIVE message")
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
	p.wg.Add(1)
	defer p.wg.Done()

	for {
		msg, more := <-p.txChan
		if !more {
			p.log().Debugf("Closing TCP connection")
			(*p.conn).Close()
			return
		}

		n, err := (*p.conn).Write(msg)
		if n != len(msg) || err != nil {
			p.log().Errorf("Could not transmit message completely: %v", err)
			go p.Reset()
		}
	}

}
