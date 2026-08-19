package server

import (
	"net/http"
	"path"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/jaredeh/vmtool/internal/api"
	"github.com/jaredeh/vmtool/spec"
)

type recMux struct {
	routes map[string]bool
}

func (m *recMux) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	if m.routes == nil {
		m.routes = map[string]bool{}
	}
	m.routes[normalizePattern(pattern)] = true
}

func (m *recMux) ServeHTTP(http.ResponseWriter, *http.Request) {}

func normalizePattern(p string) string {
	p = strings.TrimSpace(p)
	p = strings.ReplaceAll(p, "/{$}", "/")
	return p
}

func TestSpecParity(t *testing.T) {
	var _ api.ServeMux = (*recMux)(nil)
	rec := &recMux{}
	api.HandlerFromMux(api.NewStrictHandler(&handlers{}, nil), rec)

	doc, err := openapi3.NewLoader().LoadFromData(spec.OpenAPIYAML)
	if err != nil {
		t.Fatalf("load spec: %v", err)
	}
	specOps := map[string]bool{}
	for rawPath, item := range doc.Paths.Map() {
		full := path.Clean("/" + rawPath)
		if full != "/" {
			full = strings.TrimSuffix(full, "/")
		}
		for method := range item.Operations() {
			specOps[strings.ToUpper(method)+" "+full] = true
		}
	}

	var missing, extra []string
	for k := range rec.routes {
		if !specOps[k] {
			extra = append(extra, k)
		}
	}
	for k := range specOps {
		if !rec.routes[k] {
			missing = append(missing, k)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 || len(extra) > 0 {
		t.Fatalf("parity mismatch\nmissing from mux: %v\nextra on mux: %v\nmux=%v\nspec=%v", missing, extra, keys(rec.routes), keys(specOps))
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
