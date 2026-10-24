package store_test

import (
	"context"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AllenVB/sinavplan/backend/internal/domain/attendance"
	"github.com/AllenVB/sinavplan/backend/internal/domain/seating"
	"github.com/AllenVB/sinavplan/backend/internal/planning"
	"github.com/AllenVB/sinavplan/backend/internal/simdata"
	"github.com/AllenVB/sinavplan/backend/internal/store"
	"github.com/AllenVB/sinavplan/backend/internal/testdb"
	"github.com/google/uuid"
)

const qrSecret = "test-qr-secret-0123456789-0123456789"

// withSchedule: küçük veri + T-1..T-4 planı yazılmış ve yayınlanmış veritabanı.
func withSchedule(t *testing.T) (*testdb.Env, *simdata.Dataset, store.SchedulePlan, int64) {
	t.Helper()
	return withScheduleOf(t, testdb.SmallDataset(t, 7))
}

func withScheduleOf(t *testing.T, data *simdata.Dataset) (*testdb.Env, *simdata.Dataset, store.SchedulePlan, int64) {
	t.Helper()
	env, ds := testdb.SeededWith(t, data)
	sp, err := planning.BuildAll(ds, 7, 20_000)
	if err != nil {
		t.Fatal(err)
	}
	sp.CreatedBy = 1
	id, v, err := store.CreateSchedule(context.Background(), env.App.Pool, sp)
	if err != nil || v != 1 {
		t.Fatalf("çizelge yazılamadı: %v (sürüm %d)", err, v)
	}
	if err := store.PublishSchedule(context.Background(), env.App.Pool, id, 1); err != nil {
		t.Fatal(err)
	}
	return env, ds, sp, id
}

func v7(t *testing.T) string {
	u, err := uuid.NewV7()
	if err != nil {
		t.Fatal(err)
	}
	return u.String()
}

