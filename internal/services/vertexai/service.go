package vertexai

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
)

// DefaultLocation is the lab default Vertex AI region.
const DefaultLocation = "us-central1"

// AllowlistedModelIDs are the only publisher model ids that return canned JSON.
// Unknown modelId fails closed (404). No real models are invoked.
var AllowlistedModelIDs = map[string]struct{}{
	"gemini-1.5-flash":   {},
	"gemini-1.5-pro":     {},
	"gemini-2.0-flash":   {},
	"text-embedding-004": {},
	"text-bison":         {},
	"text-bison@001":     {},
}

// Service serves Vertex AI publisher-model predict / generateContent theatre.
type Service struct {
	Authz *authz.Evaluator
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// Mount registers publisher model custom methods.
// Colon actions live inside the {model} path value (Go ServeMux forbids `{id}:action`).
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	mux.HandleFunc(
		"POST /v1/projects/{project}/locations/{location}/publishers/{publisher}/models/{model}",
		s.wrap(principalFrom, s.modelPost),
	)
}

type handlerFunc func(w http.ResponseWriter, r *http.Request, p authn.Principal)

func (s *Service) wrap(principalFrom principalFunc, h handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := principalFrom(r)
		if !ok {
			gcperrors.Unauthenticated(w, "")
			return
		}
		h(w, r, p)
	}
}

func (s *Service) require(p authn.Principal, permission, projectID string) error {
	ok, err := s.Authz.Evaluate(p.Email, p.IsRoot, permission, "projects/"+projectID)
	if err != nil {
		return err
	}
	if !ok {
		return errDenied
	}
	return nil
}

var errDenied = fmt.Errorf("permission denied")

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeAuthzErr(w http.ResponseWriter, err error) {
	if err == errDenied {
		gcperrors.PermissionDenied(w, "")
		return
	}
	gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
}

func splitColonAction(seg string) (id, action string) {
	if i := strings.IndexByte(seg, ':'); i >= 0 {
		return seg[:i], seg[i+1:]
	}
	return seg, ""
}

func (s *Service) modelPost(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	publisher := r.PathValue("publisher")
	modelID, action := splitColonAction(r.PathValue("model"))
	if err := s.require(p, "aiplatform.endpoints.predict", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	if publisher != "google" {
		gcperrors.NotFound(w, "Publisher not found")
		return
	}
	if _, ok := AllowlistedModelIDs[modelID]; !ok {
		gcperrors.NotFound(w, "Model not found")
		return
	}
	// Drain body so clients may send instances/contents; ignored for canned replies.
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)

	switch action {
	case "predict":
		writeJSON(w, http.StatusOK, cannedPredict(modelID))
	case "generateContent":
		writeJSON(w, http.StatusOK, cannedGenerateContent(modelID))
	case "":
		gcperrors.InvalidArgument(w, "expected models/{model}:predict or models/{model}:generateContent")
	default:
		gcperrors.InvalidArgument(w, "unsupported model method: "+action)
	}
}

func cannedPredict(modelID string) map[string]any {
	return map[string]any{
		"predictions": []any{
			map[string]any{
				"content": fmt.Sprintf("Lab canned prediction from Noctaxris-GCP (%s)", modelID),
			},
		},
		"deployedModelId":  "lab-canned",
		"model":            "publishers/google/models/" + modelID,
		"modelDisplayName": modelID,
		"modelVersionId":   "1",
	}
}

func cannedGenerateContent(modelID string) map[string]any {
	return map[string]any{
		"candidates": []any{
			map[string]any{
				"content": map[string]any{
					"role": "model",
					"parts": []any{
						map[string]any{
							"text": fmt.Sprintf("Lab canned generateContent from Noctaxris-GCP (%s)", modelID),
						},
					},
				},
				"finishReason": "STOP",
				"index":        0,
			},
		},
		"usageMetadata": map[string]any{
			"promptTokenCount":     1,
			"candidatesTokenCount": 1,
			"totalTokenCount":      2,
		},
		"modelVersion": modelID,
	}
}
