package kms

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
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
	case r.Method == http.MethodGet && len(parts) == 5 && parts[0] == "keyRings" && parts[2] == "cryptoKeys" && parts[4] == "cryptoKeyVersions":
		s.listKeyVersions(w, r, p, project, location, parts[1], parts[3])
	case r.Method == http.MethodGet && len(parts) == 6 && parts[0] == "keyRings" && parts[2] == "cryptoKeys" && parts[4] == "cryptoKeyVersions":
		s.getKeyVersion(w, r, p, project, location, parts[1], parts[3], parts[5])
	case r.Method == http.MethodPost && len(parts) == 4 && parts[0] == "keyRings" && parts[2] == "cryptoKeys":
		key, action := splitAction(parts[3])
		switch action {
		case "encrypt":
			s.encrypt(w, r, p, project, location, parts[1], key, "")
		case "decrypt":
			s.decrypt(w, r, p, project, location, parts[1], key, "")
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
		Purpose string `json:"purpose"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	purpose := body.Purpose
	if purpose == "" {
		purpose = "ENCRYPT_DECRYPT"
	}
	if purpose != "ENCRYPT_DECRYPT" {
		gcperrors.InvalidArgument(w, "only ENCRYPT_DECRYPT is supported in this lab")
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
	material := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, material); err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	sealed, err := s.Store.Seal(material)
	if err != nil {
		gcperrors.WriteREST(w, http.StatusInternalServerError, gcperrors.StatusInternal, err.Error())
		return
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	created, err := s.Store.CreateKMSCryptoKey(
		store.KMSCryptoKey{Name: keyName, KeyRing: krName, Purpose: purpose, CreatedAt: now},
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
	writeJSON(w, http.StatusOK, map[string]any{
		"name": keyName, "purpose": purpose, "createTime": now,
		"primary": map[string]any{"name": verName, "state": "ENABLED", "createTime": now},
	})
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
	out := map[string]any{"name": k.Name, "purpose": k.Purpose, "createTime": k.CreatedAt}
	if pok {
		out["primary"] = map[string]any{
			"name": primary.Name, "state": primary.State, "createTime": primary.CreatedAt,
		}
	}
	writeJSON(w, http.StatusOK, out)
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
		items = append(items, map[string]any{"name": k.Name, "purpose": k.Purpose, "createTime": k.CreatedAt})
	}
	writeJSON(w, http.StatusOK, map[string]any{"cryptoKeys": items})
}

func (s *Service) encrypt(w http.ResponseWriter, r *http.Request, p authn.Principal, project, location, keyRing, cryptoKey, version string) {
	if err := s.require(p, "cloudkms.cryptoKeyVersions.useToEncrypt", project); err != nil {
		writeAuthzErr(w, err)
		return
	}
	keyName := fmt.Sprintf("projects/%s/locations/%s/keyRings/%s/cryptoKeys/%s", project, location, keyRing, cryptoKey)
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
