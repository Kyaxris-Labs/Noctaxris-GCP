package pubsub

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/httpegress"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// PrincipalResolver resolves a principal for gRPC (interceptor and/or metadata).
type PrincipalResolver func(ctx context.Context) (authn.Principal, error)

// Service implements google.pubsub.v1 Publisher and Subscriber lab methods.
type Service struct {
	pubsubpb.UnimplementedPublisherServer
	pubsubpb.UnimplementedSubscriberServer

	Store      *store.Store
	Authz      *authz.Evaluator
	Principal  PrincipalResolver
	HTTPClient *http.Client
}

// Register attaches Publisher and Subscriber services to gs.
func (s *Service) Register(gs *grpc.Server) {
	pubsubpb.RegisterPublisherServer(gs, s)
	pubsubpb.RegisterSubscriberServer(gs, s)
}

// PrincipalFromAuthn builds a resolver that prefers an existing context principal,
// otherwise parses metadata authorization via Authenticator.
func PrincipalFromAuthn(a *authn.Authenticator, fromCtx func(context.Context) (authn.Principal, bool)) PrincipalResolver {
	return func(ctx context.Context) (authn.Principal, error) {
		if fromCtx != nil {
			if p, ok := fromCtx(ctx); ok {
				return p, nil
			}
		}
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return authn.Principal{}, authn.ErrUnauthenticated
		}
		vals := md.Get("authorization")
		if len(vals) == 0 {
			return authn.Principal{}, authn.ErrUnauthenticated
		}
		raw := vals[0]
		const prefix = "Bearer "
		if !strings.HasPrefix(raw, prefix) {
			return authn.Principal{}, authn.ErrUnauthenticated
		}
		return a.AuthenticateToken(strings.TrimSpace(strings.TrimPrefix(raw, prefix)))
	}
}

func (s *Service) require(ctx context.Context, permission, resource string) error {
	if s.Principal == nil {
		return status.Error(codes.Unauthenticated, "gRPC auth resolver not configured")
	}
	p, err := s.Principal(ctx)
	if err != nil {
		return status.Error(codes.Unauthenticated, "unauthenticated")
	}
	allowed, err := s.Authz.Evaluate(p.Email, p.IsRoot, permission, resource)
	if err != nil {
		return status.Errorf(codes.Internal, "%v", err)
	}
	if !allowed {
		return status.Error(codes.PermissionDenied, "permission denied")
	}
	return nil
}

func projectFromResource(name string) string {
	parts := strings.Split(name, "/")
	if len(parts) >= 2 && parts[0] == "projects" {
		return parts[1]
	}
	return ""
}

func projectResource(projectID string) string {
	return "projects/" + projectID
}

func (s *Service) httpClient() *http.Client {
	if s.HTTPClient != nil {
		return s.HTTPClient
	}
	return httpegress.Client(5 * time.Second)
}

func validatePushEndpoint(push string) error {
	push = strings.TrimSpace(push)
	if push == "" {
		return nil
	}
	return httpegress.Validate(push)
}

func parsePushURL(endpoint string) (*url.URL, error) {
	return url.Parse(endpoint)
}

func (s *Service) CreateTopic(ctx context.Context, topic *pubsubpb.Topic) (*pubsubpb.Topic, error) {
	projectID := projectFromResource(topic.GetName())
	if projectID == "" {
		return nil, gcperrors.GRPC(gcperrors.StatusInvalidArgument, "invalid topic name")
	}
	if err := s.require(ctx, "pubsub.topics.create", projectResource(projectID)); err != nil {
		return nil, err
	}
	t, created, err := s.Store.CreateTopicWithLabels(topic.GetName(), projectID, topic.GetLabels())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	if !created {
		return nil, status.Error(codes.AlreadyExists, "topic already exists")
	}
	return topicPB(t), nil
}