func TestScheduleVersionsAndPublish(t *testing.T) {
	env, _, sp, first := withSchedule(t)
	ctx := context.Background()
	second, v, err := store.CreateSchedule(ctx, env.App.Pool, sp)
	if err != nil || v != 2 {
		t.Fatalf("ikinci sürüm: %v %d", err, v)
	}
	if err := store.PublishSchedule(ctx, env.App.Pool, second, 1); err != nil {
		t.Fatal(err)
	}
	var st1, st2 string
	_ = env.App.Pool.QueryRow(ctx, `SELECT status FROM schedules WHERE id=$1`, first).Scan(&st1)
	_ = env.App.Pool.QueryRow(ctx, `SELECT status FROM schedules WHERE id=$1`, second).Scan(&st2)
	if st1 != "draft" || st2 != "published" {
		t.Fatalf("yeni sürüm yayınlanınca eskisi taslağa dönmeli: %s / %s", st1, st2)
	}
	if err := store.PublishSchedule(ctx, env.App.Pool, second, 1); !errors.Is(err, store.ErrConflict) {
		t.Fatalf("zaten yayınlanmış: ErrConflict bekleniyordu: %v", err)
	}
	if err := store.PublishSchedule(ctx, env.App.Pool, 999999, 1); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("olmayan çizelge: %v", err)
	}
	var n int
	_ = env.App.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE entity='schedule'`).Scan(&n)
	if n != 4 { // 2 create + 2 publish
		t.Fatalf("4 denetim kaydı bekleniyordu: %d", n)
	}
}

// --- Senkron ---------------------------------------------------------------------

func firstSeat(sp store.SchedulePlan) store.PlanSeat { return sp.Seats[0] }

func TestSyncIsIdempotentAndRejectsInvalid(t *testing.T) {
	env, _, sp, _ := withSchedule(t)
	ctx := context.Background()
	s := firstSeat(sp)
	batch := []attendance.Event{
		{ID: v7(t), DeviceID: "tablet-1", ExamID: s.ExamID, StudentID: s.StudentID, Status: "present", Source: "manual", HLC: attendance.HLC{Wall: 1000}},
		{ID: v7(t), DeviceID: "tablet-1", ExamID: s.ExamID, StudentID: s.StudentID, Status: "present", Source: "manual", HLC: attendance.HLC{Wall: 1001}},
	}
	r1, err := store.SyncAttendance(ctx, env.App.Pool, batch, 3)
	if err != nil || r1.Accepted != 2 || r1.Duplicates != 0 {
		t.Fatalf("ilk gönderim: %+v %v", r1, err)
	}
	// Çevrimdışı cihaz aynı paketi tekrar gönderir: hepsi yok sayılır.
	r2, err := store.SyncAttendance(ctx, env.App.Pool, batch, 3)
	if err != nil || r2.Accepted != 0 || r2.Duplicates != 2 {
		t.Fatalf("tekrar gönderim idempotent olmalı: %+v %v", r2, err)
	}
	var n int
	_ = env.App.Pool.QueryRow(ctx, `SELECT count(*) FROM attendance_events`).Scan(&n)
	if n != 2 {
		t.Fatalf("2 olay bekleniyordu: %d", n)
	}
	// Geçersizler.
	v4 := uuid.New().String()
	bad := []attendance.Event{
		{ID: v4, DeviceID: "t", ExamID: s.ExamID, StudentID: s.StudentID, Status: "present", Source: "manual"},
		{ID: v7(t), DeviceID: "t", ExamID: s.ExamID, StudentID: s.StudentID, Status: "late", Source: "manual"},
		{ID: v7(t), DeviceID: "t", ExamID: s.ExamID, StudentID: 999999, Status: "present", Source: "manual"},
	}
	r3, err := store.SyncAttendance(ctx, env.App.Pool, bad, 3)
	if err != nil || r3.Accepted != 0 || len(r3.Rejected) != 3 {
		t.Fatalf("3 olay reddedilmeli: %+v %v", r3, err)
	}
}

func TestSyncRecordsConflictWithDeterministicRule(t *testing.T) {
	env, _, sp, _ := withSchedule(t)
	ctx := context.Background()
	s := firstSeat(sp)
	qr := attendance.Event{ID: v7(t), DeviceID: "tablet-A", ExamID: s.ExamID, StudentID: s.StudentID, Status: "present", Source: "qr", HLC: attendance.HLC{Wall: 1000}}
	man := attendance.Event{ID: v7(t), DeviceID: "tablet-B", ExamID: s.ExamID, StudentID: s.StudentID, Status: "absent", Source: "manual", HLC: attendance.HLC{Wall: 5000}}
	// İki cihaz ayrı ayrı (çevrimdışıyken) gönderir; gelme sırası ters.
	if _, err := store.SyncAttendance(ctx, env.App.Pool, []attendance.Event{man}, 3); err != nil {
		t.Fatal(err)
	}
	r, err := store.SyncAttendance(ctx, env.App.Pool, []attendance.Event{qr}, 3)
	if err != nil || r.NewConflicts != 1 {
		t.Fatalf("1 çakışma bekleniyordu: %+v %v", r, err)
	}
	var status, src, rule string
	_ = env.App.Pool.QueryRow(ctx, `SELECT status, source FROM attendance_state WHERE exam_id=$1 AND student_id=$2`, s.ExamID, s.StudentID).Scan(&status, &src)
	_ = env.App.Pool.QueryRow(ctx, `SELECT rule FROM attendance_conflicts`).Scan(&rule)
	if status != "present" || src != "qr" || rule != "qr_present" {
		t.Fatalf("QR present kazanmalı: %s/%s kural %s", status, src, rule)
	}
}

// Eşzamanlı cihazlar aynı öğrenciler için olay gönderir; son durum, tüm olayların
// sıradan bağımsız çözümüne (attendance.Resolve) birebir eşit olmalı.
func TestConcurrentSyncConvergesToResolve(t *testing.T) {
	env, _, sp, _ := withSchedule(t)
	ctx := context.Background()
	var all []attendance.Event
	var batches [][]attendance.Event
	devices := []string{"t-1", "t-2", "t-3", "t-4", "t-5", "t-6"}
	for di, dev := range devices {
		var b []attendance.Event
		for k := 0; k < 15; k++ {
			s := sp.Seats[k%5] // 5 öğrenci, her cihaz hepsine dokunur
			st := "present"
			if (di+k)%3 == 0 {
				st = "absent"
			}
			e := attendance.Event{ID: v7(t), DeviceID: dev, ExamID: s.ExamID, StudentID: s.StudentID, Status: st,
				Source: "manual", HLC: attendance.HLC{Wall: int64(1000 + (di*7+k)%4), Counter: int32(k % 2)}}
			b = append(b, e)
			all = append(all, e)
		}
		batches = append(batches, b)
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(batches))
	for _, b := range batches {
		wg.Add(1)
		go func(b []attendance.Event) {
			defer wg.Done()
			if _, err := store.SyncAttendance(ctx, env.App.Pool, b, 3); err != nil {
				errs <- err
			}
		}(b)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	for _, want := range attendance.Resolve(all) {
		var got string
		if err := env.App.Pool.QueryRow(ctx, `SELECT event_id::text FROM attendance_state WHERE exam_id=$1 AND student_id=$2`,
			want.ExamID, want.StudentID).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != want.Winner.ID {
			t.Fatalf("öğrenci %d: durum %s, Resolve kazananı %s", want.StudentID, got, want.Winner.ID)
		}
		var n int
		_ = env.App.Pool.QueryRow(ctx, `SELECT count(*) FROM attendance_conflicts WHERE exam_id=$1 AND student_id=$2 AND winner_event_id=$3`,
			want.ExamID, want.StudentID, want.Winner.ID).Scan(&n)
		if n != len(want.Conflicts) {
			t.Fatalf("öğrenci %d: %d çakışma kaydı, Resolve %d", want.StudentID, n, len(want.Conflicts))
		}
	}
}

// --- QR --------------------------------------------------------------------------

// YARIŞ TESTİ: 50 goroutine aynı QR'ı kullanır -> tam 1 başarı, 49 ErrQRUsed.
func TestQRSingleUseUnderFiftyConcurrentScans(t *testing.T) {
	env, _, sp, _ := withSchedule(t)
	ctx := context.Background()
	q := store.NewQR(qrSecret)
	s := firstSeat(sp)
	tok, err := q.Issue(ctx, env.App.Pool, s.ExamID, s.StudentID, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var ok, used, other atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start // hepsi aynı anda
			_, err := q.Use(ctx, env.App.Pool, tok, "tablet-"+string(rune('A'+i%26)), 3)
			switch {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, store.ErrQRUsed):
				used.Add(1)
			default:
				other.Add(1)
				t.Errorf("beklenmeyen hata: %v", err)
			}
		}(i)
	}
	close(start)
	wg.Wait()
	if ok.Load() != 1 || used.Load() != 49 || other.Load() != 0 {
		t.Fatalf("1 başarı + 49 ErrQRUsed bekleniyordu: %d başarı, %d kullanılmış, %d diğer", ok.Load(), used.Load(), other.Load())
	}
	var events int
	_ = env.App.Pool.QueryRow(ctx, `SELECT count(*) FROM attendance_events WHERE source='qr'`).Scan(&events)
	if events != 1 {
		t.Fatalf("tam 1 QR yoklama olayı yazılmalı: %d", events)
	}
}

func TestQRForgedRevokedExpired(t *testing.T) {
	env, _, sp, _ := withSchedule(t)
	ctx := context.Background()
	q := store.NewQR(qrSecret)
	s := firstSeat(sp)
	// Başka sırla imzalanmış (uydurma) token: veritabanına gitmeden reddedilir.
	forged, _ := store.NewQR(qrSecret+"x").Issue(ctx, env.App.Pool, s.ExamID, s.StudentID, time.Now().Add(time.Hour))
	if _, err := q.Use(ctx, env.App.Pool, forged, "t", 3); !errors.Is(err, store.ErrQRInvalid) {
		t.Fatalf("uydurma token ErrQRInvalid vermeli: %v", err)
	}
	if _, err := q.Use(ctx, env.App.Pool, "bozuk!!", "t", 3); !errors.Is(err, store.ErrQRInvalid) {
		t.Fatalf("biçimsiz token: %v", err)
	}
	// Yeniden üretim eskisini iptal eder.
	old, _ := q.Issue(ctx, env.App.Pool, s.ExamID, s.StudentID, time.Now().Add(time.Hour))
	fresh, _ := q.Issue(ctx, env.App.Pool, s.ExamID, s.StudentID, time.Now().Add(time.Hour))
	if _, err := q.Use(ctx, env.App.Pool, old, "t", 3); !errors.Is(err, store.ErrQRExpired) {
		t.Fatalf("iptal edilmiş eski token: %v", err)
	}
	if _, err := q.Use(ctx, env.App.Pool, fresh, "t", 3); err != nil {
		t.Fatalf("yeni token çalışmalı: %v", err)
	}
	// Süresi dolmuş.
	exp, _ := q.Issue(ctx, env.App.Pool, sp.Seats[1].ExamID, sp.Seats[1].StudentID, time.Now().Add(-time.Minute))
	if _, err := q.Use(ctx, env.App.Pool, exp, "t", 3); !errors.Is(err, store.ErrQRExpired) {
		t.Fatalf("süresi dolmuş token: %v", err)
	}
	// DB'de token'ın kendisi değil yalnızca özeti var.
	var n int
	_ = env.App.Pool.QueryRow(ctx, `SELECT count(*) FROM qr_tokens WHERE token_hash = convert_to($1, 'UTF8')`, fresh).Scan(&n)
	if n != 0 {
		t.Fatal("ham token veritabanında saklanmamalı")
	}
}

// --- Toplu salon değişikliği ------------------------------------------------------

type roomLoad struct {
	slot, room int64
	students   int
}

func occupancy(t *testing.T, env *testdb.Env, schedule int64) map[[2]int64]int {
	rows, err := env.App.Pool.Query(context.Background(),
		`SELECT slot_id, room_id, count(*) FROM seat_assignments WHERE schedule_id=$1 GROUP BY 1,2`, schedule)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[[2]int64]int{}
	for rows.Next() {
		var s, r int64
		var n int
		_ = rows.Scan(&s, &r, &n)
		out[[2]int64{s, r}] = n
	}
	return out
}

func activeSeats(ds *simdata.Dataset) map[int64]int {
	out := map[int64]int{}
	for _, r := range ds.Rooms {
		out[r.ID] = r.Capacity
	}
	return out
}

func TestRoomChangeMovesEveryoneAtomically(t *testing.T) {
	env, ds, _, sid := withSchedule(t)
	ctx := context.Background()
	occ := occupancy(t, env, sid)
	act := activeSeats(ds)
	// Kaynak: bir slottaki dolu salon; hedef: aynı slotta boş ve yeterince büyük salon.
	var req *store.RoomChangeRequest
	keys := make([][2]int64, 0, len(occ))
	for k := range occ {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(a, b int) bool { return keys[a][0]*1000+keys[a][1] < keys[b][0]*1000+keys[b][1] })
	for _, k := range keys {
		for _, r := range ds.Rooms {
			if occ[[2]int64{k[0], r.ID}] == 0 && act[r.ID] >= 2*occ[k] && (r.Type == "lab") == false {
				req = &store.RoomChangeRequest{ScheduleID: sid, SlotID: k[0], FromRoomID: k[1], ToRoomID: r.ID, Reason: "yangın tatbikatı", ActorID: 1}
				break
			}
		}
		if req != nil {
			break
		}
	}
	if req == nil {
		t.Fatal("fikstürde uygun kaynak/hedef salon bulunamadı")
	}
	moved := occ[[2]int64{req.SlotID, req.FromRoomID}]
	res, err := store.RoomChange(ctx, env.App.Pool, *req)
	if err != nil || res.Moved != moved {
		t.Fatalf("taşıma: %+v %v (beklenen %d)", res, err, moved)
	}
	after := occupancy(t, env, sid)
	if after[[2]int64{req.SlotID, req.FromRoomID}] != 0 || after[[2]int64{req.SlotID, req.ToRoomID}] != moved {
		t.Fatalf("doluluk: kaynak %d, hedef %d", after[[2]int64{req.SlotID, req.FromRoomID}], after[[2]int64{req.SlotID, req.ToRoomID}])
	}
	var ev, au int
	_ = env.App.Pool.QueryRow(ctx, `SELECT count(*) FROM room_change_events`).Scan(&ev)
	_ = env.App.Pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE action='room.change'`).Scan(&au)
	if ev != 1 || au != 1 {
		t.Fatalf("olay ve denetim kaydı: %d / %d", ev, au)
	}
	// Yeni yerleşimde yan yana aynı sınav yok.
	var adj int
	_ = env.App.Pool.QueryRow(ctx, `
		SELECT count(*) FROM seat_assignments a JOIN room_seats sa ON sa.id=a.seat_id
		JOIN seat_assignments b ON b.schedule_id=a.schedule_id AND b.slot_id=a.slot_id AND b.room_id=a.room_id AND b.exam_id=a.exam_id
		JOIN room_seats sb ON sb.id=b.seat_id
		WHERE a.schedule_id=$1 AND a.room_id=$2 AND sa.row_no=sb.row_no AND sb.col_no=sa.col_no+1`, sid, req.ToRoomID).Scan(&adj)
	if adj != 0 {
		t.Fatalf("hedef salonda %d yan yana ihlali", adj)
	}
	// Hedef doluysa kapasite hatası ve hiçbir şey değişmez.
	back := *req
	back.FromRoomID, back.ToRoomID = req.ToRoomID, req.FromRoomID
	if _, err := store.RoomChange(ctx, env.App.Pool, back); err != nil {
		t.Fatalf("geri taşıma: %v", err)
	}
}

