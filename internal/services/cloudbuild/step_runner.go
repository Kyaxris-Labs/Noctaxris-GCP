package cloudbuild

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/compute"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/httpegress"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/kernel/labtoken"
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// StepRunner executes a persisted WORKING build. Tests inject an implementation.
type StepRunner interface {
	Run(ctx context.Context, build store.CbBuild) error
}

// BuildStep is one Cloud Build step from the request JSON.
type BuildStep struct {
	Image     string
	Args      []string
	Env       []string
	Script    string
	SecretEnv []string
}

// EngineRunner runs steps on the nested engine (same family as Cloud Run :invoke).
// ExecuteStep, when set, skips Docker and is used by unit tests after egress checks.
// The returned string is treated as step stdout for buffered build logs (never log secret values).
type EngineRunner struct {
	Store       *store.Store
	Authz       authzEvaluator
	Invoker     compute.Invoker
	DockerHost  string
	TLSCertDir  string
	ExecuteStep func(ctx context.Context, step BuildStep) (stdout string, err error)
}

// authzEvaluator is the subset of authz.Evaluator used for build-SA Secret Manager access.
type authzEvaluator interface {
	Evaluate(principalEmail string, isRoot bool, permission, resource string) (bool, error)
	EvaluateAny(principalEmail string, isRoot bool, permission string, resources ...string) (bool, error)
}

var httpURLRe = regexp.MustCompile(`https?://[^\s"'\\<>]+`)

func (s *Service) runSteps(ctx context.Context, b store.CbBuild) {
	if b.Name == "" {
		return
	}
	_ = s.runner().Run(ctx, b)
}

func (s *Service) runner() StepRunner {
	if s != nil && s.StepRunner != nil {
		return s.StepRunner
	}
	var inv compute.Invoker
	host, cert := "", ""
	if s != nil {
		inv = s.Invoker
	}
	if inv == nil {
		inv = compute.NewInvokerFromEnv()
	}
	if d, ok := inv.(compute.DockerInvoker); ok {
		host = strings.TrimSpace(d.Host)
		cert = strings.TrimSpace(d.TLSCertDir)
	} else {
		host = strings.TrimSpace(os.Getenv(compute.EnvDockerHost))
		cert = strings.TrimSpace(os.Getenv(compute.EnvDockerCertPath))
	}
	var st *store.Store
	var az authzEvaluator
	if s != nil {
		st = s.Store
		az = s.Authz
	}
	return &EngineRunner{Store: st, Authz: az, Invoker: inv, DockerHost: host, TLSCertDir: cert}
}

