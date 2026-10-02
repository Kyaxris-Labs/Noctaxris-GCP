package firestore

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestWriteFirestoreRESTErrMapping(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code int
	}{
		{"plain", errors.New("boom"), http.StatusInternalServerError},
		{"denied", status.Error(codes.PermissionDenied, "no"), http.StatusForbidden},
		{"unauth", status.Error(codes.Unauthenticated, "u"), http.StatusUnauthorized},
		{"notfound", status.Error(codes.NotFound, "n"), http.StatusNotFound},
		{"exists", status.Error(codes.AlreadyExists, "e"), http.StatusConflict},
		{"invalid", status.Error(codes.InvalidArgument, "i"), http.StatusBadRequest},
		{"other", status.Error(codes.Internal, "x"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			writeFirestoreRESTErr(rec, tc.err)
			if rec.Code != tc.code {
				t.Fatalf("status=%d want %d body=%s", rec.Code, tc.code, rec.Body.String())
			}
		})
	}
}
