package p2p

import (
	"context"
	"time"

	"github.com/golang/snappy"
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	pubsub_pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/prysmaticlabs/prysm/shared/featureconfig"
	"github.com/prysmaticlabs/prysm/shared/hashutil"
	"github.com/prysmaticlabs/prysm/shared/params"
)

// JoinTopic will join PubSub topic, if not already joined.
func (s *Service) JoinTopic(topic string, opts ...pubsub.TopicOpt) (*pubsub.Topic, error) {
	s.joinedTopicsLock.Lock()
	defer s.joinedTopicsLock.Unlock()

	if _, ok := s.joinedTopics[topic]; !ok {
		topicHandle, err := s.pubsub.Join(topic, opts...)
		if err != nil {
			return nil, err
		}
		s.joinedTopics[topic] = topicHandle
	}

	return s.joinedTopics[topic], nil
}

// LeaveTopic closes topic and removes corresponding handler from list of joined topics.
// This method will return error if there are outstanding event handlers or subscriptions.
func (s *Service) LeaveTopic(topic string) error {
	s.joinedTopicsLock.Lock()
	defer s.joinedTopicsLock.Unlock()

	if t, ok := s.joinedTopics[topic]; ok {
		if err := t.Close(); err != nil {
			return err
		}
		delete(s.joinedTopics, topic)
	}
	return nil
}

// PublishToTopic joins (if necessary) and publishes a message to a PubSub topic.
func (s *Service) PublishToTopic(ctx context.Context, topic string, data []byte, opts ...pubsub.PubOpt) error {
	topicHandle, err := s.JoinTopic(topic)
	if err != nil {
		return err
	}

	// Wait for at least 1 peer to be available to receive the published message.
	for {
		if len(topicHandle.ListPeers()) > 0 {
			return topicHandle.Publish(ctx, data, opts...)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// SubscribeToTopic joins (if necessary) and subscribes to PubSub topic.
func (s *Service) SubscribeToTopic(topic string, opts ...pubsub.SubOpt) (*pubsub.Subscription, error) {
	s.awaitStateInitialized() // Genesis time and genesis validator root are required to subscribe.

	topicHandle, err := s.JoinTopic(topic)
	if err != nil {
		return nil, err
	}
	if featureconfig.Get().EnablePeerScorer {
		scoringParams := topicScoreParams(topic)
		if scoringParams != nil {
			if err = topicHandle.SetScoreParams(scoringParams); err != nil {
				return nil, err
			}
		}
	}
	return topicHandle.Subscribe(opts...)
}

// peerInspector will scrape all the relevant scoring data and add it to our
// peer handler.
// TODO(#6043): Add hooks to add in peer inspector to our global peer handler.
func (s *Service) peerInspector(peerMap map[peer.ID]*pubsub.PeerScoreSnapshot) {
	// no-op
}

// Content addressable ID function.
//
// ETH2 spec defines the message ID as:
//    The `message-id` of a gossipsub message MUST be the following 20 byte value computed from the message data:
//    If `message.data` has a valid snappy decompression, set `message-id` to the first 20 bytes of the `SHA256` hash of
//    the concatenation of `MESSAGE_DOMAIN_VALID_SNAPPY` with the snappy decompressed message data,
//    i.e. `SHA256(MESSAGE_DOMAIN_VALID_SNAPPY + snappy_decompress(message.data))[:20]`.
//
//    Otherwise, set `message-id` to the first 20 bytes of the `SHA256` hash of
//    the concatenation of `MESSAGE_DOMAIN_INVALID_SNAPPY` with the raw message data,
//    i.e. `SHA256(MESSAGE_DOMAIN_INVALID_SNAPPY + message.data)[:20]`.
func msgIDFunction(pmsg *pubsub_pb.Message) string {
	decodedData, err := snappy.Decode(nil /*dst*/, pmsg.Data)
	if err != nil {
		combinedData := append(params.BeaconNetworkConfig().MessageDomainInvalidSnappy[:], pmsg.Data...)
		h := hashutil.Hash(combinedData)
		return string(h[:20])
	}
	combinedData := append(params.BeaconNetworkConfig().MessageDomainValidSnappy[:], decodedData...)
	h := hashutil.Hash(combinedData)
	return string(h[:20])
}

func setPubSubParameters() {
	heartBeatInterval := 700 * time.Millisecond
	pubsub.GossipSubDlo = 6
	pubsub.GossipSubD = 8
	pubsub.GossipSubHeartbeatInterval = heartBeatInterval
	pubsub.GossipSubHistoryLength = 6
	pubsub.GossipSubHistoryGossip = 3
	pubsub.TimeCacheDuration = 550 * heartBeatInterval

	// Set a larger gossip history to ensure that slower
	// messages have a longer time to be propagated. This
	// comes with the tradeoff of larger memory usage and
	// size of the seen message cache.
	if featureconfig.Get().EnableLargerGossipHistory {
		pubsub.GossipSubHistoryLength = 12
		pubsub.GossipSubHistoryLength = 5
	}
}
