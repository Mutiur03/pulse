// Package archtest enforces the query-api module boundaries described in
// docs/adr/0001-query-api-modular-monolith.md.
package archtest

import (
	"os/exec"
	"strings"
	"testing"
)

const (
	root    = "github.com/Mutiur03/pulse"
	modules = root + "/internal/modules/"
)

// layers lists which sibling layers each layer of a module may import.
// The module root (module.go) may import all of them.
var layers = map[string][]string{
	"domain":   {"domain"},
	"ports":    {"domain", "ports"},
	"service":  {"domain", "ports", "service"},
	"adapters": {"domain", "ports", "service", "adapters"},
}

type pkg struct {
	path    string
	imports []string
}

func load(t *testing.T) []pkg {
	t.Helper()
	out, err := exec.Command("go", "list", "-f",
		`{{.ImportPath}} {{join .Imports " "}} {{join .TestImports " "}} {{join .XTestImports " "}}`,
		root+"/...").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	var pkgs []pkg
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		pkgs = append(pkgs, pkg{path: f[0], imports: f[1:]})
	}
	return pkgs
}

// split returns the module name and the path inside it ("" for the root).
func split(path string) (module, rest string, ok bool) {
	if !strings.HasPrefix(path, modules) {
		return "", "", false
	}
	module, rest, _ = strings.Cut(strings.TrimPrefix(path, modules), "/")
	return module, rest, true
}

// check returns why from may not import to, or "" when it may.
func check(from, to string) string {
	toMod, toRest, toIsMod := split(to)
	fromMod, fromRest, fromIsMod := split(from)
	if fromIsMod && to == root+"/internal/api" {
		return "modules must not import the query-api composition root"
	}
	if !toIsMod {
		return ""
	}
	if !fromIsMod || fromMod != toMod {
		if toRest != "" {
			return "only a module's root package (module.go) is public"
		}
		if fromIsMod && fromRest != "" && !strings.HasPrefix(fromRest, "adapters") && !strings.HasPrefix(fromRest, "service") {
			return "only a module's root, service and adapters may use another module"
		}
		return ""
	}
	// Same module: enforce layering.
	if fromRest == "" {
		return ""
	}
	fromLayer, _, _ := strings.Cut(fromRest, "/")
	toLayer, _, _ := strings.Cut(toRest, "/")
	if toRest == "" {
		return "layers must not import their own module root"
	}
	allowed, known := layers[fromLayer]
	if !known {
		return "unknown layer " + fromLayer + " (use domain, ports, service or adapters)"
	}
	for _, l := range allowed {
		if l == toLayer {
			return ""
		}
	}
	return fromLayer + " must not import " + toLayer
}

func TestModuleBoundaries(t *testing.T) {
	for _, p := range load(t) {
		seen := map[string]bool{}
		for _, imp := range p.imports {
			if seen[imp] || !strings.HasPrefix(imp, root+"/") {
				continue
			}
			seen[imp] = true
			if why := check(p.path, imp); why != "" {
				t.Errorf("%s imports %s: %s", strings.TrimPrefix(p.path, root+"/"), strings.TrimPrefix(imp, root+"/"), why)
			}
		}
	}
}

func TestCheckRules(t *testing.T) {
	m := root + "/internal/modules/"
	cases := []struct {
		from, to string
		ok       bool
	}{
		{root + "/cmd/alerting-worker", m + "alerts", true},
		{root + "/cmd/alerting-worker", m + "alerts/service", false},
		{m + "alerts/adapters/http", m + "issues", true},
		{m + "alerts/adapters/http", m + "issues/domain", false},
		{m + "alerts/domain", m + "issues", false},
		{m + "alerts/domain", m + "alerts/adapters/postgres", false},
		{m + "alerts/service", m + "alerts/ports", true},
		{m + "alerts/service", m + "alerts/adapters/postgres", false},
		{m + "alerts/adapters/postgres", m + "alerts/domain", true},
		{m + "alerts", m + "alerts/adapters/http", true},
		{m + "alerts/service", m + "alerts", false},
		{m + "alerts/service", root + "/internal/api", false},
		{root + "/internal/api", m + "alerts", true},
	}
	for _, c := range cases {
		if got := check(c.from, c.to) == ""; got != c.ok {
			t.Errorf("%s -> %s: allowed=%v, want %v (%s)", c.from, c.to, got, c.ok, check(c.from, c.to))
		}
	}
}
