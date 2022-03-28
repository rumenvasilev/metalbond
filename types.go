package metalbond

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
	IPV4 = 4
	IPV6 = 6
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
