package metalbond

import (
	"fmt"
	"net"

	"github.com/onmetal/metalbond/pb"
	"google.golang.org/protobuf/proto"
)

/////////////////////////////////////////////////////////////
//                           TYPES                         //
/////////////////////////////////////////////////////////////

type VNI uint32
type Destination struct {
	IPVersion    IPVersion
	Prefix       [16]byte
	PrefixLength uint8
}

type NextHop struct {
	TargetAddress [16]byte
	TargetVNI     uint32

	NAT              bool
	NATPortRangeFrom uint16
	NATPortRangeTo   uint16
}

type Route struct {
	Destination Destination
	NextHops    []NextHop
}

type RouteUpdate struct {
	Action       UpdateAction
	VNI          VNI
	Destination  Destination
	NextHop      NextHop
	ReceivedFrom *MetalBondPeer
}

/////////////////////////////////////////////////////////////
//                           ENUMS                         //
/////////////////////////////////////////////////////////////

type IPVersion uint8

const (
	IPV4 IPVersion = 4
	IPV6 IPVersion = 6
)

type ConnectionDirection uint8

const (
	INCOMING ConnectionDirection = iota
	OUTGOING
)

type ConnectionState uint8

const (
	CONNECTING ConnectionState = iota
	HELLO_SENT
	HELLO_RECEIVED
	ESTABLISHED
	RETRY
	CLOSED
)

type MESSAGE_TYPE uint8

const (
	HELLO       MESSAGE_TYPE = 1
	KEEPALIVE                = 2
	SUBSCRIBE                = 3
	UNSUBSCRIBE              = 4
	UPDATE                   = 5
)

type UpdateAction uint8

const (
	ADD UpdateAction = iota
	REMOVE
)

type message interface {
	Serialize() ([]byte, error)
}

type msgHello struct {
	message
	KeepaliveInterval uint32
}

func (m msgHello) Serialize() ([]byte, error) {
	pbmsg := pb.Hello{
		KeepaliveInterval: m.KeepaliveInterval,
	}

	msgBytes, err := proto.Marshal(&pbmsg)
	if err != nil {
		return nil, fmt.Errorf("Could not marshal message: %v", err)
	}

	if len(msgBytes) > 1188 {
		return nil, fmt.Errorf("Message too long: %d bytes > maximum of 1188 bytes", len(msgBytes))
	}

	return msgBytes, nil
}

type msgKeepalive struct {
	message
}

func (msg msgKeepalive) Serialize() ([]byte, error) {
	return []byte{}, nil
}

type msgSubscribe struct {
	message
	VNI uint32
}

func (msg msgSubscribe) Serialize() ([]byte, error) {
	return []byte{}, nil
}

type msgUnsubscribe struct {
	message
	VNI uint32
}

func (msg msgUnsubscribe) Serialize() ([]byte, error) {
	return []byte{}, nil
}

type msgUpdate struct {
	message
	Action      UpdateAction
	VNI         uint32
	Destination msgUpdateDestination
	NextHop     msgUpdateNextHop
}

func (msg msgUpdate) Serialize() ([]byte, error) {
	return []byte{}, nil
}

type msgUpdateDestination struct {
	IPVersion    IPVersion
	Prefix       net.IP
	PrefixLength uint8
}

type msgUpdateNextHop struct {
	TargetAddress    net.IP
	TargetVNI        uint32
	NAT              bool
	NATPortRangeFrom uint16
	NATPortRangeTo   uint16
}

func DeserializeHelloMsg(pktBytes []byte) (*msgHello, error) {
	pbmsg := &pb.Hello{}
	if err := proto.Unmarshal(pktBytes, pbmsg); err != nil {
		return nil, fmt.Errorf("Cannot unmarshal received packet. Closing connection: %v", err)
	}

	return &msgHello{
		KeepaliveInterval: pbmsg.KeepaliveInterval,
	}, nil
}

func DeserializeSubscribeMsg(pktBytes []byte) (*msgSubscribe, error) {
	pbmsg := &pb.Subscription{}
	if err := proto.Unmarshal(pktBytes, pbmsg); err != nil {
		return nil, fmt.Errorf("Cannot unmarshal received packet. Closing connection: %v", err)
	}

	return &msgSubscribe{
		VNI: pbmsg.Vni,
	}, nil
}

func DeserializeUnsubscribeMsg(pktBytes []byte) (*msgUnsubscribe, error) {
	pbmsg := &pb.Subscription{}
	if err := proto.Unmarshal(pktBytes, pbmsg); err != nil {
		return nil, fmt.Errorf("Cannot unmarshal received packet. Closing connection: %v", err)
	}

	return &msgUnsubscribe{
		VNI: pbmsg.Vni,
	}, nil
}

func DeserializeUpdateMsg(pktBytes []byte) (*msgUpdate, error) {
	pbmsg := &pb.Update{}
	if err := proto.Unmarshal(pktBytes, pbmsg); err != nil {
		return nil, fmt.Errorf("Cannot unmarshal received packet. Closing connection: %v", err)
	}

	action := ADD
	if pbmsg.Action == pb.Action_REMOVE {
		action = REMOVE
	}

	ipversion := IPV6
	if pbmsg.Destination.IpVersion == pb.IPVersion_IPv4 {
		ipversion = IPV4
	}

	destination := msgUpdateDestination{
		IPVersion:    ipversion,
		Prefix:       pbmsg.Destination.Prefix[:ipversion],
		PrefixLength: uint8(pbmsg.Destination.PrefixLength),
	}

	nexthop := msgUpdateNextHop{
		TargetAddress:    pbmsg.NextHop.TargetAddress[:ipversion],
		TargetVNI:        pbmsg.NextHop.TargetVNI,
		NAT:              pbmsg.NextHop.Nat,
		NATPortRangeFrom: uint16(pbmsg.NextHop.NatPortRangeFrom),
		NATPortRangeTo:   uint16(pbmsg.NextHop.NatPortRangeTo),
	}

	return &msgUpdate{
		Action:      action,
		VNI:         pbmsg.Vni,
		Destination: destination,
		NextHop:     nexthop,
	}, nil
}
