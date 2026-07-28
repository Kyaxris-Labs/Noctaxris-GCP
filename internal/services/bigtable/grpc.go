package bigtable

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cloud.google.com/go/bigtable/admin/apiv2/adminpb"
	longrunningpb "cloud.google.com/go/longrunning/autogen/longrunningpb"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (s *Service) requireGRPC(ctx context.Context, permission, projectID string) error {
	if s.GRPCPrincipal == nil {
		return status.Error(codes.Unauthenticated, "gRPC auth resolver not configured")
	}
	p, err := s.GRPCPrincipal(ctx)
	if err != nil {
		return status.Error(codes.Unauthenticated, "unauthenticated")
	}
	allowed, err := s.Authz.Evaluate(p.Email, p.IsRoot, permission, "projects/"+projectID)
	if err != nil {
		return status.Errorf(codes.Internal, "%v", err)
	}
	if !allowed {
		return status.Error(codes.PermissionDenied, "permission denied")
	}
	return nil
}

func projectFromParent(parent string) (string, error) {
	parts := strings.Split(parent, "/")
	if len(parts) != 2 || parts[0] != "projects" || parts[1] == "" {
		return "", fmt.Errorf("invalid parent %q", parent)
	}
	return parts[1], nil
}

func projectFromInstanceName(name string) (string, error) {
	parts := strings.Split(name, "/")
	if len(parts) != 4 || parts[0] != "projects" || parts[1] == "" || parts[2] != "instances" || parts[3] == "" {
		return "", fmt.Errorf("invalid instance name %q", name)
	}
	return parts[1], nil
}

// CreateInstance creates an instance synchronously and returns a done Operation
// whose response is the Instance (no Operations.get poll surface).
func (s *Service) CreateInstance(ctx context.Context, req *adminpb.CreateInstanceRequest) (*longrunningpb.Operation, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	project, err := projectFromParent(req.GetParent())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := s.requireGRPC(ctx, "bigtable.instances.create", project); err != nil {
		return nil, err
	}
	instanceID := strings.TrimSpace(req.GetInstanceId())
	if instanceID == "" {
		return nil, status.Error(codes.InvalidArgument, "instance_id is required")
	}
	instIn := req.GetInstance()
	if instIn == nil {
		instIn = &adminpb.Instance{}
	}
	displayName := instIn.GetDisplayName()
	instType := instanceTypeString(instIn.GetType())
	labelsJSON := "{}"
	if len(instIn.GetLabels()) > 0 {
		raw, err := json.Marshal(instIn.GetLabels())
		if err != nil {
			return nil, status.Errorf(codes.Internal, "marshal labels: %v", err)
		}
		labelsJSON = string(raw)
	}
	clustersJSON := "{}"
	if len(req.GetClusters()) > 0 {
		clusters := make(map[string]any, len(req.GetClusters()))
		for id, c := range req.GetClusters() {
			entry := map[string]any{}
			if c != nil {
				if loc := c.GetLocation(); loc != "" {
					entry["location"] = loc
				}
				if n := c.GetServeNodes(); n != 0 {
					entry["serveNodes"] = n
				}
			}
			clusters[id] = entry
		}
		raw, err := json.Marshal(clusters)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "marshal clusters: %v", err)
		}
		clustersJSON = string(raw)
	}
	name := instanceName(project, instanceID)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	created, err := s.Store.CreateBigtableInstance(store.BigtableInstance{
		Name: name, ProjectID: project, InstanceID: instanceID,
		DisplayName: displayName, State: "READY", Type: instType,
		LabelsJSON: labelsJSON, ClustersJSON: clustersJSON, CreatedAt: now,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	if !created {
		return nil, status.Error(codes.AlreadyExists, "instance already exists")
	}
	out, ok, err := s.Store.GetBigtableInstance(name)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	if !ok {
		return nil, status.Error(codes.Internal, "instance missing after create")
	}
	pb := toInstancePB(out)
	respAny, err := anypb.New(pb)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "pack operation response: %v", err)
	}
	return &longrunningpb.Operation{
		Name: fmt.Sprintf("operations/projects/%s/instances/%s/locations/global/operations/%s",
			project, instanceID, uuid.NewString()),
		Done: true,
		Result: &longrunningpb.Operation_Response{
			Response: respAny,
		},
	}, nil
}

