package metalbond

import (
	"sync"

	"github.com/onmetal/metalbond/pb"
)

// TODO: implement route serialization
func (r *Route) Serialize() ([]byte, error) {
	return nil, nil
}

type RouteTable struct {
	VNI    VNI
	Routes map[Destination]Route
}

type MetalBondDatabase struct {
	routeTables      map[VNI]RouteTable
	mtxSubscriptions sync.RWMutex                    // this locks a bit much (all VNIs). We could create a mutex for every VNI instead.
	subscriptions    map[VNI]map[*MetalBondPeer]bool // HashMap of HashSet
	inboundUpdates   chan RouteUpdate
}

func (db *MetalBondDatabase) Update(r RouteUpdate) error {
	db.inboundUpdates <- r
	return nil
}

func (db *MetalBondDatabase) ProcessProtoSubscribeMsg(msg pb.Subscription, receivedFrom *MetalBondPeer) error {
	db.mtxSubscriptions.Lock()
	db.subscriptions[VNI(msg.GetVni())][receivedFrom] = true
	db.mtxSubscriptions.Unlock()

	return nil
}

func (db *MetalBondDatabase) ProcessProtoUnsubscribeMsg(msg pb.Subscription, receivedFrom *MetalBondPeer) error {
	db.mtxSubscriptions.Lock()
	delete(db.subscriptions[VNI(msg.GetVni())], receivedFrom)
	db.mtxSubscriptions.Unlock()

	return nil
}
