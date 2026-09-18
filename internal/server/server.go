package server

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/config"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/audit"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/httpegress"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/tlsutil"
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
	catcherMaxBody  = 1 << 20 // 1 MiB
)

type ctxKey int

const (
	ctxPrincipal ctxKey = iota + 1
	ctxRequestID
)

// Server is the combined REST + gRPC (h2c) listener.
type Server struct {
	cfg           config.Config
	store         *store.Store
	audit         *audit.Writer
	authn         *authn.Authenticator
	authz         *authz.Evaluator
	grpc          *grpc.Server
	mux           *http.ServeMux
	now           func() time.Time
	clockMu       sync.RWMutex
	clockOverride *time.Time
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
		authz: &authz.Evaluator{Policies: st, Roles: st, Parents: st},
		mux:   http.NewServeMux(),
		now:   func() time.Time { return time.Now().UTC() },
	}
	s.authz.Now = s.effectiveNow
	s.registerREST()
	s.registerOIDCLab()
	s.registerIdentity()
	s.registerData()
	s.registerDocsCrypto()
	s.registerServerless()
	s.registerAnalytics()
	s.registerAppsBuild()
	s.registerLocationTriggers()
	s.registerComputeData()
	s.registerManagedKafka()
	s.registerSecurity()
	s.registerStorageAI()
	s.registerGKEEdge()
	s.registerCloudAudit()
	s.registerSecurityCenter()
	s.registerOrgPolicy()
	s.registerAccessContextManager()
	s.registerCloudAsset()
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
	catcher := httpegress.LabHTTPCatcherPath
	s.mux.HandleFunc("GET "+catcher, s.handleHTTPCatcherDump)
	s.mux.HandleFunc("POST "+catcher, s.handleHTTPCatcherAccept)
	s.mux.HandleFunc("POST "+catcher+"/{rest...}", s.handleHTTPCatcherAccept)
	s.registerLabForensics()
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		gcperrors.WriteREST(w, http.StatusMethodNotAllowed, gcperrors.StatusInvalidArgument, "method not allowed")
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

func (s *Server) handleHTTPCatcherDump(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]any{"deliveries": store.ListHTTPCatcher()})
}

func (s *Server) handleHTTPCatcherAccept(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, catcherMaxBody+1))
	if err != nil {
		gcperrors.WriteREST(w, http.StatusBadRequest, gcperrors.StatusInvalidArgument, "read body")
		return
	}
	if len(body) > catcherMaxBody {
		gcperrors.WriteREST(w, http.StatusRequestEntityTooLarge, gcperrors.StatusInvalidArgument, "body too large")
		return
	}
	store.RecordHTTPCatcher(string(body))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
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
		rewriteLabHostPath(r)
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

		// XML HMAC (GOOG4 header) authenticates in the XML handler; do not require Bearer.
		if isGOOG4HMACAuth(r.Header.Get("Authorization")) && strings.HasPrefix(r.URL.Path, "/storage/xml/") {
			next.ServeHTTP(w, r.WithContext(ctx))
			return
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
	handler := s.Handler()
	var cloudPaths tlsutil.Paths
	var cloudTLS *tls.Config
	if s.cfg.CloudHosts {
		secretsDir := filepath.Dir(store.DefaultMasterKeyPath(s.cfg.DataRoot))
		if strings.TrimSpace(s.cfg.MasterKeyPath) != "" {
			secretsDir = filepath.Dir(s.cfg.MasterKeyPath)
		}
		var err error
		cloudPaths, err = tlsutil.EnsureServerCert(secretsDir, time.Time{})
		if err != nil {
			return fmt.Errorf("cloud-hosts cert: %w", err)
		}
		cloudTLS, err = tlsutil.LoadTLSConfig(cloudPaths)
		if err != nil {
			return err
		}
	}

	main := &http.Server{
		Addr:              s.cfg.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	errCh := make(chan error, 2)
	go func() {
		var err error
		if s.cfg.TLSEnabled() {
			err = main.ListenAndServeTLS(s.cfg.TLSCertFile, s.cfg.TLSKeyFile)
		} else {
			err = main.ListenAndServe()
		}
		errCh <- err
	}()

	var cloud *http.Server
	if s.cfg.CloudHosts {
		cloud = &http.Server{
			Addr:              s.cfg.CloudHostsListen,
			Handler:           handler,
			TLSConfig:         cloudTLS,
			ReadHeaderTimeout: 10 * time.Second,
		}
		go func() {
			errCh <- cloud.ListenAndServeTLS(cloudPaths.ServerCert, cloudPaths.ServerKey)
		}()
	}

	select {
	case <-ctx.Done():
		if s.grpc != nil {
			s.grpc.GracefulStop()
		}
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		_ = main.Shutdown(shutdownCtx)
		if cloud != nil {
			_ = cloud.Shutdown(shutdownCtx)
		}
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
