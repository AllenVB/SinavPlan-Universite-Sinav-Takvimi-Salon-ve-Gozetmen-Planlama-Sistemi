package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

func checkHealth(ctx context.Context, e *env, f *failures) string {
	for _, p := range []string{"/health", "/metrics", "/docs", "/openapi.yaml"} {
		r, err := e.a.get(ctx, p, "")
		f.expectCode("GET "+p, r, err, 200)
	}
	r, _ := e.a.get(ctx, "/metrics", "")
	f.check(strings.Contains(string(r.raw), "# HELP"), "/metrics Prometheus biçiminde değil")
	if err := e.login(ctx); err != nil {
		f.check(false, "demo girişleri: %v", err)
	}
	return "health(db ping), metrics, docs, openapi; 5 demo rolü giriş yaptı"
}

func checkAuth401(ctx context.Context, e *env, f *failures) string {
	bad, _ := e.a.post(ctx, "/auth/login", "", M{"email": accounts["secretary"], "password": "yanlis-parola"})
	unk, _ := e.a.post(ctx, "/auth/login", "", M{"email": "yok@sinavplan.test", "password": "yanlis-parola"})
	f.check(bad.code == 401 && unk.code == 401, "yanlış parola/bilinmeyen kullanıcı: %d/%d (401 bekleniyor)", bad.code, unk.code)
	f.check(string(bad.raw) == string(unk.raw), "kullanıcı varlığı sızıyor: iki 401 gövdesi farklı")
	for _, tok := range []string{"", "garbage.token.value"} {
		r, _ := e.a.get(ctx, "/me", tok)
		f.check(r.code == 401, "token=%q ile /me: %d (401 bekleniyor)", tok, r.code)
	}
	// refresh rotasyonu: eski token tekrar kullanılırsa tüm zincir iptal olur.
	l, _ := e.a.post(ctx, "/auth/login", "", M{"email": accounts["secretary"], "password": e.password})
	rt1 := str(l.body, "refresh_token")
	r2, _ := e.a.post(ctx, "/auth/refresh", "", M{"refresh_token": rt1})
	rt2 := str(r2.body, "refresh_token")
	f.check(r2.code == 200 && rt2 != "" && rt2 != rt1, "refresh: HTTP %d, token döndü mü: %v", r2.code, rt2 != "")
	reuse, _ := e.a.post(ctx, "/auth/refresh", "", M{"refresh_token": rt1})
	f.check(reuse.code == 401, "kullanılmış refresh token yeniden kabul edildi: %d", reuse.code)
	chain, _ := e.a.post(ctx, "/auth/refresh", "", M{"refresh_token": rt2})
	f.check(chain.code == 401, "yeniden kullanım tespitinde zincir iptal edilmedi: %d", chain.code)
	return "yanlış parola≡bilinmeyen kullanıcı, token yok/bozuk, refresh yeniden-kullanım zinciri iptali"
}

func checkAuth403(ctx context.Context, e *env, f *failures) string {
	type c struct {
		role, method, path string
		body               any
	}
	cases := []c{
		{"student", "GET", "/students", nil},
		{"student", "POST", "/schedules", M{"period_id": 1}},
		{"invigilator", "POST", "/imports/students", nil},
		{"lecturer", "POST", "/rooms", M{"code": "X"}},
		{"secretary", "POST", "/excuses/1/decision", M{"decision": "approved"}},
		{"lecturer", "POST", "/schedules/1/invigilators/1/remove", nil},
		{"student", "POST", "/attendance/qr", M{"token": "x", "device_id": "y"}},
	}
	for _, x := range cases {
		var r resp
		var err error
		if x.method == "GET" {
			r, err = e.a.get(ctx, x.path, e.tok[x.role])
		} else {
			r, err = e.a.post(ctx, x.path, e.tok[x.role], x.body)
		}
		f.check(err == nil && r.code == 403, "%s %s %s: HTTP %d (403 bekleniyor)", x.role, x.method, x.path, r.code)
	}
	return fmt.Sprintf("%d rol×uç kombinasyonu 403", len(cases))
}

