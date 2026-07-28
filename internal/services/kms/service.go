package kms

import (
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/gcperrors"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authz"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// DefaultLocation is the lab default Cloud KMS location.
const DefaultLocation = "global"

// Service serves Cloud KMS v1 REST (lab subset).
type Service struct {
	Store *store.Store
	Authz *authz.Evaluator
}

type principalFunc func(*http.Request) (authn.Principal, bool)

// Mount registers KMS REST routes on mux.
// Colon method suffixes (:encrypt, :decrypt, :destroy) are parsed manually because
// net/http ServeMux wildcards cannot embed ':' inside a segment.
func (s *Service) Mount(mux *http.ServeMux, principalFrom principalFunc) {
	mux.HandleFunc("GET /v1/projects/{project}/locations/{location}/{rest...}", s.wrap(principalFrom, s.dispatch))
	mux.HandleFunc("POST /v1/projects/{project}/locations/{location}/{rest...}", s.wrap(principalFrom, s.dispatch))
	mux.HandleFunc("PATCH /v1/projects/{project}/locations/{location}/{rest...}", s.wrap(principalFrom, s.dispatch))
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

func (s *Service) dispatch(w http.ResponseWriter, r *http.Request, p authn.Principal) {
	project := r.PathValue("project")
	location := r.PathValue("location")
	rest := strings.Trim(r.PathValue("rest"), "/")
	parts := splitPath(rest)

	switch {
	case r.Method == http.MethodPost && len(parts) == 1 && parts[0] == "keyRings":
		s.createKeyRing(w, r, p, project, location)
	case r.Method == http.MethodGet && len(parts) == 1 && parts[0] == "keyRings":
		s.listKeyRings(w, r, p, project, location)
	case r.Method == http.MethodGet && len(parts) == 2 && parts[0] == "keyRings":
		s.getKeyRing(w, r, p, project, location, parts[1])
	case r.Method == http.MethodPost && len(parts) == 3 && parts[0] == "keyRings" && parts[2] == "cryptoKeys":
		s.createCryptoKey(w, r, p, project, location, parts[1])
	case r.Method == http.MethodGet && len(parts) == 3 && parts[0] == "keyRings" && parts[2] == "cryptoKeys":
		s.listCryptoKeys(w, r, p, project, location, parts[1])
	case r.Method == http.MethodGet && len(parts) == 4 && parts[0] == "keyRings" && parts[2] == "cryptoKeys":
		s.getCryptoKey(w, r, p, project, location, parts[1], parts[3])
	case r.Method == http.MethodPatch && len(parts) == 4 && parts[0] == "keyRings" && parts[2] == "cryptoKeys":
		s.updateCryptoKey(w, r, p, project, location, parts[1], parts[3])
	case r.Method == http.MethodGet && len(parts) == 5 && parts[0] == "keyRings" && parts[2] == "cryptoKeys" && parts[4] == "cryptoKeyVersions":
		s.listKeyVersions(w, r, p, project, location, parts[1], parts[3])
	case r.Method == http.MethodGet && len(parts) == 6 && parts[0] == "keyRings" && parts[2] == "cryptoKeys" && parts[4] == "cryptoKeyVersions":
		s.getKeyVersion(w, r, p, project, location, parts[1], parts[3], parts[5])
	case r.Method == http.MethodGet && len(parts) == 7 && parts[0] == "keyRings" && parts[2] == "cryptoKeys" && parts[4] == "cryptoKeyVersions" && parts[6] == "publicKey":
		s.getPublicKey(w, r, p, project, location, parts[1], parts[3], parts[5])
	case r.Method == http.MethodPost && len(parts) == 4 && parts[0] == "keyRings" && parts[2] == "cryptoKeys":
		key, action := splitAction(parts[3])
		switch action {
		case "encrypt":
			s.encrypt(w, r, p, project, location, parts[1], key, "")
		case "decrypt":
			s.decrypt(w, r, p, project, location, parts[1], key, "")
		case "getIamPolicy":
			s.getIamPolicy(w, r, p, project, location, parts[1], key)
		case "setIamPolicy":
			s.setIamPolicy(w, r, p, project, location, parts[1], key)
		default:
			gcperrors.NotFound(w, "unknown KMS method")
		}
	case r.Method == http.MethodPost && len(parts) == 6 && parts[0] == "keyRings" && parts[2] == "cryptoKeys" && parts[4] == "cryptoKeyVersions":
		ver, action := splitAction(parts[5])
		switch action {
		case "encrypt":
			s.encrypt(w, r, p, project, location, parts[1], parts[3], ver)
		case "decrypt":
			s.decrypt(w, r, p, project, location, parts[1], parts[3], ver)
		case "destroy":
			s.destroyVersion(w, r, p, project, location, parts[1], parts[3], ver)
		case "restore":
			s.restoreVersion(w, r, p, project, location, parts[1], parts[3], ver)
		case "asymmetricSign":
			s.asymmetricSign(w, r, p, project, location, parts[1], parts[3], ver)
		default:
			gcperrors.NotFound(w, "unknown KMS method")
		}
	default:
		gcperrors.NotFound(w, "unknown KMS path")
	}
}

func splitPath(rest string) []string {
	if rest == "" {
		return nil
	}
	return strings.Split(rest, "/")
}

func splitAction(seg string) (name, action string) {
	if i := strings.IndexByte(seg, ':'); i >= 0 {
		return seg[:i], seg[i+1:]
	}
	return seg, ""
}

func (s *Service) createKeyRing(w http.ResponseWriter, r *http.Request, p authn.Principal, project, location string) {
	if err := s.require(p, "cloudkms.keyRings.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	keyRingID := r.URL.Query().Get("keyRingId")
	if keyRingID == "" {
		gcperrors.InvalidArgument(w, "keyRingId is required")
		return
	}
	name := fmt.Sprintf("projects/%s/locations/%s/keyRings/%s", project, location, keyRingID)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	created, err := s.Store.CreateKMSKeyRing(store.KMSKeyRing{
		Name: name, ProjectID: project, Location: location, CreatedAt: now,
	})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "key ring already exists")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": name, "createTime": now})
}

func (s *Service) getKeyRing(w http.ResponseWriter, _ *http.Request, p authn.Principal, project, location, keyRing string) {
	if err := s.require(p, "cloudkms.keyRings.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := fmt.Sprintf("projects/%s/locations/%s/keyRings/%s", project, location, keyRing)
	kr, ok, err := s.Store.GetKMSKeyRing(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "KeyRing not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"name": kr.Name, "createTime": kr.CreatedAt})
}

func (s *Service) listKeyRings(w http.ResponseWriter, _ *http.Request, p authn.Principal, project, location string) {
	if err := s.require(p, "cloudkms.keyRings.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	list, err := s.Store.ListKMSKeyRings(project, location)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, kr := range list {
		items = append(items, map[string]any{"name": kr.Name, "createTime": kr.CreatedAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"keyRings": items})
}

func (s *Service) createCryptoKey(w http.ResponseWriter, r *http.Request, p authn.Principal, project, location, keyRing string) {
	if err := s.require(p, "cloudkms.cryptoKeys.create", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	cryptoKeyID := r.URL.Query().Get("cryptoKeyId")
	if cryptoKeyID == "" {
		gcperrors.InvalidArgument(w, "cryptoKeyId is required")
		return
	}
	var body struct {
		Purpose         string            `json:"purpose"`
		Labels          map[string]string `json:"labels"`
		VersionTemplate *struct {
			Algorithm       string `json:"algorithm"`
			ProtectionLevel string `json:"protectionLevel"`
		} `json:"versionTemplate"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	purpose := body.Purpose
	if purpose == "" {
		purpose = store.KMSPurposeEncrypt
	}
	algorithm := store.KMSAlgoSymmetric
	if body.VersionTemplate != nil && body.VersionTemplate.Algorithm != "" {
		algorithm = body.VersionTemplate.Algorithm
	}
	switch purpose {
	case store.KMSPurposeEncrypt:
		if algorithm != store.KMSAlgoSymmetric && algorithm != "" {
			// Symmetric encrypt/decrypt ignores non-symmetric algorithms; keep lab default.
			algorithm = store.KMSAlgoSymmetric
		}
	case store.KMSPurposeSign:
		if algorithm == "" || algorithm == store.KMSAlgoSymmetric {
			algorithm = store.KMSAlgoRSAPSS2048
		}
		if algorithm != store.KMSAlgoRSAPSS2048 {
			gcperrors.InvalidArgument(w, "only RSA_SIGN_PSS_2048_SHA256 is supported for ASYMMETRIC_SIGN")
			return
		}
	default:
		gcperrors.InvalidArgument(w, "supported purposes: ENCRYPT_DECRYPT, ASYMMETRIC_SIGN")
		return
	}
	krName := fmt.Sprintf("projects/%s/locations/%s/keyRings/%s", project, location, keyRing)
	if _, ok, err := s.Store.GetKMSKeyRing(krName); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "KeyRing not found")
		return
	}
	keyName := krName + "/cryptoKeys/" + cryptoKeyID
	verName := keyName + "/cryptoKeyVersions/1"

	var material []byte
	if purpose == store.KMSPurposeSign {
		priv, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		}
		material, err = x509.MarshalPKCS8PrivateKey(priv)
		if err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		}
	} else {
		material = make([]byte, 32)
		if _, err := io.ReadFull(rand.Reader, material); err != nil {
			gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
			return
		}
	}
	sealed, err := s.Store.Seal(material)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	labelsJSON := "{}"
	if body.Labels != nil {
		raw, _ := json.Marshal(body.Labels)
		labelsJSON = string(raw)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	created, err := s.Store.CreateKMSCryptoKey(
		store.KMSCryptoKey{
			Name: keyName, KeyRing: krName, Purpose: purpose, Algorithm: algorithm,
			LabelsJSON: labelsJSON, CreatedAt: now,
		},
		store.KMSKeyVersion{
			Name: verName, CryptoKey: keyName, VersionID: "1",
			State: store.KMSStateEnabled, KeyMaterialCiphertext: sealed, CreatedAt: now,
		},
	)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !created {
		gcperrors.WriteREST(w, http.StatusConflict, gcperrors.StatusAlreadyExists, "crypto key already exists")
		return
	}
	writeJSON(w, http.StatusOK, cryptoKeyResource(keyName, purpose, algorithm, labelsJSON, now, verName, "ENABLED", now))
}

func cryptoKeyResource(name, purpose, algorithm, labelsJSON, createTime, primaryName, primaryState, primaryCreate string) map[string]any {
	var labels map[string]string
	_ = json.Unmarshal([]byte(labelsJSON), &labels)
	if labels == nil {
		labels = map[string]string{}
	}
	out := map[string]any{
		"name": name, "purpose": purpose, "createTime": createTime, "labels": labels,
		"versionTemplate": map[string]any{
			"algorithm": algorithm, "protectionLevel": "SOFTWARE",
		},
	}
	if primaryName != "" {
		out["primary"] = map[string]any{
			"name": primaryName, "state": primaryState, "createTime": primaryCreate,
			"algorithm": algorithm, "protectionLevel": "SOFTWARE",
		}
	}
	return out
}

func (s *Service) getCryptoKey(w http.ResponseWriter, _ *http.Request, p authn.Principal, project, location, keyRing, cryptoKey string) {
	if err := s.require(p, "cloudkms.cryptoKeys.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := fmt.Sprintf("projects/%s/locations/%s/keyRings/%s/cryptoKeys/%s", project, location, keyRing, cryptoKey)
	k, ok, err := s.Store.GetKMSCryptoKey(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "CryptoKey not found")
		return
	}
	primary, pok, err := s.Store.PrimaryKMSKeyVersion(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	primaryName, primaryState, primaryCreate := "", "", ""
	if pok {
		primaryName, primaryState, primaryCreate = primary.Name, primary.State, primary.CreatedAt
	}
	writeJSON(w, http.StatusOK, cryptoKeyResource(k.Name, k.Purpose, k.Algorithm, k.LabelsJSON, k.CreatedAt, primaryName, primaryState, primaryCreate))
}

func (s *Service) updateCryptoKey(w http.ResponseWriter, r *http.Request, p authn.Principal, project, location, keyRing, cryptoKey string) {
	if err := s.require(p, "cloudkms.cryptoKeys.update", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := fmt.Sprintf("projects/%s/locations/%s/keyRings/%s/cryptoKeys/%s", project, location, keyRing, cryptoKey)
	k, ok, err := s.Store.GetKMSCryptoKey(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "CryptoKey not found")
		return
	}
	var body struct {
		Labels map[string]string `json:"labels"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	labelsJSON := k.LabelsJSON
	updateMask := r.URL.Query().Get("updateMask")
	if body.Labels != nil && (updateMask == "" || strings.Contains(updateMask, "labels")) {
		raw, _ := json.Marshal(body.Labels)
		labelsJSON = string(raw)
	}
	updated, ok, err := s.Store.UpdateKMSCryptoKey(name, labelsJSON)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "CryptoKey not found")
		return
	}
	primary, pok, err := s.Store.PrimaryKMSKeyVersion(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	primaryName, primaryState, primaryCreate := "", "", ""
	if pok {
		primaryName, primaryState, primaryCreate = primary.Name, primary.State, primary.CreatedAt
	}
	writeJSON(w, http.StatusOK, cryptoKeyResource(updated.Name, updated.Purpose, updated.Algorithm, updated.LabelsJSON, updated.CreatedAt, primaryName, primaryState, primaryCreate))
}

func (s *Service) listCryptoKeys(w http.ResponseWriter, _ *http.Request, p authn.Principal, project, location, keyRing string) {
	if err := s.require(p, "cloudkms.cryptoKeys.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	krName := fmt.Sprintf("projects/%s/locations/%s/keyRings/%s", project, location, keyRing)
	list, err := s.Store.ListKMSCryptoKeys(krName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, k := range list {
		items = append(items, cryptoKeyResource(k.Name, k.Purpose, k.Algorithm, k.LabelsJSON, k.CreatedAt, "", "", ""))
	}
	writeJSON(w, http.StatusOK, map[string]any{"cryptoKeys": items})
}

func (s *Service) getIamPolicy(w http.ResponseWriter, _ *http.Request, p authn.Principal, project, location, keyRing, cryptoKey string) {
	if err := s.require(p, "cloudkms.cryptoKeys.getIamPolicy", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := fmt.Sprintf("projects/%s/locations/%s/keyRings/%s/cryptoKeys/%s", project, location, keyRing, cryptoKey)
	if _, ok, err := s.Store.GetKMSCryptoKey(name); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "CryptoKey not found")
		return
	}
	raw, found, err := s.Store.GetIAMPolicyJSON(name)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !found {
		writeJSON(w, http.StatusOK, authz.Policy{Etag: "ACAB", Bindings: []authz.Binding{}})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func (s *Service) setIamPolicy(w http.ResponseWriter, r *http.Request, p authn.Principal, project, location, keyRing, cryptoKey string) {
	if err := s.require(p, "cloudkms.cryptoKeys.setIamPolicy", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	name := fmt.Sprintf("projects/%s/locations/%s/keyRings/%s/cryptoKeys/%s", project, location, keyRing, cryptoKey)
	if _, ok, err := s.Store.GetKMSCryptoKey(name); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "CryptoKey not found")
		return
	}
	var req struct {
		Policy authz.Policy `json:"policy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		gcperrors.InvalidArgument(w, "invalid setIamPolicy body")
		return
	}
	if err := s.Store.PutIAMPolicyJSON(name, req.Policy); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, req.Policy)
}

func (s *Service) encrypt(w http.ResponseWriter, r *http.Request, p authn.Principal, project, location, keyRing, cryptoKey, version string) {
	if err := s.require(p, "cloudkms.cryptoKeyVersions.useToEncrypt", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	keyName := fmt.Sprintf("projects/%s/locations/%s/keyRings/%s/cryptoKeys/%s", project, location, keyRing, cryptoKey)
	k, ok, err := s.Store.GetKMSCryptoKey(keyName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "CryptoKey not found")
		return
	}
	if k.Purpose != store.KMSPurposeEncrypt {
		gcperrors.WriteREST(w, http.StatusBadRequest, gcperrors.StatusFailedPrecondition, "encrypt requires ENCRYPT_DECRYPT purpose")
		return
	}
	verName := keyName + "/cryptoKeyVersions/1"
	if version != "" {
		verName = keyName + "/cryptoKeyVersions/" + version
	}
	var body struct {
		Plaintext string `json:"plaintext"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	plain, err := base64.StdEncoding.DecodeString(body.Plaintext)
	if err != nil {
		gcperrors.InvalidArgument(w, "plaintext must be base64")
		return
	}
	v, ok, err := s.Store.GetKMSKeyVersion(verName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "CryptoKeyVersion not found")
		return
	}
	if v.State == store.KMSStateDestroyed {
		gcperrors.WriteREST(w, http.StatusBadRequest, gcperrors.StatusFailedPrecondition, "crypto key version is DESTROYED")
		return
	}
	material, err := s.Store.Unseal(v.KeyMaterialCiphertext)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	ct, err := aesGCMEncrypt(material, plain)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name": verName, "ciphertext": base64.StdEncoding.EncodeToString(ct),
	})
}

func (s *Service) decrypt(w http.ResponseWriter, r *http.Request, p authn.Principal, project, location, keyRing, cryptoKey, version string) {
	if err := s.require(p, "cloudkms.cryptoKeyVersions.useToDecrypt", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	keyName := fmt.Sprintf("projects/%s/locations/%s/keyRings/%s/cryptoKeys/%s", project, location, keyRing, cryptoKey)
	k, ok, err := s.Store.GetKMSCryptoKey(keyName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "CryptoKey not found")
		return
	}
	if k.Purpose != store.KMSPurposeEncrypt {
		gcperrors.WriteREST(w, http.StatusBadRequest, gcperrors.StatusFailedPrecondition, "decrypt requires ENCRYPT_DECRYPT purpose")
		return
	}
	verName := keyName + "/cryptoKeyVersions/1"
	if version != "" {
		verName = keyName + "/cryptoKeyVersions/" + version
	}
	var body struct {
		Ciphertext string `json:"ciphertext"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	ct, err := base64.StdEncoding.DecodeString(body.Ciphertext)
	if err != nil {
		gcperrors.InvalidArgument(w, "ciphertext must be base64")
		return
	}
	v, ok, err := s.Store.GetKMSKeyVersion(verName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "CryptoKeyVersion not found")
		return
	}
	if v.State == store.KMSStateDestroyed {
		gcperrors.WriteREST(w, http.StatusBadRequest, gcperrors.StatusFailedPrecondition, "crypto key version is DESTROYED")
		return
	}
	material, err := s.Store.Unseal(v.KeyMaterialCiphertext)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	plain, err := aesGCMDecrypt(material, ct)
	if err != nil {
		gcperrors.InvalidArgument(w, "decryption failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"plaintext": base64.StdEncoding.EncodeToString(plain),
	})
}

func (s *Service) getPublicKey(w http.ResponseWriter, _ *http.Request, p authn.Principal, project, location, keyRing, cryptoKey, version string) {
	if err := s.require(p, "cloudkms.cryptoKeyVersions.viewPublicKey", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	keyName := fmt.Sprintf("projects/%s/locations/%s/keyRings/%s/cryptoKeys/%s", project, location, keyRing, cryptoKey)
	k, ok, err := s.Store.GetKMSCryptoKey(keyName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "CryptoKey not found")
		return
	}
	if k.Purpose != store.KMSPurposeSign {
		gcperrors.WriteREST(w, http.StatusBadRequest, gcperrors.StatusFailedPrecondition,
			"GetPublicKey is only supported for ASYMMETRIC_SIGN keys; ENCRYPT_DECRYPT is symmetric")
		return
	}
	verName := keyName + "/cryptoKeyVersions/" + version
	v, ok, err := s.Store.GetKMSKeyVersion(verName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "CryptoKeyVersion not found")
		return
	}
	if v.State == store.KMSStateDestroyed {
		gcperrors.WriteREST(w, http.StatusBadRequest, gcperrors.StatusFailedPrecondition, "crypto key version is DESTROYED")
		return
	}
	material, err := s.Store.Unseal(v.KeyMaterialCiphertext)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	privAny, err := x509.ParsePKCS8PrivateKey(material)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	priv, ok := privAny.(*rsa.PrivateKey)
	if !ok {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "expected RSA private key")
		return
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	writeJSON(w, http.StatusOK, map[string]any{
		"pem": string(pemBytes), "algorithm": k.Algorithm, "name": verName, "protectionLevel": "SOFTWARE",
	})
}

func (s *Service) asymmetricSign(w http.ResponseWriter, r *http.Request, p authn.Principal, project, location, keyRing, cryptoKey, version string) {
	if err := s.require(p, "cloudkms.cryptoKeyVersions.useToSign", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	keyName := fmt.Sprintf("projects/%s/locations/%s/keyRings/%s/cryptoKeys/%s", project, location, keyRing, cryptoKey)
	k, ok, err := s.Store.GetKMSCryptoKey(keyName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "CryptoKey not found")
		return
	}
	if k.Purpose != store.KMSPurposeSign {
		gcperrors.WriteREST(w, http.StatusBadRequest, gcperrors.StatusFailedPrecondition, "asymmetricSign requires ASYMMETRIC_SIGN purpose")
		return
	}
	verName := keyName + "/cryptoKeyVersions/" + version
	var body struct {
		Digest *struct {
			SHA256 string `json:"sha256"`
		} `json:"digest"`
		Data string `json:"data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		gcperrors.InvalidArgument(w, "invalid JSON body")
		return
	}
	var digest []byte
	switch {
	case body.Digest != nil && body.Digest.SHA256 != "":
		var err error
		digest, err = base64.StdEncoding.DecodeString(body.Digest.SHA256)
		if err != nil {
			gcperrors.InvalidArgument(w, "digest.sha256 must be base64")
			return
		}
		if len(digest) != sha256.Size {
			gcperrors.InvalidArgument(w, "digest.sha256 must be 32 bytes")
			return
		}
	case body.Data != "":
		raw, err := base64.StdEncoding.DecodeString(body.Data)
		if err != nil {
			gcperrors.InvalidArgument(w, "data must be base64")
			return
		}
		sum := sha256.Sum256(raw)
		digest = sum[:]
	default:
		gcperrors.InvalidArgument(w, "digest.sha256 or data is required")
		return
	}
	v, ok, err := s.Store.GetKMSKeyVersion(verName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "CryptoKeyVersion not found")
		return
	}
	if v.State == store.KMSStateDestroyed {
		gcperrors.WriteREST(w, http.StatusBadRequest, gcperrors.StatusFailedPrecondition, "crypto key version is DESTROYED")
		return
	}
	material, err := s.Store.Unseal(v.KeyMaterialCiphertext)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	privAny, err := x509.ParsePKCS8PrivateKey(material)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	priv, ok := privAny.(*rsa.PrivateKey)
	if !ok {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, "expected RSA private key")
		return
	}
	sig, err := rsa.SignPSS(rand.Reader, priv, crypto.SHA256, digest, &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash})
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"signature": base64.StdEncoding.EncodeToString(sig),
		"name":      verName,
	})
}

func (s *Service) destroyVersion(w http.ResponseWriter, _ *http.Request, p authn.Principal, project, location, keyRing, cryptoKey, version string) {
	if err := s.require(p, "cloudkms.cryptoKeyVersions.destroy", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	verName := fmt.Sprintf("projects/%s/locations/%s/keyRings/%s/cryptoKeys/%s/cryptoKeyVersions/%s",
		project, location, keyRing, cryptoKey, version)
	v, ok, err := s.Store.DestroyKMSKeyVersion(verName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "CryptoKeyVersion not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name": v.Name, "state": v.State, "createTime": v.CreatedAt,
	})
}

func (s *Service) restoreVersion(w http.ResponseWriter, _ *http.Request, p authn.Principal, project, location, keyRing, cryptoKey, version string) {
	if err := s.require(p, "cloudkms.cryptoKeyVersions.restore", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	verName := fmt.Sprintf("projects/%s/locations/%s/keyRings/%s/cryptoKeys/%s/cryptoKeyVersions/%s",
		project, location, keyRing, cryptoKey, version)
	v, ok, err := s.Store.RestoreKMSKeyVersion(verName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "CryptoKeyVersion not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name": v.Name, "state": v.State, "createTime": v.CreatedAt,
	})
}

func (s *Service) listKeyVersions(w http.ResponseWriter, _ *http.Request, p authn.Principal, project, location, keyRing, cryptoKey string) {
	if err := s.require(p, "cloudkms.cryptoKeyVersions.list", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	keyName := fmt.Sprintf("projects/%s/locations/%s/keyRings/%s/cryptoKeys/%s", project, location, keyRing, cryptoKey)
	if _, ok, err := s.Store.GetKMSCryptoKey(keyName); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	} else if !ok {
		gcperrors.NotFound(w, "CryptoKey not found")
		return
	}
	list, err := s.Store.ListKMSKeyVersions(keyName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	items := make([]map[string]any, 0, len(list))
	for _, v := range list {
		items = append(items, map[string]any{
			"name": v.Name, "state": v.State, "createTime": v.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"cryptoKeyVersions": items})
}

func (s *Service) getKeyVersion(w http.ResponseWriter, _ *http.Request, p authn.Principal, project, location, keyRing, cryptoKey, version string) {
	if err := s.require(p, "cloudkms.cryptoKeyVersions.get", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	verName := fmt.Sprintf("projects/%s/locations/%s/keyRings/%s/cryptoKeys/%s/cryptoKeyVersions/%s",
		project, location, keyRing, cryptoKey, version)
	v, ok, err := s.Store.GetKMSKeyVersion(verName)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	if !ok {
		gcperrors.NotFound(w, "CryptoKeyVersion not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name": v.Name, "state": v.State, "createTime": v.CreatedAt,
	})
}

func writeAuthzErr(w http.ResponseWriter, err error) {
	if err == errDenied {
		gcperrors.PermissionDenied(w, "")
		return
	}
	gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
}

func aesGCMEncrypt(key, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, plaintext, nil), nil
}

func aesGCMDecrypt(key, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	ns := aead.NonceSize()
	if len(ciphertext) < ns {
		return nil, fmt.Errorf("ciphertext too short")
	}
	return aead.Open(nil, ciphertext[:ns], ciphertext[ns:], nil)
}
