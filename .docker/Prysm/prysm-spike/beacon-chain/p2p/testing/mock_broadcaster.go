package testing

import (
	"context"

	"github.com/gogo/protobuf/proto"
	ethpb "github.com/prysmaticlabs/ethereumapis/eth/v1alpha1"
)

// MockBroadcaster implements p2p.Broadcaster for testing.
type MockBroadcaster struct {
	BroadcastCalled bool
}

// Broadcast records a broadcast occurred.
func (m *MockBroadcaster) Broadcast(context.Context, proto.Message) error {
	m.BroadcastCalled = true
	return nil
}

// BroadcastAttestation records a broadcast occurred.
func (m *MockBroadcaster) BroadcastAttestation(_ context.Context, _ uint64, _ *ethpb.Attestation) error {
	m.BroadcastCalled = true
	return nil
}
