package planning

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/AllenVB/sinavplan/backend/internal/domain/attendance"
	"github.com/AllenVB/sinavplan/backend/internal/simdata"
	"github.com/AllenVB/sinavplan/backend/internal/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Demo: sistem boş görünmesin diye ilk kurulumda TÜM dönem için bir çizelge hazır gelir.
const (
	DemoCourseCode = "YZM406"
	DemoCourseName = "Prompt Mühendisliği"
	demoStudents   = 36 // tek şube; ders yalnızca Yazılım Mühendisliği 4. sınıf öğrencilerine açık
	demoIterations = 200_000
)

// SeedDemoExam, ilk kurulumda ana dönemi (tüm fakülteler, bölümler ve sınıflar) doldurur:
//
//  1. Yazılım Mühendisliği 4. sınıf seçmeli dersi "Prompt Mühendisliği" (Fahrettin Burak DEMİR) ve yalnızca
//     Yazılım Mühendisliği 4. sınıf öğrencilerinden 36 kişilik şubesi dönemin sınavlarına eklenir.
//  2. Dönemin TÜM sınavları için çizelge üretilir (T-1..T-4) ve yayınlanır. Prompt Mühendisliği, öğrencileriyle
//     çakışmayan en erken oturuma konur; gözetmeni Ömer ASLAN'dır.
//  3. Tüm oturumlar, Prompt Mühendisliği sınavı "şimdi başlamış" olacak biçimde (başlangıç = şimdi−20 dk) ileri/geri
//     kaydırılır; böylece Canlı izleme, tablet, öğrenci ve mazeret ekranları ilk açılışta doludur. Yoklamanın bir
//     kısmı girilmiş, bir mazeret de bekliyor.
//
// Diğer sınavlar planlanmış (yaklaşan) durumdadır; "Sınav provası" ve takvim üretimi tüm fakülteler için çalışır.
// İdempotenttir (dönemde zaten çizelge varsa dokunmaz).
func SeedDemoExam(ctx context.Context, pool *pgxpool.Pool, now time.Time, log *slog.Logger) error {
	var period int64
	if err := pool.QueryRow(ctx, `SELECT id FROM exam_periods ORDER BY id LIMIT 1`).Scan(&period); err != nil {
		return fmt.Errorf("dönem: %w", err)
	}
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schedules WHERE period_id = $1) OR EXISTS (SELECT 1 FROM courses WHERE code = $2)`,
		period, DemoCourseCode).Scan(&exists); err != nil {
		return err
	}
	if exists {
		log.Info("demo çizelgesi zaten var, atlanıyor")
		return nil
	}
	one := func(q string, args ...any) (int64, error) {
		var id int64
		err := pool.QueryRow(ctx, q, args...).Scan(&id)
		return id, err
	}
	yzm, err := one(`SELECT id FROM departments WHERE code = 'YZM'`)
	if err != nil {
		return fmt.Errorf("YZM bölümü: %w", err)
	}
	lecturer, err := one(`SELECT l.id FROM lecturers l JOIN users u ON u.id = l.user_id WHERE u.email = $1`, simdata.EmailLecturer)
	if err != nil {
		return fmt.Errorf("demo öğretim üyesi: %w", err)
	}
	omer, err := one(`SELECT id FROM users WHERE email = $1`, simdata.EmailInvigilator)
	if err != nil {
		return fmt.Errorf("gözetmen: %w", err)
	}
	secretary, err := one(`SELECT id FROM users WHERE email = $1`, simdata.EmailSecretary)
	if err != nil {
		return fmt.Errorf("sekreter: %w", err)
	}
	rows, err := pool.Query(ctx, `SELECT DISTINCT st.id FROM students st
		JOIN enrollments en ON en.student_id = st.id JOIN sections s ON s.id = en.section_id JOIN courses c ON c.id = s.course_id
		WHERE st.department_id = $1 AND c.department_id = $1 AND c.year = 4 ORDER BY st.id LIMIT $2`, yzm, demoStudents)
	if err != nil {
		return err
	}
	var students []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		students = append(students, id)
	}
	rows.Close()
	if len(students) < 6 {
		return fmt.Errorf("demo sınavı için yeterli Yazılım Mühendisliği 4. sınıf öğrencisi yok (%d)", len(students))
	}

	var course, section, exam int64
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tx.QueryRow(ctx, `INSERT INTO courses (department_id, code, name, year, requires_lab) VALUES ($1,$2,$3,4,false) RETURNING id`,
		yzm, DemoCourseCode, DemoCourseName).Scan(&course); err != nil {
		return fmt.Errorf("demo dersi: %w", err)
	}
	if err := tx.QueryRow(ctx, `INSERT INTO sections (course_id, code, lecturer_id) VALUES ($1,'A',$2) RETURNING id`, course, lecturer).Scan(&section); err != nil {
		return err
	}
	for _, st := range students {
		if _, err := tx.Exec(ctx, `INSERT INTO enrollments (student_id, section_id) VALUES ($1,$2)`, st, section); err != nil {
			return err
		}
	}
	if err := tx.QueryRow(ctx, `INSERT INTO exams (period_id, course_id, duration_minutes, requires_lab) VALUES ($1,$2,90,false) RETURNING id`, period, course).Scan(&exam); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	// 1) tüm dönem için çizelge
	ds, err := store.LoadDataset(ctx, pool, period, 1)
	if err != nil {
		return err
	}
	const seed = 42
	sp, _, err := BuildFrom(ds, seed, demoIterations, nil)
	if err != nil {
		return fmt.Errorf("demo çizelgesi: %w", err)
	}
	// 2) Prompt Mühendisliği: öğrencileriyle çakışmayan en erken oturum
	idx := SlotIndex(ds)
	assign := make(map[int64]int, len(sp.Exams))
	for _, e := range sp.Exams {
		assign[e.ExamID] = idx[e.SlotID]
	}
	demoSlot := -1
	for i := range ds.Slots {
		trial := make(map[int64]int, len(assign))
		for k, v := range assign {
			trial[k] = v
		}
		trial[exam] = i
		if len(CheckAssignment(ds, seed, trial).Violations) == 0 {
			if _, _, err := BuildFromAssignment(ds, seed, trial, nil); err == nil {
				assign, demoSlot = trial, i
				break
			}
		}
	}
	if demoSlot < 0 {
		return fmt.Errorf("Prompt Mühendisliği için uygun oturum bulunamadı")
	}
	sp, _, err = BuildFromAssignment(ds, seed, assign, nil)
	if err != nil {
		return fmt.Errorf("demo çizelgesi (sabitlenmiş): %w", err)
	}
	// 3) gözetmen Ömer ASLAN: Prompt Mühendisliği salonunun gözetmeniyle yer değiştirir
	demoSlotID := ds.Slots[demoSlot].ID
	var roomID int64
	for _, r := range sp.Rooms {
		if r.ExamID == exam {
			roomID = r.RoomID
		}
	}
	di, oi := -1, -1
	for i, iv := range sp.Invigilators {
		if iv.SlotID == demoSlotID && iv.RoomID == roomID {
			di = i
		}
		if iv.SlotID == demoSlotID && iv.UserID == omer {
			oi = i
		}
	}
	if di < 0 {
		return fmt.Errorf("demo salonu için gözetmen görevi yok")
	}
	if oi >= 0 && oi != di {
		sp.Invigilators[oi].UserID = sp.Invigilators[di].UserID
	}
	sp.Invigilators[di].UserID = omer
	sp.CreatedBy = secretary
	sid, _, err := store.CreateSchedule(ctx, pool, sp)
	if err != nil {
		return err
	}
	if err := store.PublishSchedule(ctx, pool, sid, secretary); err != nil {
		return err
	}

	// 4) dönemi kaydır: Prompt Mühendisliği oturumu şimdi başlamış olsun
	var startsAt time.Time
	if err := pool.QueryRow(ctx, `SELECT starts_at FROM slots WHERE id = $1`, demoSlotID).Scan(&startsAt); err != nil {
		return err
	}
	delta := now.Add(-20 * time.Minute).Truncate(time.Minute).Sub(startsAt)
	if _, err := pool.Exec(ctx, `UPDATE slots SET starts_at = starts_at + $2::interval, ends_at = ends_at + $2::interval WHERE period_id = $1`,
		period, fmt.Sprintf("%d seconds", int64(delta.Seconds()))); err != nil {
		return err
	}
	istanbul, lerr := time.LoadLocation("Europe/Istanbul")
	if lerr != nil {
		istanbul = time.FixedZone("UTC+3", 3*3600)
	}
	if _, err := pool.Exec(ctx, `UPDATE exam_periods SET start_date = (SELECT min(starts_at) FROM slots WHERE period_id = $1) AT TIME ZONE $2 WHERE id = $1`,
		period, istanbul.String()); err != nil {
		return err
	}

	// 5) yoklama bir kısmı girilmiş: 36 öğrencide 24 geldi, 3 gelmedi, kalanı işaretsiz (orantılı)
	// Demo öğrenci hesabı (ogrenci@) işaretsiz bırakılır: QR/kod ile kendini yoklamaya yazmayı deneyebilsin.
	var demoStudent int64
	_ = pool.QueryRow(ctx, `SELECT st.id FROM students st JOIN users u ON u.id = st.user_id WHERE u.email = $1`, simdata.EmailStudent).Scan(&demoStudent)
	others := make([]int64, 0, len(students))
	for _, st := range students {
		if st != demoStudent {
			others = append(others, st)
		}
	}
	nPresent, nAbsent := len(students)*2/3, max(1, len(students)/12)
	clock := attendance.Clock{Physical: func() int64 { return now.UnixMilli() }}
	var events []attendance.Event
	for i, st := range others {
		status := ""
		switch {
		case i < nPresent:
			status = "present"
		case i < nPresent+nAbsent:
			status = "absent"
		default:
			continue
		}
		id, _ := uuid.NewV7()
		events = append(events, attendance.Event{ID: id.String(), DeviceID: "tablet-demo-1", ExamID: exam, StudentID: st, Status: status, Source: "manual", HLC: clock.Now()})
	}
	if _, err := store.SyncAttendance(ctx, pool, events, omer); err != nil {
		return err
	}
	// 5b) demo oturumundan ÖNCEKİ oturumlar bitmiş sayılır: panel/izleme ekranları sıfır yüzde göstermesin.
	// Belirlenimli: (öğrenci, sınav) çiftinden türeyen sayıyla ~%92 geldi, ~%4 gelmedi, kalanı işaretsiz.
	// Sıkı eşit starts_at kullanılır: demo oturumunun kendisi (yukarıda orantılı girildi) dışarıda kalır.
	prows, qerr := pool.Query(ctx, `
		SELECT sa.exam_id, sa.student_id
		FROM seat_assignments sa
		JOIN slots sl ON sl.id = sa.slot_id
		WHERE sa.schedule_id = $1 AND sa.exam_id <> $2 AND sl.starts_at < $3
		ORDER BY sa.exam_id, sa.student_id`, sid, exam, startsAt.Add(delta))
	if qerr != nil {
		return qerr
	}
	var past []attendance.Event
	for prows.Next() {
		var ex, st int64
		if err := prows.Scan(&ex, &st); err != nil {
			prows.Close()
			return err
		}
		status := ""
		switch h := (st*31 + ex*17) % 100; {
		case h < 92:
			status = "present"
		case h < 96:
			status = "absent"
		default:
			continue
		}
		id, _ := uuid.NewV7()
		past = append(past, attendance.Event{ID: id.String(), DeviceID: "tablet-demo-gecmis", ExamID: ex, StudentID: st, Status: status, Source: "manual", HLC: clock.Now()})
	}
	prows.Close()
	if err := prows.Err(); err != nil {
		return err
	}
	for len(past) > 0 {
		n := min(len(past), store.MaxSyncBatch)
		if _, err := store.SyncAttendance(ctx, pool, past[:n], omer); err != nil {
			return err
		}
		past = past[n:]
	}
	// bekleyen bir mazeret (mazeret ekranı boş kalmasın)
	if _, err := store.CreateStudentExcuse(ctx, pool, others[len(others)-1], exam, "Hastane raporum var (demo kaydı)"); err != nil {
		return err
	}
	log.Info("demo çizelgesi hazır", slog.String("ders", DemoCourseCode+" "+DemoCourseName), slog.Int("sınav", len(sp.Exams)),
		slog.Int("öğrenci", len(students)), slog.Int64("çizelge", sid), slog.Int("demo_oturum", demoSlot), slog.Time("demo_baslangic", startsAt.Add(delta)))
	return nil
}
