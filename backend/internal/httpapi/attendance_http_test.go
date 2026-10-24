package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AllenVB/sinavplan/backend/internal/auth"
	"github.com/AllenVB/sinavplan/backend/internal/planning"
	"github.com/AllenVB/sinavplan/backend/internal/simdata"
	"github.com/AllenVB/sinavplan/backend/internal/store"
	"github.com/AllenVB/sinavplan/backend/internal/testdb"
	"github.com/google/uuid"
)

const httpQRSecret = "http-test-qr-secret-0123456789-0123"

// scheduledServer: küçük veri + yayınlanmış çizelge + QR yapılandırılmış sunucu.
func scheduledServer(t *testing.T) (*Server, *testdb.Env, store.SchedulePlan, int64) {
	t.Helper()
	env, ds := testdb.Seeded(t)
	sp, err := planning.BuildAll(ds, 7, 20_000)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	sid, _, err := store.CreateSchedule(ctx, env.App.Pool, sp)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.PublishSchedule(ctx, env.App.Pool, sid, 1); err != nil {
		t.Fatal(err)
	}
	svc, _ := auth.NewService(env.App.Pool, auth.NewIssuer(rbacSecret, 15*time.Minute), 7*24*time.Hour)
	s := NewServer(Options{DB: env.App, Pool: env.App.Pool, Auth: svc, QR: store.NewQR(httpQRSecret),
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	return s, env, sp, sid
}

func post(t *testing.T, h http.Handler, path, token, idemKey string, body any) (int, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+token)
	if idemKey != "" {
		req.Header.Set("Idempotency-Key", idemKey)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// F-7 uçtan uca: öğrenci kendi sınav kartını alır, gözetmen okutur, ikinci okutma 409.
func TestExamCardFlowEndToEnd(t *testing.T) {
	s, _, sp, _ := scheduledServer(t)
	stu, _ := login(t, s, simdata.EmailStudent)
	inv, _ := login(t, s, simdata.EmailInvigilator)

	code, prog := call(t, s, http.MethodGet, "/me/exams", stu, nil)
	items, _ := prog["items"].([]any)
	if code != http.StatusOK || len(items) == 0 {
		t.Fatalf("öğrencinin yayınlanmış programı boş olmamalı: %d %v", code, prog)
	}
	first := items[0].(map[string]any)
	if first["room_code"] == nil || first["seat_row"] == nil {
		t.Fatalf("programda salon ve koltuk olmalı: %v", first)
	}
	examID := int64(first["exam_id"].(float64))

	code, card := post(t, s, "/me/exams/"+itoa(examID)+"/card", stu, "", nil)
	if code != http.StatusCreated || card["qr_token"] == "" {
		t.Fatalf("kart: %d %v", code, card)
	}
	tok := card["qr_token"].(string)

	if code, out := post(t, s, "/attendance/qr", inv, "", map[string]string{"token": tok, "device_id": "tablet-9"}); code != http.StatusOK {
		t.Fatalf("ilk okutma 200 olmalı: %d %v", code, out)
	}
	code, out := post(t, s, "/attendance/qr", inv, "", map[string]string{"token": tok, "device_id": "tablet-9"})
	if code != http.StatusConflict || out["error"].(map[string]any)["code"] != "qr_already_used" {
		t.Fatalf("ikinci okutma 409 qr_already_used olmalı: %d %v", code, out)
	}

	// Yoklama özeti, sınav gözetmen/sekreter tarafından görülür.
	sec, _ := login(t, s, simdata.EmailSecretary)
	code, att := call(t, s, http.MethodGet, "/exams/"+itoa(examID)+"/attendance", sec, nil)
	if code != http.StatusOK || att["summary"].(map[string]any)["present"].(float64) != 1 {
		t.Fatalf("yoklama özeti: %d %v", code, att)
	}

	// Öğrenci kayıtlı olmadığı bir sınavın kartını alamaz (404: varlığı sızdırılmaz).
	var other int64
	mine := map[int64]bool{}
	for _, it := range items {
		mine[int64(it.(map[string]any)["exam_id"].(float64))] = true
	}
	for _, e := range sp.Exams {
		if !mine[e.ExamID] {
			other = e.ExamID
			break
		}
	}
	if code, _ := post(t, s, "/me/exams/"+itoa(other)+"/card", stu, "", nil); code != http.StatusNotFound {
		t.Fatalf("başkasının sınav kartı 404 olmalı: %d", code)
	}
	// Uydurma token 422.
	if code, _ := post(t, s, "/attendance/qr", inv, "", map[string]string{"token": "uydurma", "device_id": "t"}); code != http.StatusUnprocessableEntity {
		t.Fatalf("uydurma token 422 olmalı: %d", code)
	}
	// Öğrenci QR okutamaz (yetkisiz rol).
	if code, _ := post(t, s, "/attendance/qr", stu, "", map[string]string{"token": tok, "device_id": "t"}); code != http.StatusForbidden {
		t.Fatalf("öğrenci QR okutamamalı: %d", code)
	}
}

// YARIŞ TESTİ (HTTP): 50 eşzamanlı okutma, aynı QR -> tam 1×200 ve 49×409.
func TestQRFiftyConcurrentScansOverHTTP(t *testing.T) {
	s, env, sp, _ := scheduledServer(t)
	inv, _ := login(t, s, simdata.EmailInvigilator)
	seat := sp.Seats[0]
	tok, err := store.NewQR(httpQRSecret).Issue(context.Background(), env.App.Pool, seat.ExamID, seat.StudentID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var c200, c409, other atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			code, _ := post(t, s, "/attendance/qr", inv, "", map[string]string{"token": tok, "device_id": "tablet-x"})
			switch code {
			case http.StatusOK:
				c200.Add(1)
			case http.StatusConflict:
				c409.Add(1)
			default:
				other.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if c200.Load() != 1 || c409.Load() != 49 || other.Load() != 0 {
		t.Fatalf("1×200 + 49×409 bekleniyordu: %d×200, %d×409, %d diğer", c200.Load(), c409.Load(), other.Load())
	}
}

// Idempotency-Key ile tekrarlanan okutma, 409 değil İLK yanıtı (200) döner: ağ kesilip
// tekrar gönderen tablet "zaten okutuldu" hatası görmez.
func TestQRRetryWithIdempotencyKeyReplaysSuccess(t *testing.T) {
	s, env, sp, _ := scheduledServer(t)
	inv, _ := login(t, s, simdata.EmailInvigilator)
	seat := sp.Seats[1]
	tok, _ := store.NewQR(httpQRSecret).Issue(context.Background(), env.App.Pool, seat.ExamID, seat.StudentID, time.Now().Add(time.Hour))
	body := map[string]string{"token": tok, "device_id": "tablet-r"}
	c1, b1 := post(t, s, "/attendance/qr", inv, "retry-1", body)
	c2, b2 := post(t, s, "/attendance/qr", inv, "retry-1", body)
	if c1 != http.StatusOK || c2 != http.StatusOK || b1["event_id"] != b2["event_id"] {
		t.Fatalf("aynı anahtarla tekrar aynı 200 yanıtını vermeli: %d %v / %d %v", c1, b1, c2, b2)
	}
}

func TestSyncOverHTTP(t *testing.T) {
	s, _, sp, _ := scheduledServer(t)
	inv, _ := login(t, s, simdata.EmailInvigilator)
	stu, _ := login(t, s, simdata.EmailStudent)
	seat := sp.Seats[0]
	id, _ := uuid.NewV7()
	ev := map[string]any{"event_id": id.String(), "device_id": "tablet-1", "exam_id": seat.ExamID, "student_id": seat.StudentID,
		"status": "present", "source": "manual", "hlc": map[string]any{"wall": 1000, "counter": 0}}
	code, out := post(t, s, "/attendance/sync", inv, "", map[string]any{"events": []any{ev}})
	if code != http.StatusOK || out["accepted"].(float64) != 1 {
		t.Fatalf("senkron: %d %v", code, out)
	}
	code, out = post(t, s, "/attendance/sync", inv, "", map[string]any{"events": []any{ev}})
	if code != http.StatusOK || out["duplicates"].(float64) != 1 || out["accepted"].(float64) != 0 {
		t.Fatalf("tekrar gönderim yok sayılmalı: %d %v", code, out)
	}
	if code, _ := post(t, s, "/attendance/sync", inv, "", map[string]any{"events": []any{}}); code != http.StatusUnprocessableEntity {
		t.Fatalf("boş paket 422 olmalı: %d", code)
	}
	if code, _ := post(t, s, "/attendance/sync", stu, "", map[string]any{"events": []any{ev}}); code != http.StatusForbidden {
		t.Fatalf("öğrenci senkron yapamamalı: %d", code)
	}
}

func TestRoomChangeValidationOverHTTP(t *testing.T) {
	s, _, _, sid := scheduledServer(t)
	sec, _ := login(t, s, simdata.EmailSecretary)
	stu, _ := login(t, s, simdata.EmailStudent)
	path := "/schedules/" + itoa(sid) + "/room-change"
	if code, _ := post(t, s, path, sec, "", map[string]any{"slot_id": 1, "from_room_id": 2, "to_room_id": 2, "reason": "x"}); code != http.StatusUnprocessableEntity {
		t.Fatalf("aynı salon 422 olmalı: %d", code)
	}
	if code, _ := post(t, s, path, sec, "", map[string]any{"slot_id": 1, "from_room_id": 2, "to_room_id": 3}); code != http.StatusUnprocessableEntity {
		t.Fatalf("gerekçesiz 422 olmalı: %d", code)
	}
	if code, _ := post(t, s, path, stu, "", map[string]any{"slot_id": 1, "from_room_id": 2, "to_room_id": 3, "reason": "x"}); code != http.StatusForbidden {
		t.Fatalf("öğrenci tahliye başlatamamalı: %d", code)
	}
	if code, _ := post(t, s, path, sec, "", map[string]any{"slot_id": 99999, "from_room_id": 2, "to_room_id": 3, "reason": "x"}); code != http.StatusNotFound {
		t.Fatalf("boş salon/slot 404 olmalı: %d", code)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
