package iam

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/celutil"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/jwtutil"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// resolveWIFSubject applies attributeCondition and attributeMapping (CEL) to
// assertion claims and returns the sanitized google.subject for the WIF principal.
// Empty mapping falls back to assertion.sub when present, else theatre subject_token.
func resolveWIFSubject(prov store.WorkloadIdentityPoolProvider, assertion map[string]any, subjectToken string) (string, error) {
	if assertion == nil {
		assertion = map[string]any{}
	}
	ok, err := celutil.EvalAttributeCondition(prov.AttributeCondition, assertion)
	if err != nil {
		return "", fmt.Errorf("attributeCondition: %w", err)
	}
	if !ok {
		return "", fmt.Errorf("attributeCondition denied")
	}

	mapping, err := parseAttributeMapping(prov.AttributeMap)
	if err != nil {
		return "", err
	}
	if len(mapping) == 0 {
		if sub := strings.TrimSpace(claimString(assertion, "sub")); sub != "" {
			return labSubjectFromToken(sub), nil
		}
		return labSubjectFromToken(subjectToken), nil
	}
	attrs, err := celutil.EvalAttributeMapping(mapping, assertion)
	if err != nil {
		return "", err
	}
	return labSubjectFromToken(attrs["google.subject"]), nil
}

func parseAttributeMapping(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return nil, nil
	}
	var mapping map[string]string
	if err := json.Unmarshal([]byte(raw), &mapping); err != nil {
		return nil, fmt.Errorf("invalid attributeMapping: %w", err)
	}
	if len(mapping) == 0 {
		return nil, nil
	}
	return mapping, nil
}

// assertionClaimsFromSubjectToken builds assertion.* claims for CEL.
// When the token is a compact JWT, the payload is decoded (unverified for theatre;
// STS verify replaces claims with the verified map before calling resolveWIFSubject).
func assertionClaimsFromSubjectToken(subjectToken string) map[string]any {
	claims, err := jwtutil.DecodeCompactClaimsUnverified(subjectToken)
	if err != nil || claims == nil {
		return map[string]any{}
	}
	return claims
}
