// Command govulncheck-ci runs govulncheck -json and fails only on findings
// whose OSV IDs are not listed in scripts/govulncheck-allowlist.txt.
//
// govulncheck has no native per-ID exclude (golang/go#61211). Prefer fixing
// reachable vulns (toolchain bumps, dependency updates). The allowlist is only
// for documented residuals with no module-path fix (see docs/security-defaults.md).
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	root, err := moduleRoot()
	if err != nil {
		fatalf("%v", err)
	}
	allowPath := os.Getenv("GOVULNCHECK_ALLOWLIST")
	if allowPath == "" {
		allowPath = filepath.Join(root, "scripts", "govulncheck-allowlist.txt")
	}
	allow, err := loadAllowlist(allowPath)
	if err != nil {
		fatalf("load allowlist: %v", err)
	}

	cmd := exec.Command("govulncheck", "-json", "./...")
	cmd.Dir = root
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		// -json mode exits 0 even when findings exist; non-zero is a tool failure.
		fatalf("govulncheck: %v", err)
	}

	found, err := osvFromJSON(out)
	if err != nil {
		fatalf("parse govulncheck json: %v", err)
	}

	fmt.Println("Vulnerabilities with call/import traces:")
	if len(found) == 0 {
		fmt.Println("(none)")
	} else {
		for _, id := range found {
			fmt.Println(id)
		}
	}

	foundSet := toSet(found)
	var stale []string
	for _, id := range allow {
		if !foundSet[id] {
			stale = append(stale, id)
		}
	}
	if len(stale) > 0 {
		fmt.Fprintf(os.Stderr, "warning: allowlisted IDs no longer reported (remove from allowlist):\n")
		for _, id := range stale {
			fmt.Fprintln(os.Stderr, id)
		}
	}

	allowSet := toSet(allow)
	var novel []string
	for _, id := range found {
		if !allowSet[id] {
			novel = append(novel, id)
		}
	}
	if len(novel) > 0 {
		fmt.Fprintf(os.Stderr, "error: vulnerabilities not in allowlist:\n")
		for _, id := range novel {
			fmt.Fprintln(os.Stderr, id)
		}
		fmt.Fprintln(os.Stderr, "Details: https://pkg.go.dev/vuln/ — or run 'govulncheck ./...' locally.")
		os.Exit(1)
	}
	fmt.Println("OK: only allowlisted findings are present (or none).")
}

func moduleRoot() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod not found above %s", wd)
		}
		dir = parent
	}
}

func loadAllowlist(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var ids []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if !strings.HasPrefix(line, "GO-") {
			return nil, fmt.Errorf("%s: invalid allowlist line %q", path, line)
		}
		ids = append(ids, line)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	sort.Strings(ids)
	return unique(ids), nil
}

type govulnEvent struct {
	Finding *struct {
		OSV   string           `json:"osv"`
		Trace []map[string]any `json:"trace"`
	} `json:"finding"`
}

func osvFromJSON(raw []byte) ([]string, error) {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	seen := map[string]struct{}{}
	var ids []string
	for {
		var ev govulnEvent
		if err := dec.Decode(&ev); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		if ev.Finding == nil || ev.Finding.OSV == "" || !symbolReachable(ev.Finding.Trace) {
			continue
		}
		if _, ok := seen[ev.Finding.OSV]; ok {
			continue
		}
		seen[ev.Finding.OSV] = struct{}{}
		ids = append(ids, ev.Finding.OSV)
	}
	sort.Strings(ids)
	return ids, nil
}

// symbolReachable matches default govulncheck text/symbol mode: ignore module-only
// findings that have no package frame.
func symbolReachable(trace []map[string]any) bool {
	for _, frame := range trace {
		pkg, _ := frame["package"].(string)
		if strings.TrimSpace(pkg) != "" {
			return true
		}
	}
	return false
}

func toSet(ids []string) map[string]bool {
	m := make(map[string]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}

func unique(ids []string) []string {
	if len(ids) == 0 {
		return ids
	}
	out := []string{ids[0]}
	for i := 1; i < len(ids); i++ {
		if ids[i] != ids[i-1] {
			out = append(out, ids[i])
		}
	}
	return out
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