func loadSeatingRoom(t *testing.T, env *testdb.Env, id int64) seating.Room {
	rows, err := env.App.Pool.Query(context.Background(), `SELECT id, row_no, col_no, is_active, is_accessible FROM room_seats WHERE room_id=$1`, id)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	r := seating.Room{ID: id}
	for rows.Next() {
		var s seating.Seat
		var rr, cc int16
		_ = rows.Scan(&s.ID, &rr, &cc, &s.Active, &s.Accessible)
		s.Row, s.Col = int(rr), int(cc)
		r.Seats = append(r.Seats, s)
	}
	return r
}

func loadGroups(t *testing.T, env *testdb.Env, schedule, slot, room int64) []seating.Group {
	rows, err := env.App.Pool.Query(context.Background(), `SELECT sa.exam_id, sa.student_id, st.disability_flag
		FROM seat_assignments sa JOIN students st ON st.id = sa.student_id
		WHERE sa.schedule_id=$1 AND sa.slot_id=$2 AND sa.room_id=$3 ORDER BY 1,2`, schedule, slot, room)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	idx := map[int64]int{}
	var gs []seating.Group
	for rows.Next() {
		var e, st int64
		var d bool
		_ = rows.Scan(&e, &st, &d)
		i, ok := idx[e]
		if !ok {
			i = len(gs)
			idx[e] = i
			gs = append(gs, seating.Group{ExamID: e})
		}
		gs[i].Students = append(gs[i].Students, seating.Student{ID: st, Disabled: d})
	}
	return gs
}

