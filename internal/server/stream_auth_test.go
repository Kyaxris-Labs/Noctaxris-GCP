package server

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type stubStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s stubStream) Context() context.Context { return s.ctx }

func TestAuthStreamContextAndInterceptorUnauth(t *testing.T) {
	s := &Server{}
	inner := stubStream{ctx: context.Background()}
	wrapped := authStream{ServerStream: inner, ctx: context.WithValue(context.Background(), ctxPrincipal, "x")}
	if wrapped.Context() == nil {
		t.Fatal("context")
	}
	err := s.streamAuthInterceptor(nil, inner, nil, func(any, grpc.ServerStream) error {
		t.Fatal("should not run")
		return nil
	})
	if err == nil {
		t.Fatal("expected unauthenticated")
	}
	md := metadata.Pairs("authorization", "Bearer ")
	ctx := metadata.NewIncomingContext(context.Background(), md)
	err = s.streamAuthInterceptor(nil, stubStream{ctx: ctx}, nil, func(any, grpc.ServerStream) error {
		return nil
	})
	if err == nil {
		t.Fatal("empty bearer")
	}
}
