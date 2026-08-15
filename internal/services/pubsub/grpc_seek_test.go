package pubsub_test

import (
	"testing"
	"time"

	"cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestPubSubGRPCSeekSnapshotModifyAckPush(t *testing.T) {
	pub, sub, ctx, cleanup := startPubSubGRPC(t)
	defer cleanup()
	project := "noctaxris-gcp-local"
	topicName := "projects/" + project + "/topics/grpc-seek-t"
	subName := "projects/" + project + "/subscriptions/grpc-seek-s"
	snapName := "projects/" + project + "/snapshots/grpc-snap"

	if _, err := pub.CreateTopic(ctx, &pubsubpb.Topic{Name: topicName}); err != nil {
		t.Fatal(err)
	}
	if _, err := sub.CreateSubscription(ctx, &pubsubpb.Subscription{
		Name: subName, Topic: topicName, AckDeadlineSeconds: 10,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := pub.Publish(ctx, &pubsubpb.PublishRequest{
		Topic: topicName,
		Messages: []*pubsubpb.PubsubMessage{{Data: []byte("m1")}, {Data: []byte("m2")}},
	}); err != nil {
		t.Fatal(err)
	}
	pulled, err := sub.Pull(ctx, &pubsubpb.PullRequest{Subscription: subName, MaxMessages: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(pulled.GetReceivedMessages()) < 1 {
		t.Fatalf("pull=%v", pulled)
	}
	ackID := pulled.GetReceivedMessages()[0].GetAckId()
	if _, err := sub.ModifyAckDeadline(ctx, &pubsubpb.ModifyAckDeadlineRequest{
		Subscription:       subName,
		AckIds:             []string{ackID},
		AckDeadlineSeconds: 60,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := sub.Acknowledge(ctx, &pubsubpb.AcknowledgeRequest{
		Subscription: subName, AckIds: []string{ackID},
	}); err != nil {
		t.Fatal(err)
	}

	snap, err := sub.CreateSnapshot(ctx, &pubsubpb.CreateSnapshotRequest{
		Name: snapName, Subscription: subName, Labels: map[string]string{"s": "1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if snap.GetName() != snapName {
		t.Fatalf("snap=%v", snap)
	}
	gotSnap, err := sub.GetSnapshot(ctx, &pubsubpb.GetSnapshotRequest{Snapshot: snapName})
	if err != nil || gotSnap.GetName() != snapName {
		t.Fatalf("get snap: %v err=%v", gotSnap, err)
	}
	listed, err := sub.ListSnapshots(ctx, &pubsubpb.ListSnapshotsRequest{Project: "projects/" + project})
	if err != nil || len(listed.GetSnapshots()) < 1 {
		t.Fatalf("list snaps: %v", listed)
	}

	if _, err := sub.Seek(ctx, &pubsubpb.SeekRequest{
		Subscription: subName,
		Target:       &pubsubpb.SeekRequest_Time{Time: timestamppb.New(time.Now().Add(-time.Hour))},
	}); err != nil {
		t.Fatal(err)
	}

	catcher := "http://127.0.0.1:4588/_noctaxris-gcp/http-catcher/grpc-push"
	if _, err := sub.ModifyPushConfig(ctx, &pubsubpb.ModifyPushConfigRequest{
		Subscription: subName,
		PushConfig:   &pubsubpb.PushConfig{PushEndpoint: catcher},
	}); err != nil {
		t.Fatal(err)
	}
	updated, err := sub.UpdateSubscription(ctx, &pubsubpb.UpdateSubscriptionRequest{
		Subscription: &pubsubpb.Subscription{
			Name:               subName,
			AckDeadlineSeconds: 20,
			Labels:             map[string]string{"s": "2"},
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"ack_deadline_seconds", "labels"}},
	})
	if err != nil || updated.GetAckDeadlineSeconds() != 20 {
		t.Fatalf("update sub: %v err=%v", updated, err)
	}

	if _, err := sub.DeleteSnapshot(ctx, &pubsubpb.DeleteSnapshotRequest{Snapshot: snapName}); err != nil {
		t.Fatal(err)
	}
	if _, err := sub.DeleteSubscription(ctx, &pubsubpb.DeleteSubscriptionRequest{Subscription: subName}); err != nil {
		t.Fatal(err)
	}
	if _, err := pub.DeleteTopic(ctx, &pubsubpb.DeleteTopicRequest{Topic: topicName}); err != nil {
		t.Fatal(err)
	}
}
