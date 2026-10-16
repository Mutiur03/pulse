package main

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/Mutiur03/pulse/internal/api"
	"github.com/Mutiur03/pulse/internal/api/gen"
	"github.com/Mutiur03/pulse/internal/api/legacy"
	"github.com/Mutiur03/pulse/internal/config"
)

// TestEveryOperationRoutedOnce checks that the modules and the legacy
// server together serve each spec operation exactly once.
func TestEveryOperationRoutedOnce(t *testing.T) {
	spec, err := gen.GetSwagger()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{}
	for path, item := range spec.Paths {
		for method := range item.Operations() {
			want[method+" "+path] = true
		}
	}

	r := chi.NewRouter()
	srv := &api.Server{}
	mountModules(r, config.Load(), deps{}, srv)
	legacy.HandlerFromMux(srv, r)
	got := map[string]int{}
	err = chi.Walk(r, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		got[method+" "+strings.TrimSuffix(route, "/")]++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var problems []string
	for k := range want {
		if got[k] != 1 {
			problems = append(problems, fmt.Sprintf("%s routed %d times", k, got[k]))
		}
	}
	for k := range got {
		if !want[k] {
			problems = append(problems, k+" is not in the spec")
		}
	}
	sort.Strings(problems)
	for _, p := range problems {
		t.Error(p)
	}
}
