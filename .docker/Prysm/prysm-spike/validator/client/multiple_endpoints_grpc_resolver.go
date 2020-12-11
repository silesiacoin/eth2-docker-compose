package client

import (
	"strings"

	"google.golang.org/grpc/resolver"
)

// Modification of a default grpc passthrough resolver (google.golang.org/grpc/resolver/passthrough) allowing to use multiple addresses
// in grpc endpoint. Example:
// conn, err := grpc.DialContext(ctx, "127.0.0.1:4000,127.0.0.1:4001", grpc.WithInsecure(), grpc.WithResolvers(&multipleEndpointsGrpcResolverBuilder{}))
// It can be used with any grpc load balancer (pick_first, round_robin). Default is pick_first.
// Round robin can be used by adding the following option:
// grpc.WithDefaultServiceConfig("{\"loadBalancingConfig\":[{\"round_robin\":{}}]}")
type multipleEndpointsGrpcResolverBuilder struct{}

func (*multipleEndpointsGrpcResolverBuilder) Build(target resolver.Target, cc resolver.ClientConn, _ resolver.BuildOptions) (resolver.Resolver, error) {
	r := &multipleEndpointsGrpcResolver{
		target: target,
		cc:     cc,
	}
	r.start()
	return r, nil
}

func (*multipleEndpointsGrpcResolverBuilder) Scheme() string {
	return resolver.GetDefaultScheme()
}

type multipleEndpointsGrpcResolver struct {
	target resolver.Target
	cc     resolver.ClientConn
}

func (r *multipleEndpointsGrpcResolver) start() {
	endpoints := strings.Split(r.target.Endpoint, ",")
	var addrs []resolver.Address
	for _, endpoint := range endpoints {
		addrs = append(addrs, resolver.Address{Addr: endpoint})
	}
	r.cc.UpdateState(resolver.State{Addresses: addrs})
}

func (*multipleEndpointsGrpcResolver) ResolveNow(_ resolver.ResolveNowOptions) {}

func (*multipleEndpointsGrpcResolver) Close() {}