// EŞZAMANLI İKİ TAHLİYE: hedef salona kaynaklardan yalnızca biri sığar. Tam biri
// başarılı olmalı, diğeri ErrCapacity almalı; hiçbir koltuk çift rezerve olmamalı.
// Senaryo, gerçek veri üzerinde seating.ArrangeInto ile kuru çalıştırılarak seçilir:
// A tek başına sığar, B tek başına sığar, A+B birlikte sığmaz.
func TestConcurrentEvacuationsIntoSameRoom(t *testing.T) {
	// Slot başına birden çok salon kullanılsın diye daha kalabalık (yapısı aynı) veri.
	data, err := simdata.Generate(simdata.Config{Seed: 7, Exams: 48, Students: 700, Rooms: 14, Days: 5, SessionsPerDay: 3, DisabilityRate: 0.02})
	if err != nil {
		t.Fatal(err)
	}
	env, ds, _, sid := withScheduleOf(t, data)
	ctx := context.Background()
	occ := occupancy(t, env, sid)
	act := activeSeats(ds)
	fits := func(target seating.Room, groups []seating.Group) bool {
		return len(seating.ArrangeInto(target, nil, groups, false).Unseated) == 0
	}
	type cand struct{ slot, a, b, target int64 }
	var c *cand
	slots := map[int64][]int64{}
	for k := range occ {
		slots[k[0]] = append(slots[k[0]], k[1])
	}
	slotIDs := make([]int64, 0, len(slots))
	for s := range slots {
		slotIDs = append(slotIDs, s)
	}
	sort.Slice(slotIDs, func(a, b int) bool { return slotIDs[a] < slotIDs[b] })
	for _, s := range slotIDs {
		used := slots[s]
		sort.Slice(used, func(a, b int) bool { return used[a] < used[b] })
		for i := 0; i < len(used) && c == nil; i++ {
			for j := i + 1; j < len(used) && c == nil; j++ {
				ga, gb := loadGroups(t, env, sid, s, used[i]), loadGroups(t, env, sid, s, used[j])
				na, nb := occ[[2]int64{s, used[i]}], occ[[2]int64{s, used[j]}]
				for _, r := range ds.Rooms {
					if r.Type != "classic" || occ[[2]int64{s, r.ID}] != 0 || act[r.ID] < max(na, nb) {
						continue
					}
					tr := loadSeatingRoom(t, env, r.ID)
					if fits(tr, ga) && fits(tr, gb) && !fits(tr, append(append([]seating.Group(nil), ga...), gb...)) {
						c = &cand{s, used[i], used[j], r.ID}
						break
					}
				}
			}
		}
		if c != nil {
			break
		}
	}
	if c == nil {
		t.Fatal("fikstürde uygun üçlü (iki kaynak, bir hedef) bulunamadı")
	}
	var ok, full atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, from := range []int64{c.a, c.b} {
		wg.Add(1)
		go func(from int64) {
			defer wg.Done()
			<-start
			_, err := store.RoomChange(ctx, env.App.Pool, store.RoomChangeRequest{ScheduleID: sid, SlotID: c.slot,
				FromRoomID: from, ToRoomID: c.target, Reason: "tahliye", ActorID: 1})
			switch {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, store.ErrCapacity):
				full.Add(1)
			default:
				t.Errorf("beklenmeyen hata: %v", err)
			}
		}(from)
	}
	close(start)
	wg.Wait()
	if ok.Load() != 1 || full.Load() != 1 {
		t.Fatalf("tam 1 başarı + 1 kapasite hatası bekleniyordu: %d / %d", ok.Load(), full.Load())
	}
	after := occupancy(t, env, sid)
	if after[[2]int64{c.slot, c.target}] > act[c.target] {
		t.Fatalf("hedef salon aşıldı: %d > %d", after[[2]int64{c.slot, c.target}], act[c.target])
	}
	var dup int
	_ = env.App.Pool.QueryRow(ctx, `SELECT count(*) FROM (SELECT slot_id, seat_id FROM seat_assignments WHERE schedule_id=$1
		GROUP BY 1,2 HAVING count(*) > 1) x`, sid).Scan(&dup)
	if dup != 0 {
		t.Fatalf("%d koltuk çift rezerve", dup)
	}
	var ev int
	_ = env.App.Pool.QueryRow(ctx, `SELECT count(*) FROM room_change_events`).Scan(&ev)
	if ev != 1 {
		t.Fatalf("tam 1 salon değişikliği olayı: %d", ev)
	}
}
