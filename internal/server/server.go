package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/config"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/audit"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/version"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
	"google.golang.org/grpc"
)

const (
	healthPath      = "/_noctaxris-gcp/health"
	readyPath       = "/_noctaxris-gcp/ready"
	versionPath     = "/_noctaxris-gcp/version"
	requestIDHeader = "X-Request-Id"
	shutdownTimeout = 10 * time.Second
)

type ctxKey int

const (
	ctxPrincipal ctxKey = iota + 1
	ctxRequestID
)

// Server is the combined REST + gRPC (h2c) listener.
type Server struct {
	cfg   config.Config
	store *store.Store
	audit *audit.Writer
	authn *authn.Authenticator
	authz *authz.Evaluator
	grpc  *grpc.Server
	mux   *http.ServeMux
	now   func() time.Time
}

// New builds a Server with health routes, identity REST, and gRPC Bearer auth.
func New(cfg config.Config, st *store.Store, aud *audit.Writer) *Server {
	s := &Server{
		cfg:   cfg,
		store: st,
		audit: aud,
		authn: &authn.Authenticator{
			RootServiceAccount: cfg.RootServiceAccount,
			RootAccessToken:    cfg.RootAccessToken,
			Tokens:             st,
		},
		authz: &authz.Evaluator{Policies: st},
		mux:   http.NewServeMux(),
		now:   func() time.Time { return time.Now().UTC() },
	}
	s.registerREST()
	s.registerIdentity()
	s.registerData()
	s.registerDocsCrypto()
	s.registerServerless()
	s.registerAnalytics()
	s.registerAppsBuild()
	s.registerLocationTriggers()
	s.registerComputeData()
	s.registerSecurity()
	s.registerStorageAI()
	return s
}

// GRPC returns the underlying gRPC server for service registration.
func (s *Server) GRPC() *grpc.Server { return s.grpc }

// Authz returns the IAM evaluator for handlers.
func (s *Server) Authz() *authz.Evaluator { return s.authz }

// PrincipalFromContext returns the authenticated principal when present.
func PrincipalFromContext(ctx context.Context) (authn.Principal, bool) {
	p, ok := ctx.Value(ctxPrincipal).(authn.Principal)
	return p, ok
}

// RequestIDFromContext returns the request id when present.
func RequestIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(ctxRequestID).(string)
	return id
}

func (s *Server) registerREST() {
	s.mux.HandleFunc(healthPath, s.handleHealth)
	s.mux.HandleFunc(readyPath, s.handleReady)
	s.mux.HandleFunc(versionPath, s.handleVersion)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		gcperrors.WriteREST(w, http.StatusMethodNotAllowed, gcperrors.StatusInvalidArgument, "method not allowed")
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		gcperrors.WriteREST(w, http.StatusMethodNotAllowed, gcperrors.StatusInvalidArgument, "method not allowed")
		return
	}
	if s.store == nil {
		gcperrors.WriteREST(w, http.StatusServiceUnavailable, gcperrors.StatusUnavailable, "store not ready")
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) handleVersion(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		gcperrors.WriteREST(w, http.StatusMethodNotAllowed, gcperrors.StatusInvalidArgument, "method not allowed")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"version": version.Version})
}

// Handler returns the h2c-capable HTTP handler (REST + gRPC).
func (s *Server) Handler() http.Handler {
	inner := http.HandlerFunc(s.serveHTTP)
	return h2c.NewHandler(s.withMiddleware(inner), &http2.Server{})
}

func (s *Server) withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqID := r.Header.Get(requestIDHeader)
		if reqID == "" {
			reqID = newRequestID()
		}
		w.Header().Set(requestIDHeader, reqID)
		ctx := context.WithValue(r.Context(), ctxRequestID, reqID)

		if authn.IsPublicPath(r.URL.Path) {
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// gRPC auth is enforced by interceptors once services register; REST requires Bearer now.
		if strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}

		// Lab GCS V4 signed URLs authenticate via query signature (verified in the GCS handler).
		// Only storage JSON/media paths may skip Bearer; never open other APIs via X-Goog-*.
		if store.HasV4Signature(r.URL.Query()) && r.Header.Get("Authorization") == "" {
			path := r.URL.Path
			if strings.HasPrefix(path, "/storage/") || strings.HasPrefix(path, "/upload/storage/") {
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}

		p, err := s.authn.AuthenticateRequest(r)
		if err != nil {
			if errors.Is(err, authn.ErrUnauthenticated) {
				gcperrors.Unauthenticated(w, "")
				return
			}
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		}
		ctx = context.WithValue(ctx, ctxPrincipal, p)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "application/grpc") {
		s.grpc.ServeHTTP(w, r)
		return
	}
	s.mux.ServeHTTP(w, r)
}

// ListenAndServeContext serves until ctx is cancelled, then drains with a timeout.
func (s *Server) ListenAndServeContext(ctx context.Context) error {
	srv := &http.Server{
		Addr:              s.cfg.ListenAddr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		var err error
		if s.cfg.TLSEnabled() {
			err = srv.ListenAndServeTLS(s.cfg.TLSCertFile, s.cfg.TLSKeyFile)
		} else {
			err = srv.ListenAndServe()
		}
		errCh <- err
	}()

	select {
	case <-ctx.Done():
		s.grpc.GracefulStop()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		err := <-errCh
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func newRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
