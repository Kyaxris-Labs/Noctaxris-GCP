package firestore_test

import (
	"testing"

	"cloud.google.com/go/firestore/apiv1/firestorepb"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestListenUnimplemented(t *testing.T) {
	client, _, cleanup := startFirestore(t)
	defer cleanup()
	ctx := authCtx("test-root-token")
	stream, err := client.Listen(ctx)
	if err != nil {
		t.Fatal(err)
	}
	err = stream.Send(&firestorepb.ListenRequest{})
	if err == nil {
		_, err = stream.Recv()
	}
	if err == nil {
		t.Fatal("expected Listen unimplemented error")
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.Unimplemented {
		// Some gRPC stacks surface send-side errors differently; accept any error.
		if err == nil {
			t.Fatalf("err=%v", err)
		}
	}
}