func (s *Service) GetTopic(ctx context.Context, req *pubsubpb.GetTopicRequest) (*pubsubpb.Topic, error) {
	projectID := projectFromResource(req.GetTopic())
	if projectID == "" {
		return nil, gcperrors.GRPC(gcperrors.StatusInvalidArgument, "invalid topic name")
	}
	if err := s.require(ctx, "pubsub.topics.get", projectResource(projectID)); err != nil {
		return nil, err
	}
	t, ok, err := s.Store.GetTopic(req.GetTopic())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	if !ok {
		return nil, status.Error(codes.NotFound, "topic not found")
	}
	return topicPB(t), nil
}

func (s *Service) ListTopics(ctx context.Context, req *pubsubpb.ListTopicsRequest) (*pubsubpb.ListTopicsResponse, error) {
	projectID := strings.TrimPrefix(req.GetProject(), "projects/")
	if projectID == "" || strings.Contains(projectID, "/") {
		return nil, gcperrors.GRPC(gcperrors.StatusInvalidArgument, "invalid project")
	}
	if err := s.require(ctx, "pubsub.topics.list", projectResource(projectID)); err != nil {
		return nil, err
	}
	list, err := s.Store.ListTopics(projectID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	out := &pubsubpb.ListTopicsResponse{}
	for i := range list {
		out.Topics = append(out.Topics, topicPB(&list[i]))
	}
	return out, nil
}

func (s *Service) DeleteTopic(ctx context.Context, req *pubsubpb.DeleteTopicRequest) (*emptypb.Empty, error) {
	projectID := projectFromResource(req.GetTopic())
	if projectID == "" {
		return nil, gcperrors.GRPC(gcperrors.StatusInvalidArgument, "invalid topic name")
	}
	if err := s.require(ctx, "pubsub.topics.delete", projectResource(projectID)); err != nil {
		return nil, err
	}
	ok, err := s.Store.DeleteTopic(req.GetTopic())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	if !ok {
		return nil, status.Error(codes.NotFound, "topic not found")
	}
	return &emptypb.Empty{}, nil
}

func (s *Service) UpdateTopic(ctx context.Context, req *pubsubpb.UpdateTopicRequest) (*pubsubpb.Topic, error) {
	name := req.GetTopic().GetName()
	projectID := projectFromResource(name)
	if projectID == "" {
		return nil, gcperrors.GRPC(gcperrors.StatusInvalidArgument, "invalid topic name")
	}
	if err := s.require(ctx, "pubsub.topics.update", projectResource(projectID)); err != nil {
		return nil, err
	}
	paths := req.GetUpdateMask().GetPaths()
	if len(paths) == 0 {
		return nil, status.Error(codes.InvalidArgument, "update_mask required")
	}
	t, ok, err := s.Store.GetTopic(name)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	if !ok {
		return nil, status.Error(codes.NotFound, "topic not found")
	}
	labels := t.Labels
	for _, p := range paths {
		switch p {
		case "labels":
			labels = req.GetTopic().GetLabels()
			if labels == nil {
				labels = map[string]string{}
			}
		}
	}
	updated, err := s.Store.UpdateTopicLabels(name, labels)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	return topicPB(updated), nil
}

func (s *Service) Publish(ctx context.Context, req *pubsubpb.PublishRequest) (*pubsubpb.PublishResponse, error) {
	projectID := projectFromResource(req.GetTopic())
	if projectID == "" {
		return nil, gcperrors.GRPC(gcperrors.StatusInvalidArgument, "invalid topic name")
	}
	if err := s.require(ctx, "pubsub.topics.publish", projectResource(projectID)); err != nil {
		return nil, err
	}
	if _, ok, err := s.Store.GetTopic(req.GetTopic()); err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	} else if !ok {
		return nil, status.Error(codes.NotFound, "topic not found")
	}
	resp := &pubsubpb.PublishResponse{}
	for _, m := range req.GetMessages() {
		id, copies, err := s.Store.PublishFanout(req.GetTopic(), m.GetData(), m.GetAttributes())
		if err != nil {
			return nil, status.Errorf(codes.Internal, "%v", err)
		}
		s.deliverPush(copies)
		resp.MessageIds = append(resp.MessageIds, id)
	}
	return resp, nil
}

