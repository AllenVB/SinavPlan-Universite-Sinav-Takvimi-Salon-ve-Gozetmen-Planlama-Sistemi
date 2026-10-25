package planning_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/AllenVB/sinavplan/backend/internal/planning"
	"github.com/AllenVB/sinavplan/backend/internal/testdb"
)

func TestSeedDemoExamBuildsTheDescribedScenario(t *testing.T) {
	env, _ := testdb.Seeded(t)
	ctx := context.Background()
	pool := env.Owner.Pool
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	now := time.Now()
	if err := planning.SeedDemoExam(ctx, pool, now, log); err != nil {
		t.Fatal(err)
	}
	one := func(dst any, q string, args ...any) {
		t.Helper()
		if err := pool.QueryRow(ctx, q, args...).Scan(dst); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	var period int64
	var exams int
	one(&period, `SELECT id FROM exam_periods WHERE name = $1`, planning.DemoPeriodName)
	one(&exams, `SELECT count(*) FROM exams WHERE period_id = $1`, period)
	if exams != 1 {
		t.Fatalf("demo döneminde tek sınav olmalı: %d", exams)
	}

	// ders: Prompt Mühendisliği, Yazılım Mühendisliği 4. sınıf, öğretim üyesi Fahrettin Burak DEMİR
	var code, name, dept, lecturer string
	var year int
	one(&code, `SELECT c.code FROM exams e JOIN courses c ON c.id = e.course_id WHERE e.period_id = $1`, period)
	one(&name, `SELECT c.name FROM exams e JOIN courses c ON c.id = e.course_id WHERE e.period_id = $1`, period)
	one(&year, `SELECT c.year FROM exams e JOIN courses c ON c.id = e.course_id WHERE e.period_id = $1`, period)
	one(&dept, `SELECT d.name FROM exams e JOIN courses c ON c.id = e.course_id JOIN departments d ON d.id = c.department_id WHERE e.period_id = $1`, period)
	one(&lecturer, `SELECT u.full_name FROM exams e JOIN sections s ON s.course_id = e.course_id JOIN lecturers l ON l.id = s.lecturer_id JOIN users u ON u.id = l.user_id WHERE e.period_id = $1`, period)
	if code != planning.DemoCourseCode || name != "Prompt Mühendisliği" || year != 4 || dept != "Yazılım Mühendisliği" || lecturer != "Fahrettin Burak DEMİR" {
		t.Fatalf("ders: %s %q %d. sınıf %q hoca %q", code, name, year, dept, lecturer)
	}

	// öğrenciler: yalnızca Yazılım Mühendisliği
	var enrolled, nonYZM int
	one(&enrolled, `SELECT count(*) FROM exams e JOIN sections s ON s.course_id = e.course_id JOIN enrollments en ON en.section_id = s.id WHERE e.period_id = $1`, period)
	one(&nonYZM, `SELECT count(*) FROM exams e JOIN sections s ON s.course_id = e.course_id JOIN enrollments en ON en.section_id = s.id
		JOIN students st ON st.id = en.student_id JOIN departments d ON d.id = st.department_id WHERE e.period_id = $1 AND d.code <> 'YZM'`, period)
	if enrolled < 6 || nonYZM != 0 {
		t.Fatalf("kayıtlı %d öğrenci, YZM dışı %d (0 olmalı)", enrolled, nonYZM)
	}

	// çizelge yayınlandı; tek salon; tek gözetmen = Ömer ASLAN
	var sched int64
	var rooms, duties int
	var inv string
	one(&sched, `SELECT id FROM schedules WHERE period_id = $1 AND status = 'published'`, period)
	one(&rooms, `SELECT count(*) FROM room_assignments WHERE schedule_id = $1`, sched)
	one(&duties, `SELECT count(*) FROM invigilator_assignments WHERE schedule_id = $1`, sched)
	one(&inv, `SELECT u.full_name FROM invigilator_assignments ia JOIN users u ON u.id = ia.user_id WHERE ia.schedule_id = $1`, sched)
	if rooms != 1 || duties != 1 || inv != "Ömer ASLAN" {
		t.Fatalf("%d salon, %d görev, gözetmen %q", rooms, duties, inv)
	}
	var seated int
	one(&seated, `SELECT count(*) FROM seat_assignments WHERE schedule_id = $1`, sched)
	if seated != enrolled {
		t.Fatalf("oturan %d ≠ kayıtlı %d", seated, enrolled)
	}

	// oturum şimdi sürüyor; yoklama kısmen girilmiş; bekleyen mazeret var
	var active bool
	one(&active, `SELECT sl.starts_at <= now() AND now() < sl.ends_at FROM slots sl WHERE sl.period_id = $1`, period)
	if !active {
		t.Fatal("demo oturumu şu an sürüyor olmalı")
	}
	var present, absent, pending int
	one(&present, `SELECT count(*) FROM attendance_state WHERE status = 'present'`)
	one(&absent, `SELECT count(*) FROM attendance_state WHERE status = 'absent'`)
	one(&pending, `SELECT count(*) FROM excuses WHERE status = 'pending'`)
	if present != enrolled*2/3 || absent < 1 || present+absent >= enrolled || pending != 1 {
		t.Fatalf("yoklama: %d geldi, %d gelmedi (kayıtlı %d), bekleyen mazeret %d", present, absent, enrolled, pending)
	}

	// tam dönemin çizelgesi YOK (demo onu etkilemez) ve ikinci çağrı idempotent
	var other int
	one(&other, `SELECT count(*) FROM schedules WHERE period_id <> $1`, period)
	if other != 0 {
		t.Fatalf("demo diğer dönemlere çizelge eklememeli: %d", other)
	}
	if err := planning.SeedDemoExam(ctx, pool, now.Add(time.Hour), log); err != nil {
		t.Fatal(err)
	}
	var periods int
	one(&periods, `SELECT count(*) FROM exam_periods WHERE name = $1`, planning.DemoPeriodName)
	if periods != 1 {
		t.Fatalf("ikinci çağrı yeni dönem açmamalı: %d", periods)
	}
}
