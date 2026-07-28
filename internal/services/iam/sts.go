package iam

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
)

// GrantTypeTokenExchange is the STS token-exchange grant type.
const GrantTypeTokenExchange = "urn:ietf:params:oauth:grant-type:token-exchange"

// MountSTS registers the lab STS token endpoint (WIF exchange theatre).
// POST /v1/token is public (subject_token authenticates the external identity).
func (h *Handler) MountSTS(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/token", h.stsToken)
}

// stsToken implements a lab subset of Google STS token exchange for WIF.
// Required: grant_type=token-exchange, audience (provider resource name), subject_token.
// Returns access_token registered as principal wif:{providerID}:{subject}.
func (h *Handler) stsToken(w http.ResponseWriter, r *http.Request) {
	grantType, audience, subjectToken, subjectTokenType := "", "", "", ""
	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") {
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err != nil {
			gcperrors.InvalidArgument(w, "unable to read body")
			return
		}
		var req struct {
			GrantType        string `json:"grantType"`
			GrantTypeSnake   string `json:"grant_type"`
			Audience         string `json:"audience"`
			SubjectToken     string `json:"subjectToken"`
			SubjectTokenSnake string `json:"subject_token"`
			SubjectTokenType string `json:"subjectTokenType"`
			SubjectTokenTypeSnake string `json:"subject_token_type"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			gcperrors.InvalidArgument(w, "invalid JSON body")
			return
		}
		grantType = firstNonEmpty(req.GrantType, req.GrantTypeSnake)
		audience = req.Audience
		subjectToken = firstNonEmpty(req.SubjectToken, req.SubjectTokenSnake)
		subjectTokenType = firstNonEmpty(req.SubjectTokenType, req.SubjectTokenTypeSnake)
	} else {
		_ = r.ParseForm()
		vals := r.Form
		if vals == nil {
			raw, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			vals, _ = url.ParseQuery(string(raw))
		}
		grantType = vals.Get("grant_type")
		audience = vals.Get("audience")
		subjectToken = vals.Get("subject_token")
		subjectTokenType = vals.Get("subject_token_type")
	}
	if grantType != GrantTypeTokenExchange {
		gcperrors.InvalidArgument(w, "grant_type must be "+GrantTypeTokenExchange)
		return
	}
	if strings.TrimSpace(audience) == "" {
		gcperrors.InvalidArgument(w, "audience is required (WIF provider resource name)")
		return
	}
	if strings.TrimSpace(subjectToken) == "" {
		gcperrors.InvalidArgument(w, "subject_token is required")
		return
	}
	_ = subjectTokenType // accepted; lab does not validate JWT signature

	providerName := normalizeWIFAudience(audience)
	prov, ok, err := h.Store.GetWIFProvider(providerName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok || prov.State != "ACTIVE" || prov.Disabled {
		gcperrors.WriteREST(w, http.StatusUnauthorized, gcperrors.StatusUnauthenticated, "unknown or disabled workload identity provider")
		return
	}
	pool, ok, err := h.Store.GetWIFPool(prov.PoolName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok || pool.State != "ACTIVE" || pool.Disabled {
		gcperrors.WriteREST(w, http.StatusUnauthorized, gcperrors.StatusUnauthenticated, "unknown or disabled workload identity pool")
		return
	}

	subject := labSubjectFromToken(subjectToken)
	principalEmail := "wif:" + prov.ProviderID + ":" + subject
	token := newAccessToken()
	expire := h.now().Add(time.Hour)
	if err := h.Store.PutAccessToken(authn.HashToken(token), principalEmail, expire); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":     token,
		"issued_token_type": "urn:ietf:params:oauth:token-type:access_token",
		"token_type":       "Bearer",
		"expires_in":       3600,
	})
}

func normalizeWIFAudience(audience string) string {
	a := strings.TrimSpace(audience)
	const prefix = "//iam.googleapis.com/"
	if strings.HasPrefix(a, prefix) {
		a = strings.TrimPrefix(a, prefix)
	}
	return a
}

func labSubjectFromToken(subjectToken string) string {
	s := strings.TrimSpace(subjectToken)
	if len(s) > 64 {
		s = s[:64]
	}
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			return r
		}
		return '-'
	}, s)
	if s == "" {
		return "anonymous"
	}
	return s
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