func (s *Service) deliverPush(copies []store.PubSubMessage) {
	for _, m := range copies {
		sub, ok, err := s.Store.GetSubscription(m.Subscription)
		if err != nil || !ok || sub.PushEndpoint == "" {
			continue
		}
		if err := httpegress.Validate(sub.PushEndpoint); err != nil {
			continue
		}
		attrs := map[string]string{}
		if m.AttributesJSON != "" && m.AttributesJSON != "{}" {
			_ = json.Unmarshal([]byte(m.AttributesJSON), &attrs)
		}
		body, _ := json.Marshal(map[string]any{
			"message": map[string]any{
				"data":        base64.StdEncoding.EncodeToString(m.Data),
				"messageId":   m.ID,
				"attributes":  attrs,
				"publishTime": m.PublishTime,
			},
			"subscription": m.Subscription,
		})
		u, err := http.NewRequest(http.MethodPost, sub.PushEndpoint, bytes.NewReader(body))
		if err != nil {
			continue
		}
		// Lab catcher: acknowledge without outbound HTTP (same-process theatre).
		if pu, perr := parsePushURL(sub.PushEndpoint); perr == nil && httpegress.IsLabCatcher(pu, strings.ToLower(pu.Scheme)) {
			store.RecordHTTPCatcher(string(body))
			_ = s.Store.Acknowledge(m.Subscription, []string{m.AckID})
			continue
		}
		u.Header.Set("Content-Type", "application/json")
		resp, err := s.httpClient().Do(u)
		if err != nil {
			continue
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			_ = s.Store.Acknowledge(m.Subscription, []string{m.AckID})
		}
	}
}

