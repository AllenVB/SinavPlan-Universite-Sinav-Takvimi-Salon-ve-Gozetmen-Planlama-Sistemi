package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/AllenVB/sinavplan/backend/internal/simdata"
)

// Faz 22 A3: komut paleti araması ve "Öğrenci 360".
func TestSearchAndStudentOverview(t *testing.T) {
	s, env, _, _ := scheduledServer(t)
	ctx := context.Background()
	sec, _ := login(t, s, simdata.EmailSecretary)
	stu, _ := login(t, s, simdata.EmailStudent)

	var me, other int64
	var meNo, meName string
	if err := env.Owner.Pool.QueryRow(ctx, `SELECT st.id, st.student_no, st.full_name FROM "wm-students" st JOIN "wm-uo-users" u ON u.id = st.user_id WHERE u.email = $1`, simdata.EmailStudent).Scan(&me, &meNo, &meName); err != nil {
		t.Fatal(err)
	}
	_ = env.Owner.Pool.QueryRow(ctx, `SELECT min(id) FROM "wm-students" WHERE id <> $1`, me).Scan(&other)

	find := func(q string) []any {
		t.Helper()
		code, out := do(t, s, http.MethodGet, "/search?q="+url.QueryEscape(q), sec, "", "")
		if code != http.StatusOK {
			t.Fatalf("arama %q: %d %v", q, code, out)
		}
		items, _ := out["items"].([]any)
		return items
	}
	hasStudent := func(items []any, id int64) bool {
		for _, it := range items {
			h := it.(map[string]any)
			if h["kind"] == "student" && int64(h["id"].(float64)) == id {
				return true
			}
		}
		return false
	}
	if !hasStudent(find(meNo[:len(meNo)-1]), me) {
		t.Fatalf("numara önekiyle öğrenci %d bulunamadı", me)
	}
	if !hasStudent(find(strings.Fields(meName)[0]), me) {
		t.Fatalf("adla öğrenci bulunamadı (%s)", meName)
	}
	if items := find("%_%"); len(items) != 0 {
		t.Fatalf("LIKE jokerleri düz metin sayılmalı, %d sonuç döndü", len(items))
	}
	if items := find("a"); len(items) != 0 {
		t.Fatalf("tek karakterlik arama boş dönmeli: %d", len(items))
	}
	if code, _ := do(t, s, http.MethodGet, "/search?q="+strings.Repeat("x", 65), sec, "", ""); code != http.StatusUnprocessableEntity {
		t.Fatalf("65 karakter → 422: %d", code)
	}
	if code, _ := do(t, s, http.MethodGet, "/search?q=ab", stu, "", ""); code != http.StatusForbidden {
		t.Fatalf("öğrenci arama yapamaz (403): %d", code)
	}

	// 360: sekreter herkesi, öğrenci yalnız kendini görür; sınavlar /students/{id}/exams ile aynı
	code, ov := do(t, s, http.MethodGet, fmt.Sprintf("/students/%d/overview", me), sec, "", "")
	if code != http.StatusOK {
		t.Fatalf("360: %d %v", code, ov)
	}
	st := ov["student"].(map[string]any)
	if st["student_no"] != meNo || st["department"] == "" {
		t.Fatalf("360 kimlik: %v", st)
	}
	_, ex := do(t, s, http.MethodGet, fmt.Sprintf("/students/%d/exams", me), sec, "", "")
	if len(ov["exams"].([]any)) != len(ex["items"].([]any)) || len(ov["exams"].([]any)) == 0 {
		t.Fatalf("360 sınavları %d ≠ program %d", len(ov["exams"].([]any)), len(ex["items"].([]any)))
	}
	if code, _ := do(t, s, http.MethodGet, fmt.Sprintf("/students/%d/overview", me), stu, "", ""); code != http.StatusOK {
		t.Fatalf("öğrenci kendi 360'ı: %d", code)
	}
	if code, _ := do(t, s, http.MethodGet, fmt.Sprintf("/students/%d/overview", other), stu, "", ""); code != http.StatusForbidden {
		t.Fatalf("başka öğrencinin 360'ı 403 olmalı: %d", code)
	}
	if code, _ := do(t, s, http.MethodGet, "/students/99999999/overview", sec, "", ""); code != http.StatusNotFound {
		t.Fatalf("bilinmeyen öğrenci 404: %d", code)
	}
}