func checkF1(ctx context.Context, e *env, f *failures) string {
	tok := e.tok["secretary"]
	n := e.runID
	sNo1, sNo2 := fmt.Sprintf("%06d", 100000+n), fmt.Sprintf("%06d", 100000+(n+1)%900000)
	course := fmt.Sprintf("VRF%d", n)
	students := "student_no,full_name,department_code,disability\n" +
		sNo1 + ",Doğrulama Öğrenci,BLM,false\n" +
		sNo2 + ",Doğrulama Engelli,ELK,true\n" +
		"9,Hatalı Numara,BLM,false\n" +
		sNo1[:5] + "7,Bilinmeyen Bölüm,XXX,false\n"
	r, err := e.a.csv(ctx, "/imports/students", tok, students)
	if f.expectCode("öğrenci içe aktarma", r, err, 200) {
		f.check(num(r.body, "created") == 2, "created=%d (2 bekleniyor)", num(r.body, "created"))
		lines := map[int64]bool{}
		for _, x := range arr(r.body, "errors") {
			if m, ok := x.(M); ok {
				lines[num(m, "line")] = true
			}
		}
		f.check(len(lines) == 2 && lines[4] && lines[5], "hatalı satırlar %v (4 ve 5 bekleniyor)", lines)
	}
	r, err = e.a.csv(ctx, "/imports/students", tok, students)
	if f.expectCode("tekrar yükleme", r, err, 200) {
		f.check(num(r.body, "created") == 0, "aynı dosya tekrar: created=%d (0 bekleniyor)", num(r.body, "created"))
	}
	r, err = e.a.csv(ctx, "/imports/students", tok, "yanlis,baslik\n1,2\n")
	f.expectCode("başlık hatası", r, err, 422)
	r, err = e.a.csv(ctx, "/imports/nope", tok, "a\n1\n")
	f.expectCode("bilinmeyen tür", r, err, 404)

	steps := []struct{ kind, csv string }{
		{"courses", "code,name,department_code,year,requires_lab\n" + course + ",Doğrulama Dersi,BLM,1,false\n"},
		{"sections", "course_code,section_code,lecturer_email\n" + course + ",A,hoca@sinavplan.test\n"},
		{"enrollments", "student_no,course_code,section_code\n" + sNo1 + "," + course + ",A\n"},
	}
	for _, s := range steps {
		r, err := e.a.csv(ctx, "/imports/"+s.kind, tok, s.csv)
		if f.expectCode(s.kind+" içe aktarma", r, err, 200) {
			f.check(num(r.body, "created") == 1 && len(arr(r.body, "errors")) == 0, "%s: created=%d errors=%v", s.kind, num(r.body, "created"), arr(r.body, "errors"))
		}
		r, err = e.a.csv(ctx, "/imports/"+s.kind, tok, s.csv)
		if f.expectCode(s.kind+" tekrar", r, err, 200) {
			f.check(num(r.body, "created") == 0, "%s tekrar: created=%d", s.kind, num(r.body, "created"))
		}
	}
	return "4 tür (öğrenci/ders/şube/kayıt), satır hataları 4,5, tekrar→created=0, başlık 422, tür 404"
}

func checkF2(ctx context.Context, e *env, f *failures) string {
	tok := e.tok["secretary"]
	body := M{"code": fmt.Sprintf("VRF-%d", e.runID), "building": "Z", "floor": 1, "type": "classic", "rows": 4, "cols": 6,
		"inactive_seats": [][]int{{3, 5}}, "accessible_seats": [][]int{{0, 0}, {0, 2}}}
	r, err := e.a.post(ctx, "/rooms", tok, body)
	if !f.expectCode("salon oluştur", r, err, 201) {
		return ""
	}
	id := num(r.body, "id")
	path := fmt.Sprintf("/rooms/%d", id)
	g, err := e.a.get(ctx, path, tok)
	if f.expectCode("salon getir", g, err, 200) {
		f.check(num(g.body, "capacity") == 23, "kapasite=%d (4x6-1 pasif=23)", num(g.body, "capacity"))
		f.check(num(g.body, "accessible_seats") == 2, "erişilebilir koltuk=%d (2)", num(g.body, "accessible_seats"))
		f.check(len(arr(g.body, "seats")) == 24, "koltuk sayısı=%d (24)", len(arr(g.body, "seats")))
	}
	d, err := e.a.post(ctx, "/rooms", tok, body)
	f.expectCode("yinelenen kod", d, err, 409)
	bad := M{"code": "BAD", "building": "Z", "floor": 1, "type": "classic", "rows": 0, "cols": 6}
	v, err := e.a.post(ctx, "/rooms", tok, bad)
	f.expectCode("geçersiz düzen", v, err, 422)
	f.check(len(obj(obj(v.body, "error"), "fields")) > 0, "422 alan hataları içermiyor")
	body["floor"] = 2
	u, err := e.a.do(ctx, "PUT", path, tok, "application/json", jsonReader(body))
	if f.expectCode("salon güncelle", u, err, 200) {
		f.check(num(u.body, "floor") == 2, "kat güncellenmedi")
	}
	l, err := e.a.get(ctx, "/rooms?type=lab&limit=5", e.tok["lecturer"])
	if f.expectCode("lab listesi", l, err, 200) {
		items := arr(l.body, "items")
		f.check(len(items) == 5 && str(l.body, "next_cursor") != "", "keyset sayfalama: %d öğe, next_cursor=%q", len(items), str(l.body, "next_cursor"))
		for _, it := range items {
			if m, ok := it.(M); ok {
				f.check(str(m, "type") == "lab", "lab filtresi: %v", str(m, "type"))
			}
		}
	}
	x, err := e.a.do(ctx, "DELETE", path, tok, "", nil)
	f.expectCode("salon sil", x, err, 204)
	g, err = e.a.get(ctx, path, tok)
	f.expectCode("silinen salon", g, err, 404)
	return "oluştur/getir(kapasite 23)/yinelenen 409/geçersiz 422/güncelle/lab filtresi+keyset/sil→404"
}

