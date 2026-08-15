package pubsub_test

import (
	"context"
	"net"
	"path/filepath"
	"testing"

	"cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/pubsub"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func startPubSubGRPC(t *testing.T) (pubsubpb.PublisherClient, pubsubpb.SubscriberClient, context.Context, func()) {
	t.Helper()
	dir := t.TempDir()
	key, err := store.LoadOrCreateMasterKey(filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(filepath.Join(dir, "data"), key)
	if err != nil {
		t.Fatal(err)
	}
	project := "noctaxris-gcp-local"
	rootSA := "root@" + project + ".iam.gserviceaccount.com"
	token := "test-root-token"
	if err := st.EnsureRoot(project, rootSA); err != nil {
		t.Fatal(err)
	}
	authn := &authn.Authenticator{RootServiceAccount: rootSA, RootAccessToken: token}
	svc := &pubsub.Service{
		Store: st,
		Authz: &authz.Evaluator{Policies: st},
		Principal: pubsub.PrincipalFromAuthn(authn, nil),
	}
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	gs := grpc.NewServer()
	svc.Register(gs)
	go func() { _ = gs.Serve(lis) }()
	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	cleanup := func() {
		_ = conn.Close()
		gs.Stop()
		_ = st.Close()
	}
	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("authorization", "Bearer "+token))
	return pubsubpb.NewPublisherClient(conn), pubsubpb.NewSubscriberClient(conn), ctx, cleanup
}

func TestPubSubGRPCTopicSubscriptionLifecycle(t *testing.T) {
	pub, sub, ctx, cleanup := startPubSubGRPC(t)
	defer cleanup()
	project := "noctaxris-gcp-local"
	topicName := "projects/" + project + "/topics/grpc-t"
	subName := "projects/" + project + "/subscriptions/grpc-s"

	topic, err := pub.CreateTopic(ctx, &pubsubpb.Topic{Name: topicName, Labels: map[string]string{"k": "v"}})
	if err != nil {
		t.Fatal(err)
	}
	if topic.GetName() != topicName {
		t.Fatalf("topic=%v", topic)
	}
	got, err := pub.GetTopic(ctx, &pubsubpb.GetTopicRequest{Topic: topicName})
	if err != nil || got.GetName() != topicName {
		t.Fatalf("get: %v err=%v", got, err)
	}
	listed, err := pub.ListTopics(ctx, &pubsubpb.ListTopicsRequest{Project: "projects/" + project})
	if err != nil || len(listed.GetTopics()) < 1 {
		t.Fatalf("list: %v err=%v", listed, err)
	}
	updated, err := pub.UpdateTopic(ctx, &pubsubpb.UpdateTopicRequest{
		Topic:      &pubsubpb.Topic{Name: topicName, Labels: map[string]string{"k": "2"}},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"labels"}},
	})
	if err != nil || updated.GetLabels()["k"] != "2" {
		t.Fatalf("update: %v err=%v", updated, err)
	}

	createdSub, err := sub.CreateSubscription(ctx, &pubsubpb.Subscription{
		Name:                  subName,
		Topic:                 topicName,
		AckDeadlineSeconds:    15,
		EnableExactlyOnceDelivery: true,
		Labels:                map[string]string{"s": "1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if createdSub.GetName() != subName {
		t.Fatalf("sub=%v", createdSub)
	}
	gotSub, err := sub.GetSubscription(ctx, &pubsubpb.GetSubscriptionRequest{Subscription: subName})
	if err != nil || gotSub.GetTopic() != topicName {
		t.Fatalf("get sub: %v err=%v", gotSub, err)
	}
	subs, err := sub.ListSubscriptions(ctx, &pubsubpb.ListSubscriptionsRequest{Project: "projects/" + project})
	if err != nil || len(subs.GetSubscriptions()) < 1 {
		t.Fatalf("list subs: %v err=%v", subs, err)
	}

	pubResp, err := pub.Publish(ctx, &pubsubpb.PublishRequest{
		Topic: topicName,
		Messages: []*pubsubpb.PubsubMessage{{
			Data:       []byte("hello-grpc"),
			Attributes: map[string]string{"a": "b"},
		}},
	})
	if err != nil || len(pubResp.GetMessageIds()) != 1 {
		t.Fatalf("publish: %v err=%v", pubResp, err)
	}
	pullResp, err := sub.Pull(ctx, &pubsubpb.PullRequest{Subscription: subName, MaxMessages: 10})
	if err != nil || len(pullResp.GetReceivedMessages()) != 1 {
		t.Fatalf("pull: %v err=%v", pullResp, err)
	}
	ackID := pullResp.GetReceivedMessages()[0].GetAckId()
	_, err = sub.ModifyAckDeadline(ctx, &pubsubpb.ModifyAckDeadlineRequest{
		Subscription:       subName,
		AckIds:             []string{ackID},
		AckDeadlineSeconds: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = sub.Acknowledge(ctx, &pubsubpb.AcknowledgeRequest{Subscription: subName, AckIds: []string{ackID}})
	if err != nil {
		t.Fatal(err)
	}

	snapName := "projects/" + project + "/snapshots/grpc-snap"
	snap, err := sub.CreateSnapshot(ctx, &pubsubpb.CreateSnapshotRequest{
		Name:         snapName,
		Subscription: subName,
		Labels:       map[string]string{"snap": "1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	gotSnap, err := sub.GetSnapshot(ctx, &pubsubpb.GetSnapshotRequest{Snapshot: snapName})
	if err != nil || gotSnap.GetName() != snap.GetName() {
		t.Fatalf("get snap: %v err=%v", gotSnap, err)
	}
	snaps, err := sub.ListSnapshots(ctx, &pubsubpb.ListSnapshotsRequest{Project: "projects/" + project})
	if err != nil || len(snaps.GetSnapshots()) < 1 {
		t.Fatalf("list snaps: %v err=%v", snaps, err)
	}
	_, err = sub.DeleteSnapshot(ctx, &pubsubpb.DeleteSnapshotRequest{Snapshot: snapName})
	if err != nil {
		t.Fatal(err)
	}

	_, err = sub.DeleteSubscription(ctx, &pubsubpb.DeleteSubscriptionRequest{Subscription: subName})
	if err != nil {
		t.Fatal(err)
	}
	_, err = pub.DeleteTopic(ctx, &pubsubpb.DeleteTopicRequest{Topic: topicName})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPubSubGRPCUnauthenticated(t *testing.T) {
	pub, _, _, cleanup := startPubSubGRPC(t)
	defer cleanup()
	_, err := pub.CreateTopic(context.Background(), &pubsubpb.Topic{Name: "projects/noctaxris-gcp-local/topics/x"})
	if err == nil {
		t.Fatal("expected unauthenticated")
	}
}
