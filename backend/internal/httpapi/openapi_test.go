package httpapi

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/AllenVB/sinavplan/backend/api"
	"gopkg.in/yaml.v3"
)

type specDoc struct {
	Paths map[string]map[string]any `yaml:"paths"`
}

// Belge ile sunucu arasında sapma olamaz: kayıtlı her route belgelenmiş, belgelenen her
// uç kayıtlı olmalı; ayrıca herkese açık uçlar belgede `security: []` ile işaretli olmalı.
func TestOpenAPIMatchesRegisteredRoutes(t *testing.T) {
	var doc specDoc
	if err := yaml.Unmarshal(api.Spec, &doc); err != nil {
		t.Fatalf("openapi.yaml çözümlenemedi: %v", err)
	}
	documented := map[string]bool{}
	public := map[string]bool{}
	for path, ops := range doc.Paths {
		for method, op := range ops {
			m := strings.ToUpper(method)
			if m == "PARAMETERS" { // yol düzeyi parametre listesi işlem değildir
				continue
			}
			documented[m+" "+path] = true
			if sec, ok := op.(map[string]any)["security"].([]any); ok && len(sec) == 0 {
				public[m+" "+path] = true
			}
		}
	}
	s := rbacServer(t)
	registered := map[string]bool{}
	for _, ri := range s.Routes() {
		k := ri.Method + " " + ri.Pattern
		registered[k] = true
		if !documented[k] {
			t.Errorf("%s OpenAPI'de belgelenmemiş", k)
		}
		if ri.Access.public != public[k] {
			t.Errorf("%s: kodda public=%v, belgede security:[] = %v", k, ri.Access.public, public[k])
		}
	}
	var extra []string
	for k := range documented {
		if !registered[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	if len(extra) > 0 {
		t.Errorf("belgelenmiş ama sunucuda olmayan uçlar: %v", extra)
	}
}

func TestDocsAndSpecAreServedOffline(t *testing.T) {
	s := newTestServer(t, fakePinger{})
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}
	if r := get("/docs"); r.Code != 200 || !strings.Contains(r.Body.String(), "/docs/assets/swagger-ui-bundle.js") ||
		strings.Contains(r.Body.String(), "cdn") || strings.Contains(r.Body.String(), "unpkg") {
		t.Fatalf("/docs yerel varlıklarla çalışmalı: %d", r.Code)
	}
	if r := get("/docs/assets/swagger-ui-bundle.js"); r.Code != 200 || r.Body.Len() < 1_000_000 {
		t.Fatalf("Swagger UI paketi gömülü olmalı: %d, %d bayt", r.Code, r.Body.Len())
	}
	if r := get("/docs/assets/swagger-ui.css"); r.Code != 200 {
		t.Fatalf("css: %d", r.Code)
	}
	if r := get("/docs/assets/..%2f..%2fgo.mod"); r.Code != http.StatusNotFound {
		t.Fatalf("liste dışı dosya sunulmamalı: %d", r.Code)
	}
	if r := get("/docs/assets/baska.js"); r.Code != http.StatusNotFound {
		t.Fatalf("bilinmeyen varlık 404: %d", r.Code)
	}
	r := get("/openapi.yaml")
	var doc map[string]any
	if r.Code != 200 || yaml.Unmarshal(r.Body.Bytes(), &doc) != nil || doc["openapi"] != "3.0.3" {
		t.Fatalf("openapi.yaml geçerli OpenAPI 3 olmalı: %d", r.Code)
	}
}
