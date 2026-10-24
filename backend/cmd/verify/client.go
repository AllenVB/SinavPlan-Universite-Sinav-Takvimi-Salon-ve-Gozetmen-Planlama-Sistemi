package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// M, JSON nesnesi.
type M = map[string]any

// api, doğrulayıcının tek bağımlılığı olan HTTP istemcisi: sistem yalnızca dışarıdan,
// API üzerinden sınanır (veritabanına doğrudan erişim yok).
type api struct {
	base string
	hc   *http.Client
}

func newAPI(base string) *api {
	return &api{base: strings.TrimRight(base, "/"), hc: &http.Client{Timeout: 60 * time.Second}}
}

// resp, bir yanıt.
type resp struct {
	code   int
	body   M
	raw    []byte
	header http.Header
}

func (a *api) do(ctx context.Context, method, path, token, ctype string, body io.Reader, hdr ...string) (resp, error) {
	req, err := http.NewRequestWithContext(ctx, method, a.base+path, body)
	if err != nil {
		return resp{}, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	r, err := a.hc.Do(req)
	if err != nil {
		return resp{}, err
	}
	defer r.Body.Close()
	raw, _ := io.ReadAll(r.Body)
	out := resp{code: r.StatusCode, raw: raw, header: r.Header}
	_ = json.Unmarshal(raw, &out.body)
	return out, nil
}

func (a *api) get(ctx context.Context, path, token string) (resp, error) {
	return a.do(ctx, http.MethodGet, path, token, "", nil)
}

func (a *api) post(ctx context.Context, path, token string, body any, hdr ...string) (resp, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	return a.do(ctx, http.MethodPost, path, token, "application/json", rd, hdr...)
}

func (a *api) csv(ctx context.Context, path, token, data string) (resp, error) {
	return a.do(ctx, http.MethodPost, path, token, "text/csv", strings.NewReader(data))
}

func (a *api) login(ctx context.Context, email, password string) (string, error) {
	r, err := a.post(ctx, "/auth/login", "", M{"email": email, "password": password})
	if err != nil {
		return "", err
	}
	if r.code != 200 {
		return "", fmt.Errorf("%s girişi: HTTP %d", email, r.code)
	}
	return str(r.body, "access_token"), nil
}

// all, keyset sayfalı bir listeyi sonuna kadar okur (next_cursor / after).
func (a *api) all(ctx context.Context, path, token string) ([]M, error) {
	var out []M
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	after := ""
	for page := 0; page < 10_000; page++ {
		p := path + sep + "limit=200"
		if after != "" {
			p += "&after=" + after
		}
		r, err := a.get(ctx, p, token)
		if err != nil {
			return nil, err
		}
		if r.code != 200 {
			return nil, fmt.Errorf("GET %s: HTTP %d %s", p, r.code, trunc(string(r.raw), 160))
		}
		for _, it := range arr(r.body, "items") {
			if m, ok := it.(M); ok {
				out = append(out, m)
			}
		}
		after = str(r.body, "next_cursor")
		if after == "" {
			return out, nil
		}
	}
	return nil, fmt.Errorf("sayfalama sonlanmadı: %s", path)
}

// --- JSON yardımcıları ---------------------------------------------------------------

func num(m M, k string) int64 {
	f, _ := m[k].(float64)
	return int64(f)
}

func str(m M, k string) string {
	s, _ := m[k].(string)
	return s
}

func boolean(m M, k string) bool {
	b, _ := m[k].(bool)
	return b
}

func arr(m M, k string) []any {
	a, _ := m[k].([]any)
	return a
}

func obj(m M, k string) M {
	o, _ := m[k].(M)
	return o
}

func trunc(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

// errCode, hata gövdesindeki error.code değeri.
func errCode(r resp) string { return str(obj(r.body, "error"), "code") }

// failures, bir kimliğin ihlal listesini biriktirir; boşsa PASS.
type failures struct{ list []string }

func (f *failures) check(ok bool, format string, a ...any) {
	if !ok {
		f.list = append(f.list, fmt.Sprintf(format, a...))
	}
}

func (f *failures) err() error {
	if len(f.list) == 0 {
		return nil
	}
	n := len(f.list)
	msg := strings.Join(f.list[:min(n, 3)], "; ")
	if n > 3 {
		msg += fmt.Sprintf(" (+%d ihlal daha)", n-3)
	}
	return fmt.Errorf("%s", strings.Join(strings.Fields(msg), " "))
}

// expectCode, HTTP durumunu denetler.
func (f *failures) expectCode(what string, r resp, err error, want int) bool {
	if err != nil {
		f.check(false, "%s: %v", what, err)
		return false
	}
	if r.code != want {
		f.check(false, "%s: HTTP %d (beklenen %d) %s", what, r.code, want, trunc(string(r.raw), 120))
		return false
	}
	return true
}

func jsonReader(v any) io.Reader {
	b, _ := json.Marshal(v)
	return bytes.NewReader(b)
}
