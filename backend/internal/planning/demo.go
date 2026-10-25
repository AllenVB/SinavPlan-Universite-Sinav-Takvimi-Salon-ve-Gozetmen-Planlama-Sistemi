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

// Demo sınavı: sistem boş görünmesin diye ilk kurulumda hazır gelen küçük, anlaşılır bir örnek.
const (
	DemoPeriodName = "Demo: Prompt Mühendisliği Sınavı"
	DemoCourseCode = "YZM406"
	DemoCourseName = "Prompt Mühendisliği"
	demoStudents   = 36 // tek salona ve tek gözetmene sığar (≤ 50 öğrenci)
)

// SeedDemoExam, ayrı bir "demo dönemi" kurar: Yazılım Mühendisliği 4. sınıf seçmeli dersi "Prompt
// Mühendisliği" (Fahrettin Burak DEMİR), yalnızca Yazılım Mühendisliği 4. sınıf öğrencilerinden 36 kişi,
// tek salon, gözetmen Ömer ASLAN. Sınav oturumu ŞİMDİ başlamış kabul edilir (başlangıç = now−20 dk), çizelge
// üretilip yayınlanır, yoklamanın bir kısmı girilmiş ve bir mazeret bekliyor — böylece Canlı izleme, takvim, tablet,
// öğrenci ve mazeret ekranları ilk açılışta doludur. Tam dönem (300 sınav) etkilenmez. İdempotenttir.
func SeedDemoExam(ctx context.Context, pool *pgxpool.Pool, now time.Time, log *slog.Logger) error {
	var exists bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM exam_periods WHERE name = $1)`, DemoPeriodName).Scan(&exists); err != nil {
		return err
	}
	if exists {
		log.Info("demo sınavı zaten var, atlanıyor")
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

	istanbul, err := time.LoadLocation("Europe/Istanbul")
	if err != nil {
		istanbul = time.FixedZone("UTC+3", 3*3600)
	}
	start := now.Add(-20 * time.Minute).Truncate(time.Minute).UTC()
	var period, exam int64
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var course, section int64
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
	if err := tx.QueryRow(ctx, `INSERT INTO exam_periods (name, kind, start_date, days, sessions_per_day) VALUES ($1,'final',$2,1,1) RETURNING id`,
		DemoPeriodName, now.In(istanbul).Format("2006-01-02")).Scan(&period); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO slots (period_id, day_index, session_index, starts_at, ends_at) VALUES ($1,0,0,$2,$3)`,
		period, start, start.Add(120*time.Minute)); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `INSERT INTO exams (period_id, course_id, duration_minutes, requires_lab) VALUES ($1,$2,90,false) RETURNING id`, period, course).Scan(&exam); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}

	// çizelge: tek sınav → tek oturum, tek salon; gözetmen Ömer ASLAN
	ds, err := store.LoadDataset(ctx, pool, period, 1)
	if err != nil {
		return err
	}
	sp, _, err := BuildFrom(ds, 1, 2000, nil)
	if err != nil {
		return fmt.Errorf("demo çizelgesi: %w", err)
	}
	if len(sp.Rooms) != 1 || len(sp.Invigilators) != 1 {
		return fmt.Errorf("demo sınavı tek salonda ve tek gözetmenle kurulamadı (%d salon, %d gözetmen)", len(sp.Rooms), len(sp.Invigilators))
	}
	sp.Invigilators[0].UserID = omer
	sp.CreatedBy = secretary
	sid, _, err := store.CreateSchedule(ctx, pool, sp)
	if err != nil {
		return err
	}
	if err := store.PublishSchedule(ctx, pool, sid, secretary); err != nil {
		return err
	}

	// yoklama bir kısmı girilmiş: 36 öğrencide 24 geldi, 3 gelmedi, kalanı işaretsiz (orantılı)
	nPresent, nAbsent := len(students)*2/3, max(1, len(students)/12)
	clock := attendance.Clock{Physical: func() int64 { return now.UnixMilli() }}
	var events []attendance.Event
	for i, st := range students {
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
	// bekleyen bir mazeret (öğrenci işleri ekranı boş kalmasın)
	if _, err := store.CreateStudentExcuse(ctx, pool, students[len(students)-1], exam, "Hastane raporum var (demo kaydı)"); err != nil {
		return err
	}
	log.Info("demo sınavı hazır", slog.String("ders", DemoCourseCode+" "+DemoCourseName), slog.Int("öğrenci", len(students)), slog.Int64("çizelge", sid))
	return nil
}
