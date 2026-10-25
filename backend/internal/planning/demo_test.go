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
	code := planning.DemoCourseCode
	var period, demoExam int64
	var exams, total int
	one(&period, `SELECT id FROM exam_periods ORDER BY id LIMIT 1`)
	one(&total, `SELECT count(*) FROM exams WHERE period_id = $1`, period)
	one(&exams, `SELECT count(*) FROM exams e JOIN courses c ON c.id = e.course_id WHERE e.period_id = $1 AND c.code = $2`, period, code)
	one(&demoExam, `SELECT e.id FROM exams e JOIN courses c ON c.id = e.course_id WHERE e.period_id = $1 AND c.code = $2`, period, code)
	if exams != 1 || total < 20 {
		t.Fatalf("ana dönemde demo dersi 1 kez ve diğer dersler de olmalı: demo=%d toplam=%d", exams, total)
	}
	var periods int
	one(&periods, `SELECT count(*) FROM exam_periods`)
	if periods != 1 {
		t.Fatalf("ayrı demo dönemi açılmamalı, tek dönem olmalı: %d", periods)
	}

	// ders: Prompt Mühendisliği, Yazılım Mühendisliği 4. sınıf, öğretim üyesi Fahrettin Burak DEMİR
	var name, dept, lecturer string
	var year int
	one(&name, `SELECT c.name FROM courses c WHERE c.code = $1`, code)
	one(&year, `SELECT c.year FROM courses c WHERE c.code = $1`, code)
	one(&dept, `SELECT d.name FROM courses c JOIN departments d ON d.id = c.department_id WHERE c.code = $1`, code)
	one(&lecturer, `SELECT u.full_name FROM courses c JOIN sections s ON s.course_id = c.id JOIN lecturers l ON l.id = s.lecturer_id JOIN users u ON u.id = l.user_id WHERE c.code = $1`, code)
	if name != "Prompt Mühendisliği" || year != 4 || dept != "Yazılım Mühendisliği" || lecturer != "Fahrettin Burak DEMİR" {
		t.Fatalf("ders: %q %d. sınıf %q hoca %q", name, year, dept, lecturer)
	}

	// öğrenciler: yalnızca Yazılım Mühendisliği
	var enrolled, nonYZM int
	one(&enrolled, `SELECT count(*) FROM courses c JOIN sections s ON s.course_id = c.id JOIN enrollments en ON en.section_id = s.id WHERE c.code = $1`, code)
	one(&nonYZM, `SELECT count(*) FROM courses c JOIN sections s ON s.course_id = c.id JOIN enrollments en ON en.section_id = s.id
		JOIN students st ON st.id = en.student_id JOIN departments d ON d.id = st.department_id WHERE c.code = $1 AND d.code <> 'YZM'`, code)
	if enrolled < 6 || nonYZM != 0 {
		t.Fatalf("kayıtlı %d öğrenci, YZM dışı %d (0 olmalı)", enrolled, nonYZM)
	}

	// TÜM sınavlar çizelgelendi ve yayınlandı; demo sınavın salonunda gözetmen Ömer ASLAN
	var sched int64
	var planned int
	one(&sched, `SELECT id FROM schedules WHERE period_id = $1 AND status = 'published'`, period)
	one(&planned, `SELECT count(*) FROM schedule_exams WHERE schedule_id = $1`, sched)
	if planned != total {
		t.Fatalf("çizelgede %d sınav var, dönemde %d", planned, total)
	}
	var inv string
	one(&inv, `SELECT u.full_name FROM invigilator_assignments ia JOIN room_assignments ra ON ra.schedule_id = ia.schedule_id AND ra.exam_id = $2 AND ra.room_id = ia.room_id AND ra.slot_id = ia.slot_id
		JOIN users u ON u.id = ia.user_id WHERE ia.schedule_id = $1`, sched, demoExam)
	if inv != "Ömer ASLAN" {
		t.Fatalf("demo salonunun gözetmeni %q", inv)
	}
	var seated int
	one(&seated, `SELECT count(*) FROM seat_assignments WHERE schedule_id = $1 AND exam_id = $2`, sched, demoExam)
	if seated != enrolled {
		t.Fatalf("oturan %d ≠ kayıtlı %d", seated, enrolled)
	}

	// demo oturumu şimdi sürüyor; yoklama kısmen girilmiş; bekleyen mazeret var
	var active bool
	one(&active, `SELECT sl.starts_at <= now() AND now() < sl.ends_at FROM schedule_exams se JOIN slots sl ON sl.id = se.slot_id WHERE se.schedule_id = $1 AND se.exam_id = $2`, sched, demoExam)
	if !active {
		t.Fatal("demo oturumu şu an sürüyor olmalı")
	}
	var present, absent, pending int
	one(&present, `SELECT count(*) FROM attendance_state WHERE exam_id = $1 AND status = 'present'`, demoExam)
	one(&absent, `SELECT count(*) FROM attendance_state WHERE exam_id = $1 AND status = 'absent'`, demoExam)
	one(&pending, `SELECT count(*) FROM excuses WHERE status = 'pending'`)
	if present != enrolled*2/3 || absent < 1 || present+absent >= enrolled || pending != 1 {
		t.Fatalf("yoklama: %d geldi, %d gelmedi (kayıtlı %d), bekleyen mazeret %d", present, absent, enrolled, pending)
	}

	// demo oturumundan ÖNCEKİ oturumların yoklaması dolu (panel sıfır yüzde göstermesin); sonrakilerde yok
	var pastExams, pastPresent, pastAll, futureMarks int
	one(&pastExams, `SELECT count(DISTINCT se.exam_id) FROM schedule_exams se JOIN slots sl ON sl.id = se.slot_id
		JOIN slots d ON d.id = (SELECT slot_id FROM schedule_exams WHERE schedule_id = $1 AND exam_id = $2)
		WHERE se.schedule_id = $1 AND sl.starts_at < d.starts_at`, sched, demoExam)
	one(&pastAll, `SELECT count(*) FROM attendance_state a JOIN schedule_exams se ON se.exam_id = a.exam_id AND se.schedule_id = $1
		JOIN slots sl ON sl.id = se.slot_id JOIN slots d ON d.id = (SELECT slot_id FROM schedule_exams WHERE schedule_id = $1 AND exam_id = $2)
		WHERE a.exam_id <> $2 AND sl.starts_at < d.starts_at`, sched, demoExam)
	one(&pastPresent, `SELECT count(*) FROM attendance_state a JOIN schedule_exams se ON se.exam_id = a.exam_id AND se.schedule_id = $1
		JOIN slots sl ON sl.id = se.slot_id JOIN slots d ON d.id = (SELECT slot_id FROM schedule_exams WHERE schedule_id = $1 AND exam_id = $2)
		WHERE a.exam_id <> $2 AND sl.starts_at < d.starts_at AND a.status = 'present'`, sched, demoExam)
	one(&futureMarks, `SELECT count(*) FROM attendance_state a JOIN schedule_exams se ON se.exam_id = a.exam_id AND se.schedule_id = $1
		JOIN slots sl ON sl.id = se.slot_id JOIN slots d ON d.id = (SELECT slot_id FROM schedule_exams WHERE schedule_id = $1 AND exam_id = $2)
		WHERE sl.starts_at >= d.starts_at AND a.exam_id <> $2`, sched, demoExam)
	if pastExams > 0 && (pastAll == 0 || pastPresent*100/pastAll < 85) {
		t.Fatalf("geçmiş oturumlar: %d sınav, %d kayıt, %d geldi (≥%%85 beklenir)", pastExams, pastAll, pastPresent)
	}
	if futureMarks != 0 {
		t.Fatalf("sonraki oturumlarda yoklama olmamalı: %d", futureMarks)
	}
	t.Logf("geçmiş oturum: %d sınav, %d yoklama kaydı, %d geldi", pastExams, pastAll, pastPresent)

	// ikinci çağrı idempotent
	if err := planning.SeedDemoExam(ctx, pool, now.Add(time.Hour), log); err != nil {
		t.Fatal(err)
	}
	var schedules int
	one(&schedules, `SELECT count(*) FROM schedules`)
	if schedules != 1 {
		t.Fatalf("ikinci çağrı yeni çizelge açmamalı: %d", schedules)
	}
}
