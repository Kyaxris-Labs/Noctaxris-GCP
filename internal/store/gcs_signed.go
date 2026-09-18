package store

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Lab GCS V4 HMAC theatre credentials (not Google Cloud HMAC keys).
const (
	LabGCSHMACAccessID = "noctaxris-gcp-lab"
	LabGCSHMACSecret   = "noctaxris-gcp-lab-hmac-secret"
	LabGCSSignAlgo     = "GOOG4-HMAC-SHA256"
)

// SignedURLRequest describes a V4 signed URL to mint.
type SignedURLRequest struct {
	Method  string
	Host    string // e.g. 127.0.0.1:4588
	Path    string // absolute path beginning with /
	Expires int    // seconds
	Now     time.Time
	Query   url.Values // extra query params included in signature (e.g. alt=media)
}

// GenerateV4SignedURL builds a lab GOOG4-HMAC-SHA256 signed URL for path on host.
func GenerateV4SignedURL(req SignedURLRequest) (string, error) {
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = "GET"
	}
	if req.Path == "" || !strings.HasPrefix(req.Path, "/") {
		return "", fmt.Errorf("path must be absolute")
	}
	expires := req.Expires
	if expires <= 0 {
		expires = 900
	}
	if expires > 604800 {
		return "", fmt.Errorf("expires must be <= 604800")
	}
	now := req.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	datestamp := now.Format("20060102")
	timestamp := now.Format("20060102T150405Z")
	credentialScope := datestamp + "/auto/storage/goog4_request"
	credential := LabGCSHMACAccessID + "/" + credentialScope

	q := url.Values{}
	for k, vs := range req.Query {
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	q.Set("X-Goog-Algorithm", LabGCSSignAlgo)
	q.Set("X-Goog-Credential", credential)
	q.Set("X-Goog-Date", timestamp)
	q.Set("X-Goog-Expires", strconv.Itoa(expires))
	q.Set("X-Goog-SignedHeaders", "host")

	canonicalQuery := canonicalQueryString(q)
	canonicalHeaders := "host:" + strings.ToLower(req.Host) + "\n"
	canonicalRequest := strings.Join([]string{
		method,
		req.Path,
		canonicalQuery,
		canonicalHeaders,
		"host",
		"UNSIGNED-PAYLOAD",
	}, "\n")
	hashed := sha256Hex([]byte(canonicalRequest))
	stringToSign := strings.Join([]string{
		LabGCSSignAlgo,
		timestamp,
		credentialScope,
		hashed,
	}, "\n")
	sig := hex.EncodeToString(hmacSHA256(deriveSigningKey(LabGCSHMACSecret, datestamp), []byte(stringToSign)))
	q.Set("X-Goog-Signature", sig)

	return "http://" + req.Host + req.Path + "?" + canonicalQueryString(q), nil
}

