package server

import (
	"context"
	"errors"
	"strings"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// authenticateGRPC validates Bearer credentials from gRPC metadata and attaches a principal.
func (s *Server) authenticateGRPC(ctx context.Context) (context.Context, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return ctx, status.Error(codes.Unauthenticated, "missing metadata")
	}
	vals := md.Get("authorization")
	if len(vals) == 0 {
		return ctx, status.Error(codes.Unauthenticated, "Request is missing required authentication credential.")
	}
	raw := vals[0]
	const prefix = "Bearer "
	if !strings.HasPrefix(raw, prefix) {
		return ctx, status.Error(codes.Unauthenticated, "Request is missing required authentication credential.")
	}
	token := strings.TrimSpace(strings.TrimPrefix(raw, prefix))
	if token == "" {
		return ctx, status.Error(codes.Unauthenticated, "Request is missing required authentication credential.")
	}
	p, err := s.authn.AuthenticateToken(token)
	if err != nil {
		if errors.Is(err, authn.ErrUnauthenticated) {
			return ctx, status.Error(codes.Unauthenticated, "Request had invalid authentication credentials.")
		}
		return ctx, status.Errorf(codes.Internal, "authenticate: %v", err)
	}
	return context.WithValue(ctx, ctxPrincipal, p), nil
}

func (s *Server) unaryAuthInterceptor(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	ctx, err := s.authenticateGRPC(ctx)
	if err != nil {
		return nil, err
	}
	return handler(ctx, req)
}

type authStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s authStream) Context() context.Context { return s.ctx }

func (s *Server) streamAuthInterceptor(srv any, ss grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	ctx, err := s.authenticateGRPC(ss.Context())
	if err != nil {
		return err
	}
	return handler(srv, authStream{ServerStream: ss, ctx: ctx})
}

// newGRPCServer builds a gRPC server with Bearer auth interceptors.
func (s *Server) newGRPCServer() *grpc.Server {
	return grpc.NewServer(
		grpc.UnaryInterceptor(s.unaryAuthInterceptor),
		grpc.StreamInterceptor(s.streamAuthInterceptor),
	)
}
