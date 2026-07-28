package pubsub

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"cloud.google.com/go/pubsub/v2/apiv1/pubsubpb"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
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

	Store     *store.Store
	Authz     *authz.Evaluator
	Principal PrincipalResolver
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

func (s *Service) CreateTopic(ctx context.Context, topic *pubsubpb.Topic) (*pubsubpb.Topic, error) {
	projectID := projectFromResource(topic.GetName())
	if projectID == "" {
		return nil, gcperrors.GRPC(gcperrors.StatusInvalidArgument, "invalid topic name")
	}
	if err := s.require(ctx, "pubsub.topics.create", projectResource(projectID)); err != nil {
		return nil, err
	}
	t, created, err := s.Store.CreateTopic(topic.GetName(), projectID)
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
		id, err := s.Store.Publish(req.GetTopic(), m.GetData(), m.GetAttributes())
		if err != nil {
			return nil, status.Errorf(codes.Internal, "%v", err)
		}
		resp.MessageIds = append(resp.MessageIds, id)
	}
	return resp, nil
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
	created, ok, err := s.Store.CreateSubscription(sub.GetName(), sub.GetTopic(), projectID, ack)
	if err != nil {
		if strings.Contains(err.Error(), "topic not found") {
			return nil, status.Error(codes.NotFound, "topic not found")
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
	out := &pubsubpb.PullResponse{}
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
		out.ReceivedMessages = append(out.ReceivedMessages, &pubsubpb.ReceivedMessage{
			AckId:   msgs[i].AckID,
			Message: pm,
		})
	}
	return out, nil
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

// StreamingPull is not implemented in Wave 1.
func (s *Service) StreamingPull(stream pubsubpb.Subscriber_StreamingPullServer) error {
	return status.Error(codes.Unimplemented, "StreamingPull is not implemented in this lab")
}

func topicPB(t *store.PubSubTopic) *pubsubpb.Topic {
	return &pubsubpb.Topic{Name: t.Name}
}

func subscriptionPB(sub *store.PubSubSubscription) *pubsubpb.Subscription {
	return &pubsubpb.Subscription{
		Name:               sub.Name,
		Topic:              sub.Topic,
		AckDeadlineSeconds: int32(sub.AckDeadlineSeconds),
	}
}

func parseRFCTime(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
}
