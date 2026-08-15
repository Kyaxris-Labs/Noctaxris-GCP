package pubsub_test

import (
	"context"
	"io"
	"testing"
	"time"

	"cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
)

func TestPubSubGRPCStreamingPull(t *testing.T) {
	pub, sub, ctx, cleanup := startPubSubGRPC(t)
	defer cleanup()
	project := "noctaxris-gcp-local"
	topicName := "projects/" + project + "/topics/stream-t"
	subName := "projects/" + project + "/subscriptions/stream-s"
	if _, err := pub.CreateTopic(ctx, &pubsubpb.Topic{Name: topicName}); err != nil {
		t.Fatal(err)
	}
	if _, err := sub.CreateSubscription(ctx, &pubsubpb.Subscription{
		Name: subName, Topic: topicName, AckDeadlineSeconds: 10,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := pub.Publish(ctx, &pubsubpb.PublishRequest{
		Topic:    topicName,
		Messages: []*pubsubpb.PubsubMessage{{Data: []byte("stream-msg")}},
	}); err != nil {
		t.Fatal(err)
	}

	streamCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	stream, err := sub.StreamingPull(streamCtx)
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Send(&pubsubpb.StreamingPullRequest{
		Subscription:             subName,
		MaxOutstandingMessages:   5,
	}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	var gotAck string
	for time.Now().Before(deadline) {
		resp, err := stream.Recv()
		if err != nil {
			if err == io.EOF || streamCtx.Err() != nil {
				break
			}
			// transient empty until message delivered
			continue
		}
		msgs := resp.GetReceivedMessages()
		if len(msgs) > 0 {
			gotAck = msgs[0].GetAckId()
			break
		}
	}
	if gotAck == "" {
		t.Fatal("expected streaming pull message")
	}
	if err := stream.Send(&pubsubpb.StreamingPullRequest{
		Subscription: subName,
		AckIds:       []string{gotAck},
	}); err != nil && err != io.EOF {
		// stream may already be closing; best-effort ack
		t.Logf("ack send: %v", err)
	}
	cancel()
	_ = stream.CloseSend()
}