func (r *EngineRunner) Run(ctx context.Context, build store.CbBuild) error {
	if r == nil || r.Store == nil {
		return nil
	}
	cur, ok, err := r.Store.GetCbBuild(build.Name)
	if err != nil {
		return err
	}
	if !ok || !isActiveBuildStatus(cur.Status) {
		return nil
	}
	if err := r.enforcePrivatePoolEgress(cur); err != nil {
		return r.failBuild(cur, err.Error())
	}
	if r.ExecuteStep == nil && !r.engineReady() {
		_, _, err := r.Store.PutCbBuildProgress(cur.Name, "WORKING", "nested engine not configured", "", "")
		return err
	}
	steps := parseBuildSteps(cur.BuildJSON)
	buildSA := parseBuildServiceAccountEmail(cur.BuildJSON, cur.ProjectID)
	secretVals, err := r.resolveStepSecrets(cur.BuildJSON, steps, buildSA)
	if err != nil {
		return r.failBuild(cur, err.Error())
	}
	token, extraHosts, err := r.buildStepIdentity(cur)
	if err != nil {
		return r.failBuild(cur, err.Error())
	}
	for i, step := range steps {
		if err := ctx.Err(); err != nil {
			return r.failBuild(cur, err.Error())
		}
		live, ok, err := r.Store.GetCbBuild(cur.Name)
		if err != nil {
			return err
		}
		if !ok || !isActiveBuildStatus(live.Status) {
			return nil
		}
		cur = live
		step, err = withSecretEnv(step, secretVals)
		if err != nil {
			cur.BuildJSON = markStepStatusAt(cur.BuildJSON, i, "FAILURE")
			return r.failBuild(cur, err.Error())
		}
		step = withBuildIdentityEnv(step, token, extraHosts)
		stdout, err := r.runOneStep(ctx, step, extraHosts)
		if err != nil {
			_ = r.Store.AppendCbBuildLogs(cur.Name, fmt.Sprintf("=== Step %d ===\n%s\n", i, stdout))
			cur.BuildJSON = markStepStatusAt(cur.BuildJSON, i, "FAILURE")
			return r.failBuild(cur, err.Error())
		}
		if err := r.Store.AppendCbBuildLogs(cur.Name, fmt.Sprintf("=== Step %d ===\n%s\n", i, stdout)); err != nil {
			return err
		}
		cur.BuildJSON = markStepStatusAt(cur.BuildJSON, i, "SUCCESS")
		if _, _, err := r.Store.PutCbBuildProgress(cur.Name, "WORKING", "running steps", cur.BuildJSON, ""); err != nil {
			return err
		}
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	jsonBody := store.MarkCbBuildStepsStatus(cur.BuildJSON, "SUCCESS")
	_, _, err = r.Store.PutCbBuildProgress(cur.Name, "SUCCESS", "steps executed", jsonBody, now)
	return err
}

func (r *EngineRunner) runOneStep(ctx context.Context, step BuildStep, extraHosts []string) (string, error) {
	if r.ExecuteStep != nil {
		return r.ExecuteStep(ctx, step)
	}
	host, cert := r.engineDial()
	cli, err := compute.Dial(host, cert)
	if err != nil {
		return "", fmt.Errorf("nested engine not configured: %w", err)
	}
	defer cli.Close()
	if !cli.Enabled() {
		return "", fmt.Errorf("nested engine not configured")
	}
	res, err := cli.RunBuildStep(ctx, compute.BuildStepRun{
		Image:      step.Image,
		Cmd:        step.Args,
		Env:        step.Env,
		Script:     step.Script,
		ExtraHosts: extraHosts,
	})
	if err != nil {
		return res.Stdout, err
	}
	if res.ExitCode != 0 {
		return res.Stdout, fmt.Errorf("step exit %d", res.ExitCode)
	}
	return res.Stdout, nil
}

type secretManagerBinding struct {
	Env         string
	VersionName string
}

func parseAvailableSecrets(buildJSON string) []secretManagerBinding {
	var cfg map[string]any
	if err := json.Unmarshal([]byte(buildJSON), &cfg); err != nil || cfg == nil {
		return nil
	}
	avail, _ := cfg["availableSecrets"].(map[string]any)
	if avail == nil {
		return nil
	}
	list, _ := avail["secretManager"].([]any)
	out := make([]secretManagerBinding, 0, len(list))
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		env := stringField(m["env"])
		ver := stringField(m["versionName"])
		if env == "" || ver == "" {
			continue
		}
		out = append(out, secretManagerBinding{Env: env, VersionName: ver})
	}
	return out
}

// resolveStepSecrets loads Secret Manager values referenced by step secretEnv.
// Matches real Cloud Build: availableSecrets only maps names; a step must list
// secretEnv to receive them. Access is authorized as the build service account
// (secretmanager.versions.access on the secret resource or project).
func (r *EngineRunner) resolveStepSecrets(buildJSON string, steps []BuildStep, buildSA string) (map[string]string, error) {
	needed := map[string]struct{}{}
	for _, step := range steps {
		for _, name := range step.SecretEnv {
			if name != "" {
				needed[name] = struct{}{}
			}
		}
	}
	if len(needed) == 0 {
		return nil, nil
	}
	bindings := parseAvailableSecrets(buildJSON)
	byEnv := make(map[string]secretManagerBinding, len(bindings))
	for _, b := range bindings {
		byEnv[b.Env] = b
	}
	if r == nil || r.Store == nil {
		return nil, fmt.Errorf("secret manager store required")
	}
	out := make(map[string]string, len(needed))
	for name := range needed {
		b, ok := byEnv[name]
		if !ok {
			return nil, fmt.Errorf("secretEnv %q not in availableSecrets", name)
		}
		secretName, versionID, ok := store.ParseSecretVersionName(b.VersionName)
		if !ok {
			return nil, fmt.Errorf("invalid secret versionName %q", b.VersionName)
		}
		if err := r.requireBuildSASecretAccess(buildSA, secretName); err != nil {
			return nil, err
		}
		plain, _, err := r.Store.AccessSecretVersion(secretName, versionID)
		if err != nil {
			return nil, fmt.Errorf("access secret %s: %w", b.Env, err)
		}
		out[name] = string(plain)
	}
	return out, nil
}

