package gcperrors_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestWriteRESTAndHelpers(t *testing.T) {
	rec := httptest.NewRecorder()
	gcperrors.WriteREST(rec, http.StatusBadRequest, gcperrors.StatusInvalidArgument, "bad")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code=%d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json; charset=utf-8" {
		t.Fatalf("content-type=%q", ct)
	}
	var body gcperrors.ErrorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != http.StatusBadRequest || body.Error.Status != gcperrors.StatusInvalidArgument || body.Error.Message != "bad" {
		t.Fatalf("body=%+v", body.Error)
	}

	cases := []struct {
		name string
		fn   func(http.ResponseWriter, string)
		code int
		st   string
		msg  string
		def  string
	}{
		{"unauth empty", gcperrors.Unauthenticated, http.StatusUnauthorized, gcperrors.StatusUnauthenticated, "", "Request is missing required authentication credential"},
		{"unauth custom", gcperrors.Unauthenticated, http.StatusUnauthorized, gcperrors.StatusUnauthenticated, "need token", "need token"},
		{"deny empty", gcperrors.PermissionDenied, http.StatusForbidden, gcperrors.StatusPermissionDenied, "", "The caller does not have permission."},
		{"deny custom", gcperrors.PermissionDenied, http.StatusForbidden, gcperrors.StatusPermissionDenied, "nope", "nope"},
		{"notfound", gcperrors.NotFound, http.StatusNotFound, gcperrors.StatusNotFound, "missing", "missing"},
		{"invalid", gcperrors.InvalidArgument, http.StatusBadRequest, gcperrors.StatusInvalidArgument, "invalid", "invalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRecorder()
			tc.fn(r, tc.msg)
			if r.Code != tc.code {
				t.Fatalf("code=%d want %d", r.Code, tc.code)
			}
			var b gcperrors.ErrorBody
			if err := json.Unmarshal(r.Body.Bytes(), &b); err != nil {
				t.Fatal(err)
			}
			if b.Error.Status != tc.st {
				t.Fatalf("status=%q", b.Error.Status)
			}
			if tc.msg == "" {
				if b.Error.Message == "" || (tc.def != "" && b.Error.Message[:len(tc.def)] != tc.def && b.Error.Message != tc.def) {
					if b.Error.Message != tc.def && !containsPrefix(b.Error.Message, tc.def) {
						t.Fatalf("message=%q want prefix %q", b.Error.Message, tc.def)
					}
				}
			} else if b.Error.Message != tc.def {
				t.Fatalf("message=%q", b.Error.Message)
			}
		})
	}
}

func containsPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func TestToGRPCCodeAndGRPC(t *testing.T) {
	mapping := map[string]codes.Code{
		gcperrors.StatusOK:                 codes.OK,
		gcperrors.StatusCancelled:          codes.Canceled,
		gcperrors.StatusInvalidArgument:    codes.InvalidArgument,
		gcperrors.StatusDeadlineExceeded:   codes.DeadlineExceeded,
		gcperrors.StatusNotFound:           codes.NotFound,
		gcperrors.StatusAlreadyExists:      codes.AlreadyExists,
		gcperrors.StatusPermissionDenied:   codes.PermissionDenied,
		gcperrors.StatusResourceExhausted:  codes.ResourceExhausted,
		gcperrors.StatusFailedPrecondition: codes.FailedPrecondition,
		gcperrors.StatusAborted:            codes.Aborted,
		gcperrors.StatusOutOfRange:         codes.OutOfRange,
		gcperrors.StatusUnimplemented:      codes.Unimplemented,
		gcperrors.StatusInternal:           codes.Internal,
		gcperrors.StatusUnavailable:        codes.Unavailable,
		gcperrors.StatusDataLoss:           codes.DataLoss,
		gcperrors.StatusUnauthenticated:    codes.Unauthenticated,
		"NOT_A_REAL_STATUS":                codes.Unknown,
		gcperrors.StatusUnknown:            codes.Unknown,
	}
	for name, want := range mapping {
		if got := gcperrors.ToGRPCCode(name); got != want {
			t.Fatalf("ToGRPCCode(%q)=%v want %v", name, got, want)
		}
	}
	err := gcperrors.GRPC(gcperrors.StatusNotFound, "gone")
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.NotFound || st.Message() != "gone" {
		t.Fatalf("GRPC err=%v", err)
	}
}

func TestHTTPStatusFor(t *testing.T) {
	cases := map[string]int{
		gcperrors.StatusOK:                 http.StatusOK,
		gcperrors.StatusInvalidArgument:    http.StatusBadRequest,
		gcperrors.StatusFailedPrecondition: http.StatusBadRequest,
		gcperrors.StatusOutOfRange:         http.StatusBadRequest,
		gcperrors.StatusUnauthenticated:    http.StatusUnauthorized,
		gcperrors.StatusPermissionDenied:   http.StatusForbidden,
		gcperrors.StatusNotFound:           http.StatusNotFound,
		gcperrors.StatusAlreadyExists:      http.StatusConflict,
		gcperrors.StatusAborted:            http.StatusConflict,
		gcperrors.StatusResourceExhausted:  http.StatusTooManyRequests,
		gcperrors.StatusCancelled:          499,
		gcperrors.StatusUnimplemented:      http.StatusNotImplemented,
		gcperrors.StatusUnavailable:        http.StatusServiceUnavailable,
		gcperrors.StatusDeadlineExceeded:   http.StatusGatewayTimeout,
		gcperrors.StatusInternal:           http.StatusInternalServerError,
		gcperrors.StatusUnknown:            http.StatusInternalServerError,
		"WEIRD":                            http.StatusInternalServerError,
	}
	for name, want := range cases {
		if got := gcperrors.HTTPStatusFor(name); got != want {
			t.Fatalf("HTTPStatusFor(%q)=%d want %d", name, got, want)
		}
	}
}
