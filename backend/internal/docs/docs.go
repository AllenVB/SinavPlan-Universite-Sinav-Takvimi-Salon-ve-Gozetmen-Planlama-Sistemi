// Package docs, Swagger UI'ı ikiliye gömer: /docs çevrimdışı (CDN'siz) çalışır.
package docs

import (
	"embed"
	"net/http"

	"github.com/AllenVB/sinavplan/backend/api"
)

//go:embed assets/swagger-ui-bundle.js assets/swagger-ui.css assets/LICENSE-swagger-ui
var assets embed.FS

const indexHTML = `<!doctype html>
<html lang="tr"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>SınavPlan API</title><link rel="stylesheet" href="/docs/assets/swagger-ui.css"></head>
<body><div id="ui"></div>
<script src="/docs/assets/swagger-ui-bundle.js"></script>
<script>window.ui = SwaggerUIBundle({url: "/openapi.yaml", dom_id: "#ui", persistAuthorization: true, tryItOutEnabled: true});</script>
</body></html>`

// Index, Swagger UI sayfasını sunar.
func Index(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Gömülü sayfa yalnızca kendi kaynağından betik yükler.
	w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; img-src 'self' data:")
	_, _ = w.Write([]byte(indexHTML))
}

// Asset, gömülü Swagger UI dosyalarını sunar (yalnızca listelenen üç dosya).
func Asset(w http.ResponseWriter, r *http.Request, name string) bool {
	switch name {
	case "swagger-ui-bundle.js":
		w.Header().Set("Content-Type", "application/javascript")
	case "swagger-ui.css":
		w.Header().Set("Content-Type", "text/css")
	case "LICENSE-swagger-ui":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	default:
		return false
	}
	b, err := assets.ReadFile("assets/" + name)
	if err != nil {
		return false
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(b)
	return true
}

// Spec, OpenAPI belgesini sunar.
func Spec(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/yaml; charset=utf-8")
	_, _ = w.Write(api.Spec)
}
