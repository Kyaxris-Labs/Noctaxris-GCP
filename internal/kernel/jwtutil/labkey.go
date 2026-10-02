package jwtutil

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"sync"
)

// LabOIDCKid is the stable key id published by the lab oidc-lab JWKS.
const LabOIDCKid = "noctaxris-gcp-oidc-lab"

// labOIDCPrivateKeyPEM is a fixed in-process RSA key for lab OIDC / push tokens.
const labOIDCPrivateKeyPEM = `-----BEGIN RSA PRIVATE KEY-----
MIIEpAIBAAKCAQEAyZPcBwc0jZyoaEvE3Irp0Aaismc+PzVkz8J0viX7jIjSqA4r
MRy68gJlduMnOcxKFre/SJodEvDvUi/yl7EmcC2AAtxeRnJSsHmKcoSs5nwSg2YO
2t3rzj2vxVrzGsUkDHlYhpwwtbd1pO0w/MmTBGgZz/VjmmPgjcTqxY+d3V0PwnCK
FxvaXNuc0edaBa5y2W1OTtd8sDO1YGMgPQP2aiGEiqqKIkChY3/zD3DZtUzCLiA6
LW8U3T9pMs4uc87CnnX/Y31go6Sq+oL8ZLeG+jrbPqzx2/QqMEDKZhCRT0lrAAEN
ezqXy9QhodyQlLbC+NOAxxqra9Rlth6YvoUeaQIDAQABAoIBABLxd8BwqhIoJ4q2
8CGlAP22hJdNF4RGJsVVf2DCiLC3cksucFsAWbCA5CXM3rSnoPYglMkkXoNsq/jm
c57+HArNDoDLp8++p6GuUldFEPXksV1de8Y6tmcef0RbMd8RVBFuB+aNNWX7qdfI
HeNA/YrGTlgE4LQzcE4HtDkWWt4LWXRqV0oBDh3yA+8YDqEY+pyXR5506YFQJ4JM
y3/NB7wayarKeOMvyZQ6REfwsdG6o6yYj8/85flDjRS9iaM9Y9XQ8CCZZJrB8b/A
3YWZEyy6wKgHI1IC1Q5OLTPFBxcKT999/s+aKGBFmPO5yEy7hQ7d8FeTUPYRxIYc
Rq6LiKcCgYEA+KTuchwcKEh/OxGTQ6XOKSERVgsJA/Tt+gIxKHBlor9QB9ivUzFn
HTytjm0YjOdEqorBwgaXLEovv311ZSOC3FWEUwNc3R8fSc9d7j1Svx+DPa7G1Bl4
ae96rzXiAo/wmCsIb/Bn43olF0O6kL9L7TiLIpG0+UND4I0hyfZGb1sCgYEAz4p5
0EAYbfIFhvEQuhHxFDit0akk11Aw4UO0bqPsf2rI0rTfDY4RJgt90bPWoxCksovI
wFRe2unGP9j/tTnApjc7e83AGWD1gSG5qEM0VnEe/RyE9FGeGk7CGEDKumOmGVRg
m9AvR8dL9v3FuJhmB2ZcbAderTeBSgXy0dZ8eIsCgYEAsi/QSapna174/uXLeXE7
WzI9cEIcRd+jI8WqYOabj5Q20Eiy7JW85bD0V9tK+r9J8EXcMSXz9GN98GcCWGao
gyot2CfSxwxkqcqX8AG2aQ02SmAUUS+noZNjgmjE/T0WGJbORxor+VMxfYimDNFq
oighXba50OAppqS9kDSTqX0CgYEAitgVTmDS9xrm37P+gLzoD6MrhgwmfXVEfi+R
UkOQQF3sJCqk3qigiFc/wT8S5NyJknk5wJGxM7sZyjUePNt6Krjgrp6jWVcoZ09s
qUjshrf/B05BFEJWBzuRVjBib/eic2ejihnox5hpFcAIusoZ1/F++zai/DcZ46+/
FurrMqkCgYBo/Po7fjpyVjYwAOue8TOByzzu2+8SfQnreaFFXM9S1g3l6uALemKH
qHdxLP3dGLbHUK9c/f3XVy2EWnjw24Dk7lmVdNJPr/5gg/jCi4hzp7RTGImZ5rmy
oF3IzvPPtfypNh/Ex7VO8n4XYEzR1T3emupnFYE3mO6N8ImlOpYFjQ==
-----END RSA PRIVATE KEY-----`

var (
	labOIDCKeyOnce sync.Once
	labOIDCKey     *rsa.PrivateKey
	labOIDCKeyErr  error
)

// LabOIDCPrivateKey returns the stable lab OIDC RSA signing key.
func LabOIDCPrivateKey() (*rsa.PrivateKey, error) {
	labOIDCKeyOnce.Do(func() {
		block, _ := pem.Decode([]byte(labOIDCPrivateKeyPEM))
		if block == nil {
			labOIDCKeyErr = x509.ErrUnsupportedAlgorithm
			return
		}
		key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			labOIDCKeyErr = err
			return
		}
		labOIDCKey = key
	})
	return labOIDCKey, labOIDCKeyErr
}

// SignLabOIDCRS256 signs claims with the lab OIDC RSA key (RS256, kid set).
// Rejects unsigned theatre: callers must not mint alg=none tokens.
func SignLabOIDCRS256(claims map[string]any) (string, error) {
	key, err := LabOIDCPrivateKey()
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", fmt.Errorf("jwt: claims: %w", err)
	}
	return SignRS256(payload, key, LabOIDCKid)
}

// MarshalLabOIDCJWKS returns JWKS JSON for the lab OIDC public key.
func MarshalLabOIDCJWKS() ([]byte, error) {
	key, err := LabOIDCPrivateKey()
	if err != nil {
		return nil, err
	}
	return MarshalJWKS(&key.PublicKey, LabOIDCKid)
}
