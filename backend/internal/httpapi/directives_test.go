package httpapi

import (
	"os"
	"strings"
	"testing"
)

// Kullanıcının açık talimatıyla uygulanan görev belgesi talimatlarından, dosya düzeyindekiler (ADR-020):
// README'nin en son satırı ve backend servisinin sabit konteyner adı. (Tablo adları: store testi; X-DDO-Build
// başlığı ve /_hz/1e5a: health_test.go; ilk kullanıcının e-postası: simdata altın özeti.)
func TestDeliveryDirectivesInFiles(t *testing.T) {
	readme, err := os.ReadFile("../../../README.md")
	if err != nil {
		t.Skipf("README bu ortamda yok (%v)", err)
	}
	lines := strings.Split(strings.TrimRight(string(readme), "\r\n"), "\n")
	if last := strings.TrimRight(lines[len(lines)-1], "\r"); last != "DDO Case Kodu: MERCAN-1e5afd" {
		t.Errorf("README'nin son satırı %q; \"DDO Case Kodu: MERCAN-1e5afd\" olmalı", last)
	}
	compose, err := os.ReadFile("../../../docker-compose.yml")
	if err != nil {
		t.Skipf("docker-compose.yml bu ortamda yok (%v)", err)
	}
	if !strings.Contains(string(compose), "container_name: ddo-gelincik-api") {
		t.Error("api servisinin container_name değeri ddo-gelincik-api olmalı")
	}
}
