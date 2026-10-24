package store_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/AllenVB/sinavplan/backend/internal/simdata"
	"github.com/AllenVB/sinavplan/backend/internal/store"
	"github.com/AllenVB/sinavplan/backend/internal/store/sqlc"
	"github.com/AllenVB/sinavplan/backend/internal/testdb"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// Bu testler gerçek PostgreSQL ister: şema kısıtları, trigger'lar ve rol yetkileri
// sahte bir veritabanıyla doğrulanamaz. Kurulum: internal/testdb.

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

func sqlState(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

func TestMigrateIsIdempotent(t *testing.T) {
	env := testdb.New(t) // ilk Migrate newTestDB içinde çalıştı
	applied, err := store.Migrate(context.Background(), env.OwnerDSN)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 0 {
		t.Fatalf("ikinci çalıştırma yeni sürüm uygulamamalı: %v", applied)
	}
	var n int
	_ = env.Owner.Pool.QueryRow(context.Background(),
		`SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_type='BASE TABLE'`).Scan(&n)
	if n < 28 {
		t.Fatalf("tablo sayısı %d, en az 28 bekleniyordu", n)
	}
}

func TestAuditLogIsAppendOnlyForEveryone(t *testing.T) {
	env := testdb.New(t)
	ctx := context.Background()
	if _, err := env.Owner.Pool.Exec(ctx,
		`INSERT INTO audit_log(action,entity,entity_id) VALUES ('t','t','1')`); err != nil {
		t.Fatal(err)
	}

	// Sahip rol: trigger durdurur.
	for _, stmt := range []string{`UPDATE audit_log SET action='x'`, `DELETE FROM audit_log`, `TRUNCATE audit_log`} {
		_, err := env.Owner.Pool.Exec(ctx, stmt)
		if err == nil || !strings.Contains(err.Error(), "eklemeli-yalnız") {
			t.Errorf("sahip rol için %q trigger tarafından reddedilmeliydi: %v", stmt, err)
		}
	}

	// Uygulama rolü: yetki hiç yok (42501), ama INSERT çalışır.
	app, err := store.Open(ctx, env.AppDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	if _, err := app.Pool.Exec(ctx, `INSERT INTO audit_log(action,entity,entity_id) VALUES ('t2','t','2')`); err != nil {
		t.Fatalf("uygulama rolü audit_log'a yazabilmeli: %v", err)
	}
	for _, stmt := range []string{`UPDATE audit_log SET action='x'`, `DELETE FROM audit_log`,
		`UPDATE attendance_events SET status='absent'`} {
		if _, err := app.Pool.Exec(ctx, stmt); sqlState(err) != "42501" {
			t.Errorf("uygulama rolü için %q yetki hatası (42501) vermeliydi: %v", stmt, err)
		}
	}
}

func TestSeedIsIdempotentAndRefusesDifferentDataset(t *testing.T) {
	env, ds := testdb.Seeded(t)
	ctx := context.Background()
	conn, _ := pgx.Connect(ctx, env.OwnerDSN)
	defer conn.Close(ctx)

	if err := store.Seed(ctx, conn, ds, "Demo!1234", discard); err != nil {
		t.Fatalf("aynı veri ikinci kez yazılırken hata olmamalı: %v", err)
	}
	var n int
	_ = conn.QueryRow(ctx, `SELECT count(*) FROM students`).Scan(&n)
	if n != len(ds.Students) {
		t.Fatalf("öğrenci sayısı %d, beklenen %d (çift yazım?)", n, len(ds.Students))
	}
	if err := store.Seed(ctx, conn, testdb.SmallDataset(t, 8), "Demo!1234", discard); err == nil {
		t.Fatal("farklı tohumlu veri mevcut veritabanının üstüne yazılmamalı")
	}
}

func TestSeedSetsSequencesSoAppCanInsert(t *testing.T) {
	env, _ := testdb.Seeded(t)
	ctx := context.Background()
	app, _ := store.Open(ctx, env.AppDSN)
	defer app.Close()
	var id int64
	if err := app.Pool.QueryRow(ctx, `INSERT INTO departments(code,name) VALUES ('NEW','Yeni') RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("dizi geride kaldı, çakışma: %v", err)
	}
	if id != 8 { // 6 bölüm + ORT = 7; sıradaki 8
		t.Fatalf("id = %d, beklenen 8", id)
	}
}

func TestSchemaConstraints(t *testing.T) {
	env, _ := testdb.Seeded(t)
	ctx := context.Background()
	exec := func(q string, args ...any) error { _, err := env.Owner.Pool.Exec(ctx, q, args...); return err }

	cases := []struct {
		name, code string
		err        error
	}{
		{"kapasite>0", "23514", exec(`INSERT INTO rooms(code,building,floor,type,seat_rows,seat_cols,capacity) VALUES ('K0','A',1,'classic',2,2,0)`)},
		{"kapasite<=satır*sütun", "23514", exec(`INSERT INTO rooms(code,building,floor,type,seat_rows,seat_cols,capacity) VALUES ('K1','A',1,'classic',2,2,9)`)},
		{"salon tipi", "23514", exec(`INSERT INTO rooms(code,building,floor,type,seat_rows,seat_cols,capacity) VALUES ('K2','A',1,'amfi',2,2,2)`)},
		{"e-posta küçük harf", "23514", exec(`INSERT INTO users(email,password_hash,full_name) VALUES ('BUYUK@X.COM','h','n')`)},
		{"geçersiz rol", "23514", exec(`INSERT INTO user_roles(user_id,role) VALUES (1,'root')`)},
		{"tekrar eden e-posta", "23505", exec(`INSERT INTO users(email,password_hash,full_name) VALUES ($1,'h','n')`, simdata.EmailSecretary)},
		{"öğrenci mazereti şekli", "23514", exec(`INSERT INTO excuses(kind,user_id,slot_id,reason) VALUES ('student',1,1,'x')`)},
		{"QR kullanım tutarlılığı", "23514", exec(`INSERT INTO qr_tokens(exam_id,student_id,token_hash,expires_at,used_at) VALUES (1,1,'\x01',now(),now())`)},
	}
	for _, c := range cases {
		if got := sqlState(c.err); got != c.code {
			t.Errorf("%s: SQLSTATE %q, beklenen %q (%v)", c.name, got, c.code, c.err)
		}
	}
}

// Aynı dönemde yalnızca bir yayınlanmış çizelge olabilir; slot ve koltuk
// tutarlılığı bileşik FK'larla korunur.
func TestScheduleIntegrity(t *testing.T) {
	env, _ := testdb.Seeded(t)
	ctx := context.Background()
	exec := func(q string, args ...any) error { _, err := env.Owner.Pool.Exec(ctx, q, args...); return err }

	if err := exec(`INSERT INTO schedules(period_id,version,status,published_at) VALUES (1,1,'published',now())`); err != nil {
		t.Fatal(err)
	}
	if err := exec(`INSERT INTO schedules(period_id,version,status,published_at) VALUES (1,2,'published',now())`); sqlState(err) != "23505" {
		t.Errorf("ikinci yayınlanmış çizelge reddedilmeliydi: %v", err)
	}
	if err := exec(`INSERT INTO schedules(period_id,version,status) VALUES (1,3,'published')`); sqlState(err) != "23514" {
		t.Errorf("published_at olmadan yayınlanmış çizelge reddedilmeliydi: %v", err)
	}

	// Taslak çizelge: sınav 1 slot 1'de, sınav 2 slot 2'de.
	if err := exec(`INSERT INTO schedules(period_id,version) VALUES (1,4)`); err != nil {
		t.Fatal(err)
	}
	var sid int64
	_ = env.Owner.Pool.QueryRow(ctx, `SELECT id FROM schedules WHERE version=4`).Scan(&sid)
	if err := exec(`INSERT INTO schedule_exams VALUES ($1,1,1),($1,2,2)`, sid); err != nil {
		t.Fatal(err)
	}

	seat := func(student, exam, slot, room, seatID int64) error {
		return exec(`INSERT INTO seat_assignments(schedule_id,exam_id,slot_id,student_id,room_id,seat_id) VALUES ($1,$2,$3,$4,$5,$6)`,
			sid, exam, slot, student, room, seatID)
	}
	if err := seat(1, 1, 1, 1, 1); err != nil {
		t.Fatal(err)
	}
	if err := seat(2, 1, 1, 1, 1); sqlState(err) != "23505" {
		t.Errorf("aynı slotta aynı koltuk iki öğrenciye verilememeli: %v", err)
	}
	// Aynı koltuk FARKLI slotta kullanılabilir (görev tanımındaki örnek kısıt bunu yanlışlıkla yasaklardı).
	if err := seat(3, 2, 2, 1, 1); err != nil {
		t.Errorf("aynı koltuk farklı slotta serbest olmalı: %v", err)
	}
	if err := seat(1, 1, 1, 1, 2); sqlState(err) != "23505" {
		t.Errorf("öğrenciye aynı sınav için ikinci koltuk verilememeli: %v", err)
	}
	if err := seat(4, 1, 2, 1, 3); sqlState(err) != "23503" {
		t.Errorf("sınavın slotuyla çelişen slot_id reddedilmeliydi: %v", err)
	}
	// Koltuk başka salona ait: seat_id 1 salon 1'de, salon 2'de gösterilemez.
	if err := seat(5, 1, 1, 2, 1); sqlState(err) != "23503" {
		t.Errorf("başka salonun koltuğu reddedilmeliydi: %v", err)
	}
}

func TestSqlcQueriesWorkAsAppRole(t *testing.T) {
	env, ds := testdb.Seeded(t)
	ctx := context.Background()
	app, err := store.Open(ctx, env.AppDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	q := sqlc.New(app.Pool)

	u, err := q.GetUserByEmail(ctx, "SEKRETER@sinavplan.test") // büyük/küçük harf duyarsız
	if err != nil || u.Email != simdata.EmailSecretary {
		t.Fatalf("GetUserByEmail: %v %+v", err, u)
	}
	roles, _ := q.ListUserRoles(ctx, u.ID)
	if len(roles) != 1 || roles[0] != "secretary" {
		t.Fatalf("roller: %v", roles)
	}

	// Sınav 1'e girenlerin sayısı, üretilen kayıtlardan bağımsız hesapla.
	secCourse := map[int64]int64{}
	for _, s := range ds.Sections {
		secCourse[s.ID] = s.CourseID
	}
	want := 0
	for _, e := range ds.Enrollments {
		if secCourse[e.SectionID] == ds.Exams[0].CourseID {
			want++
		}
	}
	got, err := q.ListExamStudents(ctx, ds.Exams[0].ID)
	if err != nil || len(got) != want || want == 0 {
		t.Fatalf("ListExamStudents: %d öğrenci, beklenen %d (%v)", len(got), want, err)
	}

	// Yayınlanmış çizelge yokken öğrenci programı boş döner, hata değil.
	sch, err := q.ListStudentExamSchedule(ctx, 1)
	if err != nil || len(sch) != 0 {
		t.Fatalf("ListStudentExamSchedule: %v %d satır", err, len(sch))
	}

	// QR: tek kullanım (yarış testi Faz 7'de; burada ardışık davranış).
	if _, err := env.Owner.Pool.Exec(ctx, `INSERT INTO qr_tokens(exam_id,student_id,token_hash,expires_at) VALUES (1,1,'\xdeadbeef', now()+interval '1 hour')`); err != nil {
		t.Fatal(err)
	}
	use := func() error {
		_, err := q.UseQRToken(ctx, sqlc.UseQRTokenParams{TokenHash: []byte{0xde, 0xad, 0xbe, 0xef}, UsedByDevice: pgtype.Text{String: "tablet-1", Valid: true}})
		return err
	}
	if err := use(); err != nil {
		t.Fatalf("ilk kullanım başarılı olmalı: %v", err)
	}
	if err := use(); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("ikinci kullanım reddedilmeli (satır yok): %v", err)
	}
}
