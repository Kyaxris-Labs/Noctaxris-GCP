package iam_test

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/authn"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/services/iam"
)

func TestCreateKeyEmitsRSAPEMNotBearer(t *testing.T) {
	h := openIAM(t)
	const project = "noctaxris-gcp-local"
	email := seedServiceAccount(t, h.store, project, "rsa-key")
	h.setWho("root@"+project+".iam.gserviceaccount.com", true)

	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/serviceAccounts/"+url.PathEscape(email)+"/keys", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create key status=%d body=%s", rec.Code, rec.Body.String())
	}
	var keyBody map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &keyBody); err != nil {
		t.Fatal(err)
	}
	b64, _ := keyBody["privateKeyData"].(string)
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatal(err)
	}
	var cred map[string]string
	if err := json.Unmarshal(raw, &cred); err != nil {
		t.Fatal(err)
	}
	if cred["type"] != "service_account" || cred["client_email"] != email {
		t.Fatalf("cred shape = %#v", cred)
	}
	if !strings.Contains(cred["private_key"], "BEGIN PRIVATE KEY") {
		preview := cred["private_key"]
		if len(preview) > 80 {
			preview = preview[:80]
		}
		t.Fatalf("private_key not PKCS#8 PEM: %q", preview)
	}
	if strings.HasPrefix(cred["private_key"], "ngsa_") {
		t.Fatal("private_key still looks like lab access token")
	}

	// PEM must not authenticate as Bearer.
	lookup, ok, err := h.store.LookupAccessToken(authn.HashToken(cred["private_key"]), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatalf("PEM registered as access token for %q", lookup)
	}
}

func TestOAuthJWTBearerGrant(t *testing.T) {
	h := openIAM(t)
	const project = "noctaxris-gcp-local"
	email := seedServiceAccount(t, h.store, project, "jwt-sa")
	h.setWho("root@"+project+".iam.gserviceaccount.com", true)

	cred := createSACredentials(t, h, project, email)
	priv := parsePKCS8PEM(t, cred["private_key"])
	now := time.Now().UTC()
	assertion := signSAJWT(t, priv, email, cred["private_key_id"], "https://oauth2.googleapis.com/token", now, now.Add(time.Hour))

	form := url.Values{}
	form.Set("grant_type", iam.GrantTypeJWTBearer)
	form.Set("assertion", assertion)
	form.Set("scope", "https://www.googleapis.com/auth/cloud-platform")
	req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("token status=%d body=%s", rec.Code, rec.Body.String())
	}
	var tok map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &tok); err != nil {
		t.Fatal(err)
	}
	access, _ := tok["access_token"].(string)
	if access == "" || tok["token_type"] != "Bearer" {
		t.Fatalf("token response = %#v", tok)
	}
	if int(tok["expires_in"].(float64)) != 3600 {
		t.Fatalf("expires_in = %#v", tok["expires_in"])
	}
	emailOut, ok, err := h.store.LookupAccessToken(authn.HashToken(access), time.Now().UTC())
	if err != nil || !ok || emailOut != email {
		t.Fatalf("lookup access token email=%q ok=%v err=%v", emailOut, ok, err)
	}

	// JSON body on /oauth2/token with lab audience path.
	assertion2 := signSAJWT(t, priv, email, cred["private_key_id"], "http://127.0.0.1:4588/oauth2/token", now, now.Add(30*time.Minute))
	body := `{"grant_type":"` + iam.GrantTypeJWTBearer + `","assertion":"` + assertion2 + `"}`
	req = httptest.NewRequest(http.MethodPost, "/oauth2/token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("oauth2/token status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOAuthJWTBearerRejects(t *testing.T) {
	h := openIAM(t)
	const project = "noctaxris-gcp-local"
	email := seedServiceAccount(t, h.store, project, "jwt-bad")
	h.setWho("root@"+project+".iam.gserviceaccount.com", true)
	cred := createSACredentials(t, h, project, email)
	priv := parsePKCS8PEM(t, cred["private_key"])
	now := time.Now().UTC()

	post := func(assertion string) *httptest.ResponseRecorder {
		t.Helper()
		form := url.Values{}
		form.Set("grant_type", iam.GrantTypeJWTBearer)
		form.Set("assertion", assertion)
		req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.mux.ServeHTTP(rec, req)
		return rec
	}

	badGrant := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader("grant_type=client_credentials&assertion=x"))
	badGrant.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, badGrant)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad grant status=%d", rec.Code)
	}

	expired := signSAJWT(t, priv, email, cred["private_key_id"], "https://oauth2.googleapis.com/token", now.Add(-2*time.Hour), now.Add(-time.Hour))
	if rec = post(expired); rec.Code != http.StatusUnauthorized {
		t.Fatalf("expired status=%d body=%s", rec.Code, rec.Body.String())
	}

	farIat := signSAJWT(t, priv, email, cred["private_key_id"], "https://oauth2.googleapis.com/token", now.Add(time.Hour), now.Add(2*time.Hour))
	if rec = post(farIat); rec.Code != http.StatusUnauthorized {
		t.Fatalf("far iat status=%d body=%s", rec.Code, rec.Body.String())
	}

	badAud := signSAJWT(t, priv, email, cred["private_key_id"], "https://evil.example/token", now, now.Add(time.Hour))
	if rec = post(badAud); rec.Code != http.StatusUnauthorized {
		t.Fatalf("bad aud status=%d body=%s", rec.Code, rec.Body.String())
	}

	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	wrongKey := signSAJWT(t, other, email, cred["private_key_id"], "https://oauth2.googleapis.com/token", now, now.Add(time.Hour))
	if rec = post(wrongKey); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong key status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func createSACredentials(t *testing.T, h *iamTestHarness, project, email string) map[string]string {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/projects/"+project+"/serviceAccounts/"+url.PathEscape(email)+"/keys", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create key status=%d body=%s", rec.Code, rec.Body.String())
	}
	var keyBody map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &keyBody); err != nil {
		t.Fatal(err)
	}
	b64, _ := keyBody["privateKeyData"].(string)
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatal(err)
	}
	var cred map[string]string
	if err := json.Unmarshal(raw, &cred); err != nil {
		t.Fatal(err)
	}
	return cred
}

func parsePKCS8PEM(t *testing.T, pemStr string) *rsa.PrivateKey {
	t.Helper()
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		t.Fatal("no PEM")
	}
	keyAny, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	priv, ok := keyAny.(*rsa.PrivateKey)
	if !ok {
		t.Fatal("not RSA")
	}
	return priv
}

func signSAJWT(t *testing.T, priv *rsa.PrivateKey, email, kid, aud string, iat, exp time.Time) string {
	t.Helper()
	header := map[string]string{"alg": "RS256", "typ": "JWT", "kid": kid}
	claims := map[string]any{
		"iss": email,
		"sub": email,
		"aud": aud,
		"iat": iat.Unix(),
		"exp": exp.Unix(),
	}
	hb, _ := json.Marshal(header)
	cb, _ := json.Marshal(claims)
	signingInput := base64.RawURLEncoding.EncodeToString(hb) + "." + base64.RawURLEncoding.EncodeToString(cb)
	sum := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, priv, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig)
}