func (s *Service) CreateSubscription(ctx context.Context, sub *pubsubpb.Subscription) (*pubsubpb.Subscription, error) {
	projectID := projectFromResource(sub.GetName())
	if projectID == "" {
		return nil, gcperrors.GRPC(gcperrors.StatusInvalidArgument, "invalid subscription name")
	}
	if err := s.require(ctx, "pubsub.subscriptions.create", projectResource(projectID)); err != nil {
		return nil, err
	}
	ack := int(sub.GetAckDeadlineSeconds())
	push := ""
	if sub.GetPushConfig() != nil {
		push = sub.GetPushConfig().GetPushEndpoint()
	}
	if err := validatePushEndpoint(push); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	dlTopic := ""
	maxAttempts := 0
	if dl := sub.GetDeadLetterPolicy(); dl != nil {
		dlTopic = dl.GetDeadLetterTopic()
		maxAttempts = int(dl.GetMaxDeliveryAttempts())
	}
	created, ok, err := s.Store.CreateSubscriptionFull(
		sub.GetName(), sub.GetTopic(), projectID, ack, push, sub.GetLabels(), sub.GetFilter(),
		dlTopic, maxAttempts, sub.GetEnableExactlyOnceDelivery(),
	)
	if err != nil {
		if strings.Contains(err.Error(), "topic not found") || strings.Contains(err.Error(), "dead letter topic not found") {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if strings.Contains(err.Error(), "invalid filter") || strings.Contains(err.Error(), "maxDeliveryAttempts") {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	if !ok {
		return nil, status.Error(codes.AlreadyExists, "subscription already exists")
	}
	return subscriptionPB(created), nil
}

func (s *Service) GetSubscription(ctx context.Context, req *pubsubpb.GetSubscriptionRequest) (*pubsubpb.Subscription, error) {
	projectID := projectFromResource(req.GetSubscription())
	if projectID == "" {
		return nil, gcperrors.GRPC(gcperrors.StatusInvalidArgument, "invalid subscription name")
	}
	if err := s.require(ctx, "pubsub.subscriptions.get", projectResource(projectID)); err != nil {
		return nil, err
	}
	sub, ok, err := s.Store.GetSubscription(req.GetSubscription())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	if !ok {
		return nil, status.Error(codes.NotFound, "subscription not found")
	}
	return subscriptionPB(sub), nil
}

func (s *Service) ListSubscriptions(ctx context.Context, req *pubsubpb.ListSubscriptionsRequest) (*pubsubpb.ListSubscriptionsResponse, error) {
	projectID := strings.TrimPrefix(req.GetProject(), "projects/")
	if projectID == "" || strings.Contains(projectID, "/") {
		return nil, gcperrors.GRPC(gcperrors.StatusInvalidArgument, "invalid project")
	}
	if err := s.require(ctx, "pubsub.subscriptions.list", projectResource(projectID)); err != nil {
		return nil, err
	}
	list, err := s.Store.ListSubscriptions(projectID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	out := &pubsubpb.ListSubscriptionsResponse{}
	for i := range list {
		out.Subscriptions = append(out.Subscriptions, subscriptionPB(&list[i]))
	}
	return out, nil
}

func (s *Service) DeleteSubscription(ctx context.Context, req *pubsubpb.DeleteSubscriptionRequest) (*emptypb.Empty, error) {
	projectID := projectFromResource(req.GetSubscription())
	if projectID == "" {
		return nil, gcperrors.GRPC(gcperrors.StatusInvalidArgument, "invalid subscription name")
	}
	if err := s.require(ctx, "pubsub.subscriptions.delete", projectResource(projectID)); err != nil {
		return nil, err
	}
	ok, err := s.Store.DeleteSubscription(req.GetSubscription())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	if !ok {
		return nil, status.Error(codes.NotFound, "subscription not found")
	}
	return &emptypb.Empty{}, nil
}

func (s *Service) UpdateSubscription(ctx context.Context, req *pubsubpb.UpdateSubscriptionRequest) (*pubsubpb.Subscription, error) {
	name := req.GetSubscription().GetName()
	projectID := projectFromResource(name)
	if projectID == "" {
		return nil, gcperrors.GRPC(gcperrors.StatusInvalidArgument, "invalid subscription name")
	}
	if err := s.require(ctx, "pubsub.subscriptions.update", projectResource(projectID)); err != nil {
		return nil, err
	}
	paths := req.GetUpdateMask().GetPaths()
	if len(paths) == 0 {
		return nil, status.Error(codes.InvalidArgument, "update_mask required")
	}
	var ack *int
	var push *string
	var labels *map[string]string
	var filter *string
	var deadLetter *store.PubSubDeadLetterPolicy
	var enableExactlyOnce *bool
	for _, p := range paths {
		switch p {
		case "ack_deadline_seconds":
			v := int(req.GetSubscription().GetAckDeadlineSeconds())
			ack = &v
		case "push_config", "push_config.push_endpoint":
			ep := ""
			if req.GetSubscription().GetPushConfig() != nil {
				ep = req.GetSubscription().GetPushConfig().GetPushEndpoint()
			}
			if err := validatePushEndpoint(ep); err != nil {
				return nil, status.Error(codes.InvalidArgument, err.Error())
			}
			push = &ep
		case "labels":
			l := req.GetSubscription().GetLabels()
			if l == nil {
				l = map[string]string{}
			}
			labels = &l
		case "filter":
			f := req.GetSubscription().GetFilter()
			filter = &f
		case "dead_letter_policy":
			dl := &store.PubSubDeadLetterPolicy{}
			if pol := req.GetSubscription().GetDeadLetterPolicy(); pol != nil {
				dl.DeadLetterTopic = pol.GetDeadLetterTopic()
				dl.MaxDeliveryAttempts = int(pol.GetMaxDeliveryAttempts())
			}
			deadLetter = dl
		case "enable_exactly_once_delivery":
			v := req.GetSubscription().GetEnableExactlyOnceDelivery()
			enableExactlyOnce = &v
		}
	}
	updated, err := s.Store.UpdateSubscription(name, ack, push, labels, filter, deadLetter, enableExactlyOnce)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, status.Error(codes.NotFound, err.Error())
		}
		if strings.Contains(err.Error(), "invalid filter") || strings.Contains(err.Error(), "maxDeliveryAttempts") {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	return subscriptionPB(updated), nil
}

func (s *Service) Pull(ctx context.Context, req *pubsubpb.PullRequest) (*pubsubpb.PullResponse, error) {
	projectID := projectFromResource(req.GetSubscription())
	if projectID == "" {
		return nil, gcperrors.GRPC(gcperrors.StatusInvalidArgument, "invalid subscription name")
	}
	if err := s.require(ctx, "pubsub.subscriptions.consume", projectResource(projectID)); err != nil {
		return nil, err
	}
	msgs, err := s.Store.Pull(req.GetSubscription(), int(req.GetMaxMessages()))
	if err != nil {
		if strings.Contains(err.Error(), "subscription not found") {
			return nil, status.Error(codes.NotFound, "subscription not found")
		}
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	return pullResponse(msgs), nil
}

func (s *Service) Acknowledge(ctx context.Context, req *pubsubpb.AcknowledgeRequest) (*emptypb.Empty, error) {
	projectID := projectFromResource(req.GetSubscription())
	if projectID == "" {
		return nil, gcperrors.GRPC(gcperrors.StatusInvalidArgument, "invalid subscription name")
	}
	if err := s.require(ctx, "pubsub.subscriptions.consume", projectResource(projectID)); err != nil {
		return nil, err
	}
	if err := s.Store.Acknowledge(req.GetSubscription(), req.GetAckIds()); err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	return &emptypb.Empty{}, nil
}

func (s *Service) ModifyAckDeadline(ctx context.Context, req *pubsubpb.ModifyAckDeadlineRequest) (*emptypb.Empty, error) {
	projectID := projectFromResource(req.GetSubscription())
	if projectID == "" {
		return nil, gcperrors.GRPC(gcperrors.StatusInvalidArgument, "invalid subscription name")
	}
	if err := s.require(ctx, "pubsub.subscriptions.consume", projectResource(projectID)); err != nil {
		return nil, err
	}
	if err := s.Store.ModifyAckDeadline(req.GetSubscription(), req.GetAckIds(), int(req.GetAckDeadlineSeconds())); err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	return &emptypb.Empty{}, nil
}

// StreamingPull delivers messages until the client cancels or the stream ends.
// Client requests may carry ack_ids and modify_deadline_* continuously.
func (s *Service) StreamingPull(stream pubsubpb.Subscriber_StreamingPullServer) error {
	req, err := stream.Recv()
	if err != nil {
		return err
	}
	subName := req.GetSubscription()
	projectID := projectFromResource(subName)
	if projectID == "" {
		return gcperrors.GRPC(gcperrors.StatusInvalidArgument, "invalid subscription name")
	}
	if err := s.require(stream.Context(), "pubsub.subscriptions.consume", projectResource(projectID)); err != nil {
		return err
	}
	max := int(req.GetMaxOutstandingMessages())
	if max <= 0 {
		max = 100
	}
	if err := s.applyStreamingClientRequest(subName, req); err != nil {
		return err
	}

	errCh := make(chan error, 1)
	go func() {
		for {
			next, err := stream.Recv()
			if err != nil {
				errCh <- err
				return
			}
			if err := s.applyStreamingClientRequest(subName, next); err != nil {
				errCh <- err
				return
			}
		}
	}()

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case err := <-errCh:
			if err == io.EOF {
				return nil
			}
			return err
		case <-ticker.C:
			msgs, err := s.Store.Pull(subName, max)
			if err != nil {
				if strings.Contains(err.Error(), "subscription not found") {
					return status.Error(codes.NotFound, "subscription not found")
				}
				return status.Errorf(codes.Internal, "%v", err)
			}
			if len(msgs) == 0 {
				continue
			}
			if err := stream.Send(streamingPullResponse(msgs)); err != nil {
				return err
			}
		}
	}
}

func (s *Service) applyStreamingClientRequest(subName string, req *pubsubpb.StreamingPullRequest) error {
	if len(req.GetAckIds()) > 0 {
		if err := s.Store.Acknowledge(subName, req.GetAckIds()); err != nil {
			return status.Errorf(codes.Internal, "%v", err)
		}
	}
	ackIDs := req.GetModifyDeadlineAckIds()
	secs := req.GetModifyDeadlineSeconds()
	if len(ackIDs) != len(secs) {
		return status.Error(codes.InvalidArgument, "modify_deadline_ack_ids and modify_deadline_seconds length mismatch")
	}
	for i := range ackIDs {
		if err := s.Store.ModifyAckDeadline(subName, []string{ackIDs[i]}, int(secs[i])); err != nil {
			return status.Errorf(codes.Internal, "%v", err)
		}
	}
	return nil
}

func (s *Service) Seek(ctx context.Context, req *pubsubpb.SeekRequest) (*pubsubpb.SeekResponse, error) {
	projectID := projectFromResource(req.GetSubscription())
	if projectID == "" {
		return nil, gcperrors.GRPC(gcperrors.StatusInvalidArgument, "invalid subscription name")
	}
	if err := s.require(ctx, "pubsub.subscriptions.consume", projectResource(projectID)); err != nil {
		return nil, err
	}
	if req.GetSnapshot() != "" {
		return nil, status.Error(codes.InvalidArgument, "seek to snapshot not supported (snapshot CRUD is metadata-only)")
	}
	if req.GetTime() == nil {
		return nil, status.Error(codes.InvalidArgument, "seek time required")
	}
	if err := s.Store.SeekToTime(req.GetSubscription(), req.GetTime().AsTime()); err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil, status.Error(codes.NotFound, "subscription not found")
		}
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	return &pubsubpb.SeekResponse{}, nil
}

func (s *Service) CreateSnapshot(ctx context.Context, req *pubsubpb.CreateSnapshotRequest) (*pubsubpb.Snapshot, error) {
	projectID := projectFromResource(req.GetName())
	if projectID == "" {
		return nil, gcperrors.GRPC(gcperrors.StatusInvalidArgument, "invalid snapshot name")
	}
	if err := s.require(ctx, "pubsub.snapshots.create", projectResource(projectID)); err != nil {
		return nil, err
	}
	snap, created, err := s.Store.CreateSnapshot(req.GetName(), req.GetSubscription(), req.GetLabels())
	if err != nil {
		if strings.Contains(err.Error(), "subscription not found") {
			return nil, status.Error(codes.NotFound, "subscription not found")
		}
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	if !created {
		return nil, status.Error(codes.AlreadyExists, "snapshot already exists")
	}
	return snapshotPB(snap), nil
}

func (s *Service) GetSnapshot(ctx context.Context, req *pubsubpb.GetSnapshotRequest) (*pubsubpb.Snapshot, error) {
	projectID := projectFromResource(req.GetSnapshot())
	if projectID == "" {
		return nil, gcperrors.GRPC(gcperrors.StatusInvalidArgument, "invalid snapshot name")
	}
	if err := s.require(ctx, "pubsub.snapshots.get", projectResource(projectID)); err != nil {
		return nil, err
	}
	snap, ok, err := s.Store.GetSnapshot(req.GetSnapshot())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	if !ok {
		return nil, status.Error(codes.NotFound, "snapshot not found")
	}
	return snapshotPB(snap), nil
}

func (s *Service) ListSnapshots(ctx context.Context, req *pubsubpb.ListSnapshotsRequest) (*pubsubpb.ListSnapshotsResponse, error) {
	projectID := strings.TrimPrefix(req.GetProject(), "projects/")
	if projectID == "" || strings.Contains(projectID, "/") {
		return nil, gcperrors.GRPC(gcperrors.StatusInvalidArgument, "invalid project")
	}
	if err := s.require(ctx, "pubsub.snapshots.list", projectResource(projectID)); err != nil {
		return nil, err
	}
	list, err := s.Store.ListSnapshots(projectID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	out := &pubsubpb.ListSnapshotsResponse{}
	for i := range list {
		out.Snapshots = append(out.Snapshots, snapshotPB(&list[i]))
	}
	return out, nil
}

func (s *Service) DeleteSnapshot(ctx context.Context, req *pubsubpb.DeleteSnapshotRequest) (*emptypb.Empty, error) {
	projectID := projectFromResource(req.GetSnapshot())
	if projectID == "" {
		return nil, gcperrors.GRPC(gcperrors.StatusInvalidArgument, "invalid snapshot name")
	}
	if err := s.require(ctx, "pubsub.snapshots.delete", projectResource(projectID)); err != nil {
		return nil, err
	}
	ok, err := s.Store.DeleteSnapshot(req.GetSnapshot())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	if !ok {
		return nil, status.Error(codes.NotFound, "snapshot not found")
	}
	return &emptypb.Empty{}, nil
}

func pullResponse(msgs []store.PubSubMessage) *pubsubpb.PullResponse {
	out := &pubsubpb.PullResponse{}
	out.ReceivedMessages = receivedMessages(msgs)
	return out
}

func streamingPullResponse(msgs []store.PubSubMessage) *pubsubpb.StreamingPullResponse {
	return &pubsubpb.StreamingPullResponse{ReceivedMessages: receivedMessages(msgs)}
}

func receivedMessages(msgs []store.PubSubMessage) []*pubsubpb.ReceivedMessage {
	out := make([]*pubsubpb.ReceivedMessage, 0, len(msgs))
	for i := range msgs {
		attrs := map[string]string{}
		if msgs[i].AttributesJSON != "" && msgs[i].AttributesJSON != "{}" {
			_ = json.Unmarshal([]byte(msgs[i].AttributesJSON), &attrs)
		}
		pm := &pubsubpb.PubsubMessage{
			Data:       msgs[i].Data,
			Attributes: attrs,
			MessageId:  msgs[i].ID,
		}
		if ts, err := parseRFCTime(msgs[i].PublishTime); err == nil {
			pm.PublishTime = timestamppb.New(ts)
		}
		out = append(out, &pubsubpb.ReceivedMessage{
			AckId:   msgs[i].AckID,
			Message: pm,
		})
	}
	return out
}

func topicPB(t *store.PubSubTopic) *pubsubpb.Topic {
	return &pubsubpb.Topic{Name: t.Name, Labels: t.Labels}
}

func snapshotPB(s *store.PubSubSnapshot) *pubsubpb.Snapshot {
	out := &pubsubpb.Snapshot{Name: s.Name, Topic: s.Topic, Labels: s.Labels}
	if ts, err := parseRFCTime(s.ExpireTime); err == nil {
		out.ExpireTime = timestamppb.New(ts)
	}
	return out
}

func subscriptionPB(sub *store.PubSubSubscription) *pubsubpb.Subscription {
	out := &pubsubpb.Subscription{
		Name:                      sub.Name,
		Topic:                     sub.Topic,
		AckDeadlineSeconds:        int32(sub.AckDeadlineSeconds),
		Labels:                    sub.Labels,
		Filter:                    sub.Filter,
		EnableExactlyOnceDelivery: sub.EnableExactlyOnceDelivery,
	}
	if sub.PushEndpoint != "" {
		out.PushConfig = &pubsubpb.PushConfig{PushEndpoint: sub.PushEndpoint}
	}
	if sub.DeadLetterTopic != "" {
		out.DeadLetterPolicy = &pubsubpb.DeadLetterPolicy{
			DeadLetterTopic:     sub.DeadLetterTopic,
			MaxDeliveryAttempts: int32(sub.MaxDeliveryAttempts),
		}
	}
	return out
}

func parseRFCTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}
