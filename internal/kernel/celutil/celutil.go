// Package celutil evaluates lab CEL with cel-go (IAM conditions, WIF mapping, Armor).
//
// Fail closed on compile, type-check, or evaluation errors.
package celutil

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"cel.dev/cel-go/cel"
)

var (
	requestTimeEnvOnce sync.Once
	requestTimeEnv     *cel.Env
	requestTimeEnvErr  error

	assertionEnvOnce sync.Once
	assertionEnv     *cel.Env
	assertionEnvErr  error

	armorEnvOnce sync.Once
	armorEnv     *cel.Env
	armorEnvErr  error
)

func requestEnv() (*cel.Env, error) {
	requestTimeEnvOnce.Do(func() {
		requestTimeEnv, requestTimeEnvErr = cel.NewEnv(
			cel.Variable("request", cel.MapType(cel.StringType, cel.DynType)),
		)
	})
	return requestTimeEnv, requestTimeEnvErr
}

func getAssertionEnv() (*cel.Env, error) {
	assertionEnvOnce.Do(func() {
		assertionEnv, assertionEnvErr = cel.NewEnv(
			cel.Variable("assertion", cel.MapType(cel.StringType, cel.DynType)),
		)
	})
	return assertionEnv, assertionEnvErr
}

func getArmorEnv() (*cel.Env, error) {
	armorEnvOnce.Do(func() {
		armorEnv, armorEnvErr = cel.NewEnv(
			cel.Variable("request", cel.MapType(cel.StringType, cel.DynType)),
		)
	})
	return armorEnv, armorEnvErr
}

// EvalRequestTime evaluates expr with request.time bound to now (UTC).
// Empty expressions allow (no condition). Errors and non-bool results deny.
func EvalRequestTime(expr string, now time.Time) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return true
	}
	env, err := requestEnv()
	if err != nil || env == nil {
		return false
	}
	ast, issues := env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return false
	}
	prg, err := env.Program(ast)
	if err != nil {
		return false
	}
	out, _, err := prg.Eval(map[string]any{
		"request": map[string]any{
			"time": now.UTC(),
		},
	})
	if err != nil || out == nil {
		return false
	}
	v, ok := out.Value().(bool)
	if !ok {
		return false
	}
	return v
}

// EvalAttributeMapping evaluates WIF attributeMapping expressions against assertion claims.
// Each mapping value is a CEL expression; results are stringified.
// Fail closed on any compile/eval error. Requires non-empty google.subject in the output.
func EvalAttributeMapping(mapping map[string]string, assertion map[string]any) (map[string]string, error) {
	if len(mapping) == 0 {
		return nil, fmt.Errorf("cel: empty attributeMapping")
	}
	env, err := getAssertionEnv()
	if err != nil {
		return nil, err
	}
	if assertion == nil {
		assertion = map[string]any{}
	}
	vars := map[string]any{"assertion": assertion}
	out := make(map[string]string, len(mapping))
	for key, expr := range mapping {
		expr = strings.TrimSpace(expr)
		if expr == "" {
			return nil, fmt.Errorf("cel: empty mapping for %s", key)
		}
		v, err := evalString(env, expr, vars)
		if err != nil {
			return nil, fmt.Errorf("cel: mapping %s: %w", key, err)
		}
		out[key] = v
	}
	if strings.TrimSpace(out["google.subject"]) == "" {
		return nil, fmt.Errorf("cel: google.subject required")
	}
	return out, nil
}

// EvalAttributeCondition evaluates optional WIF attributeCondition. Empty allows.
func EvalAttributeCondition(expr string, assertion map[string]any) (bool, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return true, nil
	}
	env, err := getAssertionEnv()
	if err != nil {
		return false, err
	}
	if assertion == nil {
		assertion = map[string]any{}
	}
	ast, issues := env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return false, issues.Err()
	}
	prg, err := env.Program(ast)
	if err != nil {
		return false, err
	}
	out, _, err := prg.Eval(map[string]any{"assertion": assertion})
	if err != nil {
		return false, err
	}
	if out == nil {
		return false, fmt.Errorf("cel: nil condition result")
	}
	v, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("cel: condition must be bool")
	}
	return v, nil
}

// EvalArmorMatch evaluates a Cloud Armor CEL expression over request attributes
// (host, path, headers). Empty expression matches. Eval/compile errors are non-match.
func EvalArmorMatch(expr string, attrs map[string]any) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return true
	}
	env, err := getArmorEnv()
	if err != nil || env == nil {
		return false
	}
	ast, issues := env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return false
	}
	prg, err := env.Program(ast)
	if err != nil {
		return false
	}
	if attrs == nil {
		attrs = map[string]any{}
	}
	out, _, err := prg.Eval(map[string]any{"request": attrs})
	if err != nil || out == nil {
		return false
	}
	v, ok := out.Value().(bool)
	if !ok {
		return false
	}
	return v
}

func evalString(env *cel.Env, expr string, vars map[string]any) (string, error) {
	ast, issues := env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return "", issues.Err()
	}
	prg, err := env.Program(ast)
	if err != nil {
		return "", err
	}
	out, _, err := prg.Eval(vars)
	if err != nil {
		return "", err
	}
	if out == nil {
		return "", fmt.Errorf("nil result")
	}
	switch v := out.Value().(type) {
	case string:
		return v, nil
	default:
		return fmt.Sprint(v), nil
	}
}
