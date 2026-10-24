// Uçtan uca doğrulayıcı: ayağa kalkmış sistemde F-1..F-7, T-1..T-5, /health ve
// yetkilendirme senaryolarını API üzerinden dener; sonunda
// "kimlik | sonuç | süre | not" tablosu basar. Hata varsa exit kodu 1.
//
// FAZ 1 DURUMU: senaryolar henüz yazılmadı (Faz 9). Komut şu an yalnızca
// API'nin /health ucuna erişebildiğini kontrol eder ve bunu tablo biçiminde
// bildirir; kalan kimlikleri "YAPILMADI" olarak listeler ki eksiklik
// gizlenmesin. Eksik kimlik bulunduğu için exit kodu 1 döner.
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"
)

type result struct {
	id      string
	status  string
	elapsed time.Duration
	note    string
}

func main() {
	base := os.Getenv("API_BASE_URL")
	if base == "" {
		base = "http://localhost:8080"
	}

	results := []result{checkHealth(base)}

	// Henüz yazılmamış kimlikler açıkça işaretlenir.
	pending := []string{
		"F-1", "F-2", "F-3", "F-4", "F-5", "F-6", "F-7",
		"T-1", "T-2", "T-3", "T-4", "T-5",
		"AUTH-401", "AUTH-403", "OWNERSHIP",
	}
	for _, id := range pending {
		results = append(results, result{id, "YAPILMADI", 0, "Faz 9'da yazılacak"})
	}

	failed := printTable(results)
	if failed > 0 {
		fmt.Printf("\n%d kimlik başarısız veya eksik — doğrulama BAŞARISIZ\n", failed)
		os.Exit(1)
	}
	fmt.Println("\ntüm kimlikler geçti")
}

func checkHealth(base string) result {
	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/health", nil)
	if err != nil {
		return result{"HEALTH", "FAIL", time.Since(start), err.Error()}
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return result{"HEALTH", "FAIL", time.Since(start), "api'ye ulaşılamadı"}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return result{"HEALTH", "FAIL", time.Since(start),
			fmt.Sprintf("beklenen 200, gelen %d", resp.StatusCode)}
	}
	return result{"HEALTH", "PASS", time.Since(start), "db ping dahil"}
}

func printTable(rs []result) int {
	fmt.Printf("\n%-12s | %-9s | %8s | %s\n", "KİMLİK", "SONUÇ", "SÜRE", "NOT")
	fmt.Println("-------------+-----------+----------+--------------------------------")
	failed := 0
	for _, r := range rs {
		if r.status != "PASS" {
			failed++
		}
		fmt.Printf("%-12s | %-9s | %7dms | %s\n",
			r.id, r.status, r.elapsed.Milliseconds(), r.note)
	}
	return failed
}
