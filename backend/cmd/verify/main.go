// Uçtan uca doğrulayıcı: ayağa kalkmış sistemde F-1..F-7, T-1..T-5, /health ve
// yetkilendirme senaryolarını YALNIZCA HTTP API üzerinden dener (çözücü kodu veya
// veritabanı kullanmaz; T-1..T-4 kısıtları API'den okunan veriden yeniden hesaplanır).
// Sonunda "kimlik | sonuç | süre | not" tablosu basar; PASS olmayan kimlik varsa exit 1.
package main

import (
	"context"
	"fmt"
	"os"
	"time"
)

type result struct {
	id      string
	status  string
	elapsed time.Duration
	note    string
}

// check, bir kimliğin doğrulama işlevi; ihlalleri f'e yazar, kısa bir not döner.
type check struct {
	id  string
	run func(ctx context.Context, e *env, f *failures) string
}

func main() {
	base := os.Getenv("API_BASE_URL")
	if base == "" {
		base = "http://localhost:8080"
	}
	password := os.Getenv("DEMO_PASSWORD")
	if password == "" {
		password = "Demo!1234"
	}
	e := &env{a: newAPI(base), password: password}

	// Sıra bağımlılığa göredir: çizelge üretilmeden T-1..T-4, T-4'ten sonra F-5
	// (gözetmen çıkarma planı değiştirir), tahliye (T-5) en sonda.
	checks := []check{
		{"HEALTH", checkHealth},
		{"AUTH-401", checkAuth401},
		// CHK-1 ilk sırada: F-3 yeni bir çizelge yayınlayınca ilk kurulumdaki "şimdi süren sınav" artık görev olmaz
		{"CHK-1", checkCheckin1},
		{"AUTH-403", checkAuth403},
		{"F-1", checkF1},
		{"F-2", checkF2},
		{"F-3", checkF3},
		{"T-1", checkT1},
		{"T-2", checkT2},
		{"T-3", checkT3},
		{"F-4", checkF4},
		{"T-4", checkT4},
		{"F-5", checkF5},
		{"F-6", checkF6},
		{"OWNERSHIP", checkOwnership},
		{"F-7", checkF7},
		{"T-5", checkT5},
		{"X-2", checkX2},
		{"X-3", checkX3},
		{"X-4", checkX4},
		{"X-5/X-7", checkX5X7},
		{"X-6", checkX6},
		{"L-1", checkL1},
		{"Q-1", checkQ1},
		{"FAK-1", checkFak1},
		{"B1-1", checkB1},
		{"A3-1", checkA3},
	}
	var results []result
	for _, c := range checks {
		results = append(results, runCheck(c, e))
	}
	failed := printTable(results)
	if failed > 0 {
		fmt.Printf("\n%d kimlik PASS değil — doğrulama BAŞARISIZ\n", failed)
		os.Exit(1)
	}
	fmt.Println("\ntüm kimlikler geçti")
}

func runCheck(c check, e *env) (r result) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	defer func() {
		if p := recover(); p != nil {
			r = result{c.id, "FAIL", time.Since(start), fmt.Sprintf("panik: %v", p)}
		}
	}()
	var f failures
	note := c.run(ctx, e, &f)
	if err := f.err(); err != nil {
		return result{c.id, "FAIL", time.Since(start), err.Error()}
	}
	return result{c.id, "PASS", time.Since(start), note}
}

func printTable(rs []result) int {
	fmt.Printf("\n%-10s | %-6s | %8s | %s\n", "KİMLİK", "SONUÇ", "SÜRE", "NOT")
	fmt.Println("-----------+--------+----------+--------------------------------------------")
	failed := 0
	for _, r := range rs {
		if r.status != "PASS" {
			failed++
		}
		fmt.Printf("%-10s | %-6s | %7dms | %s\n", r.id, r.status, r.elapsed.Milliseconds(), r.note)
	}
	return failed
}
