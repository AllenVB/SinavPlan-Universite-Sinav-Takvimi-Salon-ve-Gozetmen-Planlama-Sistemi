// Package api, OpenAPI 3 belgesini ikiliye gömer. Belge, sunulan uçlarla birebir
// uyumluluğu httpapi.TestOpenAPIMatchesRegisteredRoutes ile zorlanır.
package api

import _ "embed"

// Spec, openapi.yaml içeriği.
//
//go:embed openapi.yaml
var Spec []byte
