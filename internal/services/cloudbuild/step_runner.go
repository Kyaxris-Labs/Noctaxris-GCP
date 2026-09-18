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
	"github.com/Kyaxris-Labs/Noctaxris-GCP/internal/store"
)

// StepRunner executes a persisted WORKING build. Tests inject an implementation.
type StepRunner interface {
	Run(ctx context.Context, build store.CbBuild) error
}

// BuildStep is one Cloud Build step from the request JSON.
type BuildStep struct {
	Image  string
	Args   []string
	Env    []string
	Script string
}

// EngineRunner runs steps on the nested engine (same family as Cloud Run :invoke).
// ExecuteStep, when set, skips Docker and is used by unit tests after egress checks.
type EngineRunner struct {
	Store       *store.Store
	Invoker     compute.Invoker
	DockerHost  string
	TLSCertDir  string
	ExecuteStep func(ctx context.Context, step BuildStep) error
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
	if s != nil {
		st = s.Store
	}
	return &EngineRunner{Store: st, Invoker: inv, DockerHost: host, TLSCertDir: cert}
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
		if err := r.runOneStep(ctx, step); err != nil {
			cur.BuildJSON = markStepStatusAt(cur.BuildJSON, i, "FAILURE")
			return r.failBuild(cur, err.Error())
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

func (r *EngineRunner) runOneStep(ctx context.Context, step BuildStep) error {
	if r.ExecuteStep != nil {
		return r.ExecuteStep(ctx, step)
	}
	host, cert := r.engineDial()
	cli, err := compute.Dial(host, cert)
	if err != nil {
		return fmt.Errorf("nested engine not configured: %w", err)
	}
	defer cli.Close()
	if !cli.Enabled() {
		return fmt.Errorf("nested engine not configured")
	}
	res, err := cli.RunBuildStep(ctx, compute.BuildStepRun{
		Image:  step.Image,
		Cmd:    step.Args,
		Env:    step.Env,
		Script: step.Script,
	})
	if err != nil {
		return err
	}
	if res.ExitCode != 0 {
		return fmt.Errorf("step exit %d", res.ExitCode)
	}
	return nil
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
			Image:  stringField(sm["name"]),
			Script: stringField(sm["script"]),
			Args:   stringList(sm["args"]),
			Env:    stringList(sm["env"]),
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
