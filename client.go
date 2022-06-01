package metalbond

type MetalBondClient interface {
	AddRoute(vni VNI, dest Destination, nexthop NextHop) error
	RemoveRoute(vni VNI, dest Destination, nexthop NextHop) error
}

type DummyClient struct{}

func NewDummyClient() *DummyClient {
	return &DummyClient{}
}

func (c DummyClient) AddRoute(vni VNI, dest Destination, nexthop NextHop) error {
	return nil
}

func (c DummyClient) RemoveRoute(vni VNI, dest Destination, nexthop NextHop) error {
	return nil
}
