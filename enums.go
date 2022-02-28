package metalbond

type ConnectionDirection uint8

const (
	INCOMING ConnectionDirection = iota
	OUTGOING
)

type ConnectionState uint8

const (
	CONNECT ConnectionState = iota
	HELLO_SENT
	HELLO_CONFIRMED
	ESTABLISHED
	RETRY
)