func (s *Service) GetInstance(ctx context.Context, req *adminpb.GetInstanceRequest) (*adminpb.Instance, error) {
	if req == nil || strings.TrimSpace(req.GetName()) == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	project, err := projectFromInstanceName(req.GetName())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := s.requireGRPC(ctx, "bigtable.instances.get", project); err != nil {
		return nil, err
	}
	inst, ok, err := s.Store.GetBigtableInstance(req.GetName())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	if !ok {
		return nil, status.Error(codes.NotFound, "Instance not found")
	}
	return toInstancePB(inst), nil
}

func (s *Service) ListInstances(ctx context.Context, req *adminpb.ListInstancesRequest) (*adminpb.ListInstancesResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	project, err := projectFromParent(req.GetParent())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := s.requireGRPC(ctx, "bigtable.instances.list", project); err != nil {
		return nil, err
	}
	list, err := s.Store.ListBigtableInstances(project)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	items := make([]*adminpb.Instance, 0, len(list))
	for _, inst := range list {
		items = append(items, toInstancePB(inst))
	}
	return &adminpb.ListInstancesResponse{
		Instances:       items,
		FailedLocations: nil,
	}, nil
}

func (s *Service) DeleteInstance(ctx context.Context, req *adminpb.DeleteInstanceRequest) (*emptypb.Empty, error) {
	if req == nil || strings.TrimSpace(req.GetName()) == "" {
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	project, err := projectFromInstanceName(req.GetName())
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	if err := s.requireGRPC(ctx, "bigtable.instances.delete", project); err != nil {
		return nil, err
	}
	ok, err := s.Store.DeleteBigtableInstance(req.GetName())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "%v", err)
	}
	if !ok {
		return nil, status.Error(codes.NotFound, "Instance not found")
	}
	return &emptypb.Empty{}, nil
}

func instanceTypeString(t adminpb.Instance_Type) string {
	switch t {
	case adminpb.Instance_DEVELOPMENT:
		return "DEVELOPMENT"
	case adminpb.Instance_PRODUCTION:
		return "PRODUCTION"
	case adminpb.Instance_TYPE_UNSPECIFIED:
		return "PRODUCTION"
	default:
		return "PRODUCTION"
	}
}

func instanceTypePB(s string) adminpb.Instance_Type {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "DEVELOPMENT":
		return adminpb.Instance_DEVELOPMENT
	case "PRODUCTION", "":
		return adminpb.Instance_PRODUCTION
	default:
		if v, ok := adminpb.Instance_Type_value[strings.ToUpper(s)]; ok {
			return adminpb.Instance_Type(v)
		}
		return adminpb.Instance_PRODUCTION
	}
}

func instanceStatePB(s string) adminpb.Instance_State {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "READY":
		return adminpb.Instance_READY
	case "CREATING":
		return adminpb.Instance_CREATING
	default:
		if v, ok := adminpb.Instance_State_value[strings.ToUpper(s)]; ok {
			return adminpb.Instance_State(v)
		}
		return adminpb.Instance_READY
	}
}

func toInstancePB(inst store.BigtableInstance) *adminpb.Instance {
	var labels map[string]string
	_ = json.Unmarshal([]byte(inst.LabelsJSON), &labels)
	if labels == nil {
		labels = map[string]string{}
	}
	out := &adminpb.Instance{
		Name:        inst.Name,
		DisplayName: inst.DisplayName,
		State:       instanceStatePB(inst.State),
		Type:        instanceTypePB(inst.Type),
		Labels:      labels,
	}
	if t, err := time.Parse(time.RFC3339Nano, inst.CreatedAt); err == nil {
		out.CreateTime = timestamppb.New(t)
	}
	return out
}