// VerifyV4SignedRequest validates a lab HMAC V4 signed request against host/path/method.
func VerifyV4SignedRequest(method, host, path string, query url.Values, now time.Time) error {
	algo := query.Get("X-Goog-Algorithm")
	if algo == "" {
		algo = query.Get("x-goog-algorithm")
	}
	if !strings.EqualFold(algo, LabGCSSignAlgo) {
		return fmt.Errorf("unsupported or missing X-Goog-Algorithm")
	}
	credential := firstQuery(query, "X-Goog-Credential", "x-goog-credential")
	date := firstQuery(query, "X-Goog-Date", "x-goog-date")
	expiresStr := firstQuery(query, "X-Goog-Expires", "x-goog-expires")
	signedHeaders := strings.ToLower(firstQuery(query, "X-Goog-SignedHeaders", "x-goog-signedheaders"))
	signature := strings.ToLower(firstQuery(query, "X-Goog-Signature", "x-goog-signature"))
	if credential == "" || date == "" || expiresStr == "" || signedHeaders == "" || signature == "" {
		return fmt.Errorf("missing V4 signature query parameters")
	}
	if !strings.Contains(signedHeaders, "host") {
		return fmt.Errorf("X-Goog-SignedHeaders must include host")
	}
	expires, err := strconv.Atoi(expiresStr)
	if err != nil || expires <= 0 {
		return fmt.Errorf("invalid X-Goog-Expires")
	}
	active, err := time.Parse("20060102T150405Z", date)
	if err != nil {
		return fmt.Errorf("invalid X-Goog-Date")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if now.After(active.Add(time.Duration(expires) * time.Second)) {
		return fmt.Errorf("signed URL expired")
	}
	// Allow 15 minutes clock skew before active time (matches Cloud Storage docs).
	if now.Before(active.Add(-15 * time.Minute)) {
		return fmt.Errorf("signed URL not yet valid")
	}

	parts := strings.SplitN(credential, "/", 2)
	if len(parts) != 2 || parts[0] != LabGCSHMACAccessID {
		return fmt.Errorf("unknown X-Goog-Credential access id")
	}
	credentialScope := parts[1]
	scopeParts := strings.Split(credentialScope, "/")
	if len(scopeParts) != 4 || scopeParts[2] != "storage" || scopeParts[3] != "goog4_request" {
		return fmt.Errorf("invalid credential scope")
	}
	datestamp := scopeParts[0]

	q := url.Values{}
	for k, vs := range query {
		lk := strings.ToLower(k)
		if lk == "x-goog-signature" {
			continue
		}
		for _, v := range vs {
			q.Add(k, v)
		}
	}
	// Normalize goog param casing used when signing.
	canonicalQuery := canonicalQueryString(q)
	canonicalHeaders := "host:" + strings.ToLower(host) + "\n"
	canonicalRequest := strings.Join([]string{
		strings.ToUpper(method),
		path,
		canonicalQuery,
		canonicalHeaders,
		"host",
		"UNSIGNED-PAYLOAD",
	}, "\n")
	hashed := sha256Hex([]byte(canonicalRequest))
	stringToSign := strings.Join([]string{
		LabGCSSignAlgo,
		date,
		credentialScope,
		hashed,
	}, "\n")
	want := hex.EncodeToString(hmacSHA256(deriveSigningKey(LabGCSHMACSecret, datestamp), []byte(stringToSign)))
	if !hmac.Equal([]byte(want), []byte(signature)) {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}

// HasV4Signature reports whether query looks like a GCS V4 signed request.
func HasV4Signature(query url.Values) bool {
	algo := firstQuery(query, "X-Goog-Algorithm", "x-goog-algorithm")
	sig := firstQuery(query, "X-Goog-Signature", "x-goog-signature")
	return algo != "" && sig != ""
}

// SignGOOG4HMACHeader builds Authorization + x-goog-date for XML HMAC requests.
func SignGOOG4HMACHeader(method, host, path, accessID, secret string, now time.Time) (authorization, googDate string) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	datestamp := now.Format("20060102")
	timestamp := now.Format("20060102T150405Z")
	credentialScope := datestamp + "/auto/storage/goog4_request"
	credential := accessID + "/" + credentialScope
	signedHeaders := "host;x-goog-content-sha256;x-goog-date"
	canonicalHeaders := "host:" + strings.ToLower(host) + "\n" +
		"x-goog-content-sha256:UNSIGNED-PAYLOAD\n" +
		"x-goog-date:" + timestamp + "\n"
	canonicalRequest := strings.Join([]string{
		strings.ToUpper(method),
		path,
		"",
		canonicalHeaders,
		signedHeaders,
		"UNSIGNED-PAYLOAD",
	}, "\n")
	hashed := sha256Hex([]byte(canonicalRequest))
	stringToSign := strings.Join([]string{
		LabGCSSignAlgo,
		timestamp,
		credentialScope,
		hashed,
	}, "\n")
	sig := hex.EncodeToString(hmacSHA256(deriveSigningKey(secret, datestamp), []byte(stringToSign)))
	auth := LabGCSSignAlgo + " Credential=" + credential + ", SignedHeaders=" + signedHeaders + ", Signature=" + sig
	return auth, timestamp
}

// VerifyGOOG4HMACHeader validates a GOOG4 Authorization header. Skew uses wall now.
func VerifyGOOG4HMACHeader(method, host, path, authorization, googDate, secret string, now time.Time) error {
	authorization = strings.TrimSpace(authorization)
	if !strings.HasPrefix(authorization, LabGCSSignAlgo+" ") {
		return fmt.Errorf("missing GOOG4-HMAC-SHA256 Authorization")
	}
	rest := strings.TrimSpace(strings.TrimPrefix(authorization, LabGCSSignAlgo+" "))
	parts := strings.Split(rest, ",")
	var credential, signedHeaders, signature string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		switch {
		case strings.HasPrefix(p, "Credential="):
			credential = strings.TrimPrefix(p, "Credential=")
		case strings.HasPrefix(p, "SignedHeaders="):
			signedHeaders = strings.ToLower(strings.TrimPrefix(p, "SignedHeaders="))
		case strings.HasPrefix(p, "Signature="):
			signature = strings.ToLower(strings.TrimPrefix(p, "Signature="))
		}
	}
	if credential == "" || signedHeaders == "" || signature == "" {
		return fmt.Errorf("incomplete Authorization")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if googDate == "" {
		return fmt.Errorf("missing x-goog-date")
	}
	active, err := time.Parse("20060102T150405Z", googDate)
	if err != nil {
		return fmt.Errorf("invalid x-goog-date")
	}
	if now.After(active.Add(15 * time.Minute)) {
		return fmt.Errorf("request expired")
	}
	if now.Before(active.Add(-15 * time.Minute)) {
		return fmt.Errorf("request not yet valid")
	}
	scopeParts := strings.SplitN(credential, "/", 2)
	if len(scopeParts) != 2 {
		return fmt.Errorf("invalid credential")
	}
	credentialScope := scopeParts[1]
	dsParts := strings.Split(credentialScope, "/")
	if len(dsParts) != 4 || dsParts[2] != "storage" {
		return fmt.Errorf("invalid credential scope")
	}
	datestamp := dsParts[0]
	canonicalHeaders := "host:" + strings.ToLower(host) + "\n" +
		"x-goog-content-sha256:UNSIGNED-PAYLOAD\n" +
		"x-goog-date:" + googDate + "\n"
	canonicalRequest := strings.Join([]string{
		strings.ToUpper(method),
		path,
		"",
		canonicalHeaders,
		signedHeaders,
		"UNSIGNED-PAYLOAD",
	}, "\n")
	hashed := sha256Hex([]byte(canonicalRequest))
	stringToSign := strings.Join([]string{
		LabGCSSignAlgo,
		googDate,
		credentialScope,
		hashed,
	}, "\n")
	want := hex.EncodeToString(hmacSHA256(deriveSigningKey(secret, datestamp), []byte(stringToSign)))
	if !hmac.Equal([]byte(want), []byte(signature)) {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}

func GOOG4AccessID(authorization string) string {
	authorization = strings.TrimSpace(authorization)
	idx := strings.Index(authorization, "Credential=")
	if idx < 0 {
		return ""
	}
	rest := authorization[idx+len("Credential="):]
	if i := strings.IndexByte(rest, '/'); i > 0 {
		return rest[:i]
	}
	if i := strings.IndexByte(rest, ','); i > 0 {
		return rest[:i]
	}
	return rest
}

func deriveSigningKey(secret, datestamp string) []byte {
	kDate := hmacSHA256([]byte("GOOG4"+secret), []byte(datestamp))
	kRegion := hmacSHA256(kDate, []byte("auto"))
	kService := hmacSHA256(kRegion, []byte("storage"))
	return hmacSHA256(kService, []byte("goog4_request"))
}

func hmacSHA256(key, data []byte) []byte {
	m := hmac.New(sha256.New, key)
	_, _ = m.Write(data)
	return m.Sum(nil)
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func canonicalQueryString(q url.Values) string {
	if len(q) == 0 {
		return ""
	}
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		vs := q[k]
		sort.Strings(vs)
		ek := url.QueryEscape(k)
		ek = strings.ReplaceAll(ek, "+", "%20")
		for _, v := range vs {
			ev := url.QueryEscape(v)
			ev = strings.ReplaceAll(ev, "+", "%20")
			parts = append(parts, ek+"="+ev)
		}
	}
	return strings.Join(parts, "&")
}

func firstQuery(q url.Values, keys ...string) string {
	for _, k := range keys {
		if v := q.Get(k); v != "" {
			return v
		}
	}
	return ""
}