func (r *EngineRunner) requireBuildSASecretAccess(buildSA, secretName string) error {
	if strings.TrimSpace(buildSA) == "" {
		return fmt.Errorf("build service account required for secretEnv")
	}
	if r.Authz == nil {
		// Fail closed when Authz is unset outside tests that inject secrets without IAM.
		return fmt.Errorf("secretmanager.versions.access denied for %s on %s", buildSA, secretName)
	}
	// Match Secret Manager handlers: secret resource OR project-level grant (EvaluateAny).
	resources := []string{secretName}
	if project := projectIDFromResourceName(secretName); project != "" {
		resources = append(resources, "projects/"+project)
	}
	ok, err := r.Authz.EvaluateAny(buildSA, false, "secretmanager.versions.access", resources...)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("secretmanager.versions.access denied for %s on %s", buildSA, secretName)
	}
	return nil
}

// projectIDFromResourceName extracts projects/{id}/... project id.
func projectIDFromResourceName(name string) string {
	parts := strings.Split(strings.TrimSpace(name), "/")
	if len(parts) >= 2 && parts[0] == "projects" && parts[1] != "" {
		return parts[1]
	}
	return ""
}

// withSecretEnv injects Secret Manager values listed in step.secretEnv only
// (real Cloud Build: availableSecrets alone does not populate step env).
func withSecretEnv(step BuildStep, secrets map[string]string) (BuildStep, error) {
	if len(step.SecretEnv) == 0 {
		return step, nil
	}
	if len(secrets) == 0 {
		return step, fmt.Errorf("secretEnv requires availableSecrets")
	}
	env := append([]string(nil), step.Env...)
	for _, name := range step.SecretEnv {
		val, ok := secrets[name]
		if !ok {
			return step, fmt.Errorf("secretEnv %q not in availableSecrets", name)
		}
		env = upsertEnv(env, name, val)
	}
	step.Env = env
	return step, nil
}

func (r *EngineRunner) failBuild(cur store.CbBuild, detail string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	body := store.MarkCbBuildStepsStatus(cur.BuildJSON, "FAILURE")
	_, _, err := r.Store.PutCbBuildProgress(cur.Name, "FAILURE", detail, body, now)
	return err
}

func (r *EngineRunner) engineReady() bool {
	host, _ := r.engineDial()
	if host == "" {
		return false
	}
	lower := strings.ToLower(host)
	return !strings.Contains(lower, "docker.sock")
}

func (r *EngineRunner) engineDial() (host, cert string) {
	host = strings.TrimSpace(r.DockerHost)
	cert = strings.TrimSpace(r.TLSCertDir)
	if host != "" {
		return host, cert
	}
	if d, ok := r.Invoker.(compute.DockerInvoker); ok {
		return strings.TrimSpace(d.Host), strings.TrimSpace(d.TLSCertDir)
	}
	return "", ""
}

func (r *EngineRunner) enforcePrivatePoolEgress(b store.CbBuild) error {
	var cfg map[string]any
	_ = json.Unmarshal([]byte(b.BuildJSON), &cfg)
	poolName := workerPoolNameFromBuild(cfg)
	if poolName == "" {
		return nil
	}
	pool, ok, err := r.Store.GetCbWorkerPool(poolName)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("worker pool not found")
	}
	if !poolNoPublicEgress(pool) {
		return nil
	}
	for _, ep := range httpEndpointsFromBuildJSON(b.BuildJSON) {
		if err := httpegress.Validate(ep); err != nil {
			return fmt.Errorf("NO_PUBLIC_EGRESS denied %s", ep)
		}
	}
	return nil
}

func poolNoPublicEgress(p *store.CbWorkerPool) bool {
	if p == nil {
		return true
	}
	var ann map[string]any
	_ = json.Unmarshal([]byte(p.AnnotationsJSON), &ann)
	if ann == nil {
		return true
	}
	v, ok := ann["NO_PUBLIC_EGRESS"]
	if !ok {
		return true
	}
	switch t := v.(type) {
	case bool:
		return t
	case string:
		s := strings.TrimSpace(t)
		if s == "" {
			return true
		}
		return !strings.EqualFold(s, "false") && s != "0"
	default:
		return true
	}
}

func isActiveBuildStatus(status string) bool {
	switch status {
	case "WORKING", "QUEUED", "PENDING":
		return true
	default:
		return false
	}
}