func (e *env) pollJob(ctx context.Context, id int64) (M, error) {
	deadline := time.Now().Add(8 * time.Minute)
	for time.Now().Before(deadline) {
		r, err := e.a.get(ctx, fmt.Sprintf("/schedule-jobs/%d", id), e.tok["secretary"])
		if err != nil {
			return nil, err
		}
		switch str(r.body, "status") {
		case "succeeded":
			return r.body, nil
		case "failed":
			return r.body, fmt.Errorf("iş %d başarısız: %s", id, trunc(string(r.raw), 300))
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
	return nil, fmt.Errorf("iş %d zaman aşımı", id)
}

func checkF3(ctx context.Context, e *env, f *failures) string {
	tok := e.tok["secretary"]
	p, err := e.a.get(ctx, "/periods", tok)
	if !f.expectCode("dönemler", p, err, 200) || len(arr(p.body, "items")) == 0 {
		f.check(false, "dönem yok")
		return ""
	}
	period := arr(p.body, "items")[0].(M)
	e.periodID, e.periodExams = num(period, "id"), num(period, "exams")
	start := time.Now()
	run := func() (int64, error) {
		r, err := e.a.post(ctx, "/schedules", tok, M{"period_id": e.periodID, "seed": 42, "iterations": 200000})
		if err != nil || r.code != 202 {
			return 0, fmt.Errorf("takvim başlatma: HTTP %d %s %v", r.code, trunc(string(r.raw), 120), err)
		}
		if dup, _ := e.a.post(ctx, "/schedules", tok, M{"period_id": e.periodID, "seed": 42, "iterations": 200000}); dup.code != 409 {
			f.check(false, "çalışan iş varken ikinci istek: HTTP %d (409 bekleniyor)", dup.code)
		}
		job, err := e.pollJob(ctx, num(r.body, "job_id"))
		if err != nil {
			return 0, err
		}
		return num(job, "schedule_id"), nil
	}
	var err1, err2 error
	e.sched1, err1 = run()
	e.sched2, err2 = run()
	if err1 != nil || err2 != nil {
		f.check(false, "iş hatası: %v %v", err1, err2)
		return ""
	}
	f.check(e.sched1 != e.sched2 && e.sched1 > 0, "iki iş aynı sürümü üretti: %d", e.sched1)
	jobTime := time.Since(start)

	// Yayın: Idempotency-Key ile tekrar aynı yanıt; anahtarsız ikinci yayın 409.
	key := fmt.Sprintf("vrf-publish-%d-%d", e.sched2, e.runID)
	path := fmt.Sprintf("/schedules/%d/publish", e.sched2)
	p1, err := e.a.post(ctx, path, tok, nil, "Idempotency-Key", key)
	if f.expectCode("yayınla", p1, err, 200) {
		p2, err := e.a.post(ctx, path, tok, nil, "Idempotency-Key", key)
		f.expectCode("yayın tekrarı (aynı anahtar)", p2, err, 200)
		f.check(string(p1.raw) == string(p2.raw), "aynı Idempotency-Key farklı yanıt döndü")
		p3, err := e.a.post(ctx, path, tok, nil)
		f.expectCode("anahtarsız ikinci yayın", p3, err, 409)
	}
	s, err := e.a.get(ctx, fmt.Sprintf("/schedules/%d", e.sched2), tok)
	if f.expectCode("sürüm", s, err, 200) {
		f.check(str(s.body, "status") == "published", "durum=%s", str(s.body, "status"))
		f.check(len(obj(s.body, "metrics")) > 0, "kalite metrikleri boş")
	}
	vs, err := e.a.get(ctx, fmt.Sprintf("/schedules?period_id=%d", e.periodID), tok)
	if f.expectCode("sürüm listesi", vs, err, 200) {
		f.check(len(arr(vs.body, "items")) >= 2, "sürüm sayısı=%d", len(arr(vs.body, "items")))
	}
	sn1, err1 := loadSnapshot(ctx, e.a, tok, e.sched1)
	sn2, err2 := loadSnapshot(ctx, e.a, tok, e.sched2)
	if err1 != nil || err2 != nil {
		f.check(false, "çizelge okunamadı: %v %v", err1, err2)
		return ""
	}
	e.sn = sn2
	f.check(sn1.fingerprint() == sn2.fingerprint(), "aynı tohum+iterasyonla iki üretim farklı (takvim/salon/oturma/gözetmen)")
	f.check(len(sn2.exams) == int(e.periodExams), "takvimdeki sınav=%d, dönemdeki=%d", len(sn2.exams), e.periodExams)
	return fmt.Sprintf("2 iş sıralı (toplam %.0fs), 2. istek 409, aynı tohum→özdeş çizelge, yayın idempotent", jobTime.Seconds())
}