func (r *EngineRunner) buildStepIdentity(build store.CbBuild) (token string, extraHosts []string, err error) {
	extraHosts = compute.HostGatewayExtraHosts()
	if r == nil || r.Store == nil {
		return "", extraHosts, fmt.Errorf("build service account store required")
	}
	email := parseBuildServiceAccountEmail(build.BuildJSON, build.ProjectID)
	if email == "" {
		return "", extraHosts, fmt.Errorf("build service account email required")
	}
	project := strings.TrimSpace(build.ProjectID)
	if project == "" {
		return "", extraHosts, fmt.Errorf("build project required")
	}
	if err := r.Store.EnsureServiceAccount(project, email, "cloud build service account"); err != nil {
		return "", extraHosts, err
	}
	tok, _, err := labtoken.Mint(r.Store, email, labtoken.DefaultLifetime)
	if err != nil {
		return "", extraHosts, err
	}
	return tok, extraHosts, nil
}

func parseBuildServiceAccountEmail(buildJSON, projectID string) string {
	var cfg map[string]any
	if err := json.Unmarshal([]byte(buildJSON), &cfg); err == nil && cfg != nil {
		if email := serviceAccountEmail(stringField(cfg["serviceAccount"])); email != "" {
			return email
		}
	}
	return labtoken.DefaultComputeSAEmail(projectID)
}

func serviceAccountEmail(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	const marker = "/serviceAccounts/"
	if i := strings.LastIndex(raw, marker); i >= 0 {
		return strings.TrimSpace(raw[i+len(marker):])
	}
	return raw
}

func withBuildIdentityEnv(step BuildStep, token string, extraHosts []string) BuildStep {
	env := append([]string(nil), step.Env...)
	if token != "" {
		env = upsertEnv(env, "CLOUDSDK_AUTH_ACCESS_TOKEN", token)
	}
	if len(extraHosts) > 0 {
		base := "http://host.docker.internal:" + httpegress.LabListenPort + "/"
		env = upsertEnv(env, "CLOUDSDK_API_ENDPOINT_OVERRIDES_IAMCREDENTIALS", base)
		env = upsertEnv(env, "CLOUDSDK_API_ENDPOINT_OVERRIDES_IAM", base)
		env = upsertEnv(env, "CLOUDSDK_API_ENDPOINT_OVERRIDES_STORAGE", base)
		env = upsertEnv(env, "STORAGE_EMULATOR_HOST", "host.docker.internal:"+httpegress.LabListenPort)
	}
	step.Env = env
	return step
}

func upsertEnv(env []string, key, value string) []string {
	prefix := key + "="
	for i, e := range env {
		if strings.HasPrefix(e, prefix) {
			env[i] = prefix + value
			return env
		}
	}
	return append(env, prefix+value)
}

func parseBuildSteps(buildJSON string) []BuildStep {
	var cfg map[string]any
	if err := json.Unmarshal([]byte(buildJSON), &cfg); err != nil || cfg == nil {
		return nil
	}
	list, _ := cfg["steps"].([]any)
	out := make([]BuildStep, 0, len(list))
	for _, step := range list {
		sm, ok := step.(map[string]any)
		if !ok {
			continue
		}
		bs := BuildStep{
			Image:     stringField(sm["name"]),
			Script:    stringField(sm["script"]),
			Args:      stringList(sm["args"]),
			Env:       stringList(sm["env"]),
			SecretEnv: stringList(sm["secretEnv"]),
		}
		out = append(out, bs)
	}
	return out
}

func httpEndpointsFromBuildJSON(buildJSON string) []string {
	var seen []string
	add := func(vals []string) {
		for _, ep := range vals {
			if ep != "" {
				seen = append(seen, ep)
			}
		}
	}
	for _, step := range parseBuildSteps(buildJSON) {
		for _, a := range step.Args {
			add(httpEndpointsFromString(a))
		}
		for _, e := range step.Env {
			add(httpEndpointsFromString(e))
		}
		add(httpEndpointsFromString(step.Script))
	}
	return seen
}

func httpEndpointsFromString(s string) []string {
	matches := httpURLRe.FindAllString(s, -1)
	if len(matches) == 0 {
		return nil
	}
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		m = strings.TrimRight(m, ".,);]}'\"")
		if m != "" {
			out = append(out, m)
		}
	}
	return out
}

func markStepStatusAt(buildJSON string, index int, status string) string {
	var cfg map[string]any
	if err := json.Unmarshal([]byte(buildJSON), &cfg); err != nil || cfg == nil {
		return buildJSON
	}
	list, _ := cfg["steps"].([]any)
	if index < 0 || index >= len(list) {
		return buildJSON
	}
	sm, ok := list[index].(map[string]any)
	if !ok {
		return buildJSON
	}
	sm["status"] = status
	list[index] = sm
	cfg["steps"] = list
	raw, err := json.Marshal(cfg)
	if err != nil {
		return buildJSON
	}
	return string(raw)
}

func stringField(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func stringList(v any) []string {
	list, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}
