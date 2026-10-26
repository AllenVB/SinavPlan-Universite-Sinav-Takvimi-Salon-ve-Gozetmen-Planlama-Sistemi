package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Kanıtlar (Faz 22 A2): kayıtlı bir çizelgenin T-1..T-5 kısıtlarını çözücü kodundan BAĞIMSIZ, saf SQL ile
// yeniden denetler. Her denetim iki kez koşar: gerçek plan (ihlal 0 beklenir) ve bilerek bozulmuş SANAL plan
// (CTE parametresiyle; veritabanına hiçbir şey yazılmaz) — ikincisinde ihlal ≥ 1 beklenir. Böylece
// "doğrulayıcı gerçekten bir şey yakalıyor mu?" sorusu canlı yanıtlanır.

// Proof, tek bir denetimin sonucu.
type Proof struct {
	ID       string `json:"id"`   // T-1a …
	Task     string `json:"task"` // T-1 …
	Title    string `json:"title"`
	Found    int64  `json:"found"` // gerçek plandaki ihlal
	Mutation string `json:"mutation"`
	Caught   int64  `json:"caught"` // bozulmuş sanal plandaki ihlal
	OK       bool   `json:"ok"`     // Found == 0 && Caught > 0
	Detail   string `json:"detail,omitempty"`
	Ms       int64  `json:"ms"`
}

// Proofs, yanıtın tamamı.
type Proofs struct {
	ScheduleID int64     `json:"schedule_id"`
	Version    int       `json:"version"`
	Exams      int64     `json:"exams"`
	Seats      int64     `json:"seats"`
	Duties     int64     `json:"duties"`
	Items      []Proof   `json:"items"`
	RanAt      time.Time `json:"ran_at"`
	TotalMs    int64     `json:"total_ms"`
}

// Ortak CTE'ler. $1 = çizelge. Bozma parametreleri her sorguda ayrı numaralanır.
const (
	// se: sınav → slot; $2 sınavı (0 = yok) $3 slotuna taşınmış sayılır.
	cteSE = `se AS (SELECT exam_id, CASE WHEN exam_id = $2 THEN $3::bigint ELSE slot_id END AS slot_id FROM schedule_exams WHERE schedule_id = $1),
	es AS (SELECT DISTINCT e.id AS exam_id, en.student_id FROM exams e JOIN sections s ON s.course_id = e.course_id
	       JOIN enrollments en ON en.section_id = s.id WHERE e.id IN (SELECT exam_id FROM schedule_exams WHERE schedule_id = $1))`
	// sa: oturma satırları koordinatlı; $2 satırı (0 = yok) ($3, $4) koordinatına oturmuş sayılır.
	cteSA = `sa AS (SELECT a.id, a.exam_id, a.slot_id, a.room_id, a.student_id,
	       CASE WHEN a.id = $2 THEN $3::smallint ELSE rs.row_no END AS row_no,
	       CASE WHEN a.id = $2 THEN $4::smallint ELSE rs.col_no END AS col_no
	       FROM seat_assignments a JOIN room_seats rs ON rs.id = a.seat_id WHERE a.schedule_id = $1)`
	// ia: gözetmen görevleri; $2 kullanıcısı (0 = yok) $3 slotunda ek görev almış sayılır.
	cteIA = `ia AS (SELECT user_id, slot_id, room_id, exam_id FROM invigilator_assignments WHERE schedule_id = $1
	       UNION ALL SELECT $2::bigint, $3::bigint, 0, 0 WHERE $2::bigint <> 0)`
)

type proofSpec struct {
	id, task, title, mutation string
	sql                       string
	// mutate, bozma parametrelerini bulur (ör. ortak öğrencili iki sınav); ok=false ise bozma uygulanamaz.
	mutate func(ctx context.Context, tx pgx.Tx, sid int64) ([]any, bool, error)
	args   int // bozma parametre sayısı (gerçek koşuda sıfırlarla doldurulur)
}

var proofSpecs = []proofSpec{
	{id: "T-1a", task: "T-1", title: "Ortak öğrencili iki sınav aynı oturumda değil",
		mutation: "ortak öğrencisi olan bir sınav diğerinin oturumuna taşındı", args: 2,
		sql: `WITH ` + cteSE + ` SELECT count(*) FROM (SELECT es.student_id, se.slot_id FROM es JOIN se USING (exam_id) GROUP BY 1, 2 HAVING count(*) > 1) x`,
		mutate: func(ctx context.Context, tx pgx.Tx, sid int64) ([]any, bool, error) {
			var x, slot int64
			// önce iki sınavı olan TEK bir öğrenci seç (tüm öğrenci×sınav çiftlerini birleştirmek ~10 sn sürüyordu)
			err := tx.QueryRow(ctx, `WITH `+cteSE+`, p AS (SELECT student_id FROM es GROUP BY 1 HAVING count(*) >= 2 ORDER BY 1 LIMIT 1)
				SELECT a.exam_id, sb.slot_id FROM p JOIN es a USING (student_id) JOIN es b USING (student_id)
				JOIN se sa ON sa.exam_id = a.exam_id JOIN se sb ON sb.exam_id = b.exam_id WHERE a.exam_id <> b.exam_id AND sa.slot_id <> sb.slot_id ORDER BY 1, 2 LIMIT 1`, sid, 0, 0).Scan(&x, &slot)
			return []any{x, slot}, err == nil, ignoreNoRows(err)
		}},
	{id: "T-1b", task: "T-1", title: "Hiçbir öğrenci bir günde 2'den fazla sınava girmiyor",
		mutation: "aynı gün iki sınavı olan bir öğrencinin üçüncü sınavı o güne taşındı", args: 2,
		sql: `WITH ` + cteSE + ` SELECT count(*) FROM (SELECT es.student_id, sl.day_index FROM es JOIN se USING (exam_id) JOIN slots sl ON sl.id = se.slot_id
			GROUP BY 1, 2 HAVING count(*) > 2) x`,
		mutate: func(ctx context.Context, tx pgx.Tx, sid int64) ([]any, bool, error) {
			// aynı gün 2 sınavı olan öğrencinin o gün DIŞINDAKİ bir sınavını, o günün sınavı olmayan oturumuna taşı
			var x, slot int64
			err := tx.QueryRow(ctx, `WITH `+cteSE+`, d AS (SELECT es.student_id, sl.day_index FROM es JOIN se USING (exam_id) JOIN slots sl ON sl.id = se.slot_id GROUP BY 1, 2 HAVING count(*) = 2 ORDER BY 1, 2 LIMIT 1)
				SELECT es.exam_id, free.id FROM d JOIN es ON es.student_id = d.student_id JOIN se USING (exam_id) JOIN slots sl ON sl.id = se.slot_id
				JOIN slots free ON free.period_id = sl.period_id AND free.day_index = d.day_index
				WHERE sl.day_index <> d.day_index AND free.id NOT IN (SELECT se2.slot_id FROM es es2 JOIN se se2 USING (exam_id) WHERE es2.student_id = d.student_id)
				ORDER BY 1, 2 LIMIT 1`, sid, 0, 0).Scan(&x, &slot)
			return []any{x, slot}, err == nil, ignoreNoRows(err)
		}},
	{id: "T-2a", task: "T-2", title: "Hiçbir salon, oturumda aktif koltuk sayısından fazla öğrenci almıyor",
		mutation: "en dolu salona koltuğundan bir fazla öğrenci yazıldı", args: 2,
		sql: `WITH a AS (SELECT slot_id, room_id, count(*) + CASE WHEN slot_id = $2 AND room_id = $3 THEN (SELECT count(*) FILTER (WHERE is_active) FROM room_seats WHERE room_id = $3) - count(*) + 1 ELSE 0 END AS n
			FROM seat_assignments WHERE schedule_id = $1 GROUP BY 1, 2)
			SELECT count(*) FROM a JOIN (SELECT room_id, count(*) FILTER (WHERE is_active) AS cap FROM room_seats GROUP BY 1) r USING (room_id) WHERE a.n > r.cap`,
		mutate: func(ctx context.Context, tx pgx.Tx, sid int64) ([]any, bool, error) {
			var slot, room int64
			err := tx.QueryRow(ctx, `SELECT slot_id, room_id FROM seat_assignments WHERE schedule_id = $1 GROUP BY 1, 2 ORDER BY count(*) DESC, 1, 2 LIMIT 1`, sid).Scan(&slot, &room)
			return []any{slot, room}, err == nil, ignoreNoRows(err)
		}},
	{id: "T-2b", task: "T-2", title: "Laboratuvar sınavları yalnız laboratuvar salonlarında",
		mutation: "bir laboratuvar sınavının payı derslik salona yazıldı", args: 2,
		sql: `SELECT count(*) FROM (SELECT ra.exam_id, CASE WHEN ra.id = $2 THEN $3::bigint ELSE ra.room_id END AS room_id FROM room_assignments ra WHERE ra.schedule_id = $1) x
			JOIN exams e ON e.id = x.exam_id JOIN rooms r ON r.id = x.room_id WHERE e.requires_lab AND r.type <> 'lab'`,
		mutate: func(ctx context.Context, tx pgx.Tx, sid int64) ([]any, bool, error) {
			var ra, room int64
			err := tx.QueryRow(ctx, `SELECT ra.id, (SELECT id FROM rooms WHERE type = 'classic' ORDER BY id LIMIT 1) FROM room_assignments ra JOIN exams e ON e.id = ra.exam_id
				WHERE ra.schedule_id = $1 AND e.requires_lab ORDER BY ra.id LIMIT 1`, sid).Scan(&ra, &room)
			return []any{ra, room}, err == nil, ignoreNoRows(err)
		}},
	{id: "T-3a", task: "T-3", title: "Aynı sınavın öğrencileri yan yana oturmuyor",
		mutation: "bir öğrenci aynı sınavdan bir öğrencinin hemen yanına oturtuldu", args: 3,
		sql: `WITH ` + cteSA + ` SELECT count(*) FROM sa a JOIN sa b ON b.slot_id = a.slot_id AND b.room_id = a.room_id AND b.exam_id = a.exam_id
			AND b.row_no = a.row_no AND b.col_no = a.col_no + 1 AND b.id <> a.id`,
		mutate: func(ctx context.Context, tx pgx.Tx, sid int64) ([]any, bool, error) {
			var id int64
			var row, col int16
			err := tx.QueryRow(ctx, `WITH `+cteSA+` SELECT b.id, a.row_no, a.col_no + 1 FROM sa a JOIN sa b ON b.slot_id = a.slot_id AND b.room_id = a.room_id AND b.exam_id = a.exam_id AND b.id <> a.id
				ORDER BY a.id, b.id LIMIT 1`, sid, 0, 0, 0).Scan(&id, &row, &col)
			return []any{id, row, col}, err == nil, ignoreNoRows(err)
		}},
	{id: "T-3b", task: "T-3", title: "Her koltukta bir öğrenci; her öğrencinin sınav başına bir koltuğu var",
		mutation: "bir öğrenci dolu bir koltuğa oturtuldu", args: 3,
		sql: `WITH ` + cteSA + ` SELECT (SELECT count(*) FROM (SELECT slot_id, room_id, row_no, col_no FROM sa GROUP BY 1, 2, 3, 4 HAVING count(*) > 1) d)
			+ (SELECT count(*) FROM (SELECT exam_id, student_id FROM sa GROUP BY 1, 2 HAVING count(*) > 1) e)`,
		mutate: func(ctx context.Context, tx pgx.Tx, sid int64) ([]any, bool, error) {
			var id int64
			var row, col int16
			err := tx.QueryRow(ctx, `WITH `+cteSA+` SELECT b.id, a.row_no, a.col_no FROM sa a JOIN sa b ON b.slot_id = a.slot_id AND b.room_id = a.room_id AND b.id <> a.id ORDER BY a.id, b.id LIMIT 1`, sid, 0, 0, 0).Scan(&id, &row, &col)
			return []any{id, row, col}, err == nil, ignoreNoRows(err)
		}},
	{id: "T-3c", task: "T-3", title: "Engelli öğrenciler erişilebilir koltukta",
		mutation: "engelli bir öğrenci erişilebilir olmayan koltuğa taşındı", args: 3,
		sql: `WITH ` + cteSA + ` SELECT count(*) FROM sa JOIN students st ON st.id = sa.student_id
			LEFT JOIN room_seats rs ON rs.room_id = sa.room_id AND rs.row_no = sa.row_no AND rs.col_no = sa.col_no
			WHERE st.disability_flag AND NOT coalesce(rs.is_accessible, false)`,
		mutate: func(ctx context.Context, tx pgx.Tx, sid int64) ([]any, bool, error) {
			var id int64
			var row, col int16
			err := tx.QueryRow(ctx, `SELECT a.id, rs2.row_no, rs2.col_no FROM seat_assignments a JOIN students st ON st.id = a.student_id
				JOIN room_seats rs2 ON rs2.room_id = a.room_id AND NOT rs2.is_accessible AND rs2.is_active
				WHERE a.schedule_id = $1 AND st.disability_flag ORDER BY a.id, rs2.id LIMIT 1`, sid).Scan(&id, &row, &col)
			return []any{id, row, col}, err == nil, ignoreNoRows(err)
		}},
	{id: "T-4a", task: "T-4", title: "Gözetmenin aynı gün art arda iki oturumu yok, bir oturumda tek görevi var",
		mutation: "bir gözetmene aynı gün bir sonraki oturumda ek görev verildi", args: 2,
		sql: `WITH ` + cteIA + `, u AS (SELECT DISTINCT ia.user_id, sl.period_id, sl.day_index, sl.session_index, ia.slot_id FROM ia JOIN slots sl ON sl.id = ia.slot_id)
			SELECT (SELECT count(*) FROM u a JOIN u b ON b.user_id = a.user_id AND b.period_id = a.period_id AND b.day_index = a.day_index AND b.session_index = a.session_index + 1)
			     + (SELECT count(*) FROM (SELECT user_id, slot_id FROM ia GROUP BY 1, 2 HAVING count(DISTINCT room_id) > 1) d)`,
		mutate: func(ctx context.Context, tx pgx.Tx, sid int64) ([]any, bool, error) {
			var user, slot int64
			err := tx.QueryRow(ctx, `SELECT ia.user_id, nx.id FROM invigilator_assignments ia JOIN slots sl ON sl.id = ia.slot_id
				JOIN slots nx ON nx.period_id = sl.period_id AND nx.day_index = sl.day_index AND nx.session_index = sl.session_index + 1
				WHERE ia.schedule_id = $1 ORDER BY ia.id LIMIT 1`, sid).Scan(&user, &slot)
			return []any{user, slot}, err == nil, ignoreNoRows(err)
		}},
	{id: "T-4b", task: "T-4", title: "50'den fazla öğrencili salonda en az 2 gözetmen",
		mutation: "kalabalık bir salonun gözetmenlerinden biri sayılmadı", args: 2,
		sql: `WITH n AS (SELECT slot_id, room_id, count(*) AS st FROM seat_assignments WHERE schedule_id = $1 GROUP BY 1, 2),
			g AS (SELECT slot_id, room_id, count(DISTINCT user_id) - CASE WHEN slot_id = $2 AND room_id = $3 THEN 1 ELSE 0 END AS k FROM invigilator_assignments WHERE schedule_id = $1 GROUP BY 1, 2)
			SELECT count(*) FROM n LEFT JOIN g USING (slot_id, room_id) WHERE n.st > 50 AND coalesce(g.k, 0) < 2`,
		mutate: func(ctx context.Context, tx pgx.Tx, sid int64) ([]any, bool, error) {
			var slot, room int64
			err := tx.QueryRow(ctx, `SELECT slot_id, room_id FROM seat_assignments WHERE schedule_id = $1 GROUP BY 1, 2 HAVING count(*) > 50 ORDER BY 1, 2 LIMIT 1`, sid).Scan(&slot, &room)
			return []any{slot, room}, err == nil, ignoreNoRows(err)
		}},
	{id: "T-4c", task: "T-4", title: "Öğretim üyesi kendi dersinin sınavında gözetmen değil",
		mutation: "bir sınavın gözetmeni o dersin öğretim üyesi yapıldı", args: 2,
		// salondaki TÜM sınavlar (paylaşılan salon) denetlenir; bozmadaki sanal görevin salonu yok (0): o oturumun ilk sınavı
		sql: `WITH ` + cteIA + `, x AS (
			SELECT ia.user_id, ra.exam_id FROM ia JOIN room_assignments ra ON ra.schedule_id = $1 AND ra.slot_id = ia.slot_id AND ra.room_id = ia.room_id
			UNION ALL SELECT ia.user_id, (SELECT exam_id FROM schedule_exams WHERE schedule_id = $1 AND slot_id = ia.slot_id ORDER BY exam_id LIMIT 1) FROM ia WHERE ia.room_id = 0)
			SELECT count(*) FROM (SELECT DISTINCT x.user_id, x.exam_id FROM x JOIN exams e ON e.id = x.exam_id JOIN sections s ON s.course_id = e.course_id
			JOIN lecturers l ON l.id = s.lecturer_id AND l.user_id = x.user_id) y`,
		mutate: func(ctx context.Context, tx pgx.Tx, sid int64) ([]any, bool, error) {
			// bir sınavın öğretim üyesini, o sınavın (sıralamada ilk) oturumuna "ek görev" olarak yaz
			var user, slot int64
			err := tx.QueryRow(ctx, `SELECT l.user_id, se.slot_id FROM schedule_exams se JOIN exams e ON e.id = se.exam_id JOIN sections s ON s.course_id = e.course_id
				JOIN lecturers l ON l.id = s.lecturer_id WHERE se.schedule_id = $1
				AND se.exam_id = (SELECT exam_id FROM schedule_exams WHERE schedule_id = $1 AND slot_id = se.slot_id ORDER BY exam_id LIMIT 1)
				ORDER BY se.exam_id LIMIT 1`, sid).Scan(&user, &slot)
			return []any{user, slot}, err == nil, ignoreNoRows(err)
		}},
}

func ignoreNoRows(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}

// RunProofs, denetimleri tek bir salt-okur (REPEATABLE READ) işlemde çalıştırır: tüm sayımlar aynı anlık görüntüden.
func RunProofs(ctx context.Context, pool *pgxpool.Pool, sid int64) (Proofs, error) {
	start := time.Now()
	out := Proofs{ScheduleID: sid, Items: []Proof{}}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// Denetimler kümesel toplamlardır; hash join her zaman doğrusu. Yeni bir çizelgenin on binlerce satırı
	// henüz ANALYZE edilmemişken planlayıcı tabloyu küçük sanıp iç içe döngü seçiyor ve sorgu karesel büyüyordu
	// (ölçüm: verify'da F-3'ten hemen sonra >60 sn, istatistik güncelken 1,8 sn). Ayar yalnız bu işlem içindir.
	if _, err := tx.Exec(ctx, `SET LOCAL enable_nestloop = off`); err != nil {
		return out, err
	}
	err = tx.QueryRow(ctx, `SELECT s.version, (SELECT count(*) FROM schedule_exams WHERE schedule_id = s.id), (SELECT count(*) FROM seat_assignments WHERE schedule_id = s.id),
		(SELECT count(*) FROM invigilator_assignments WHERE schedule_id = s.id) FROM schedules s WHERE s.id = $1`, sid).Scan(&out.Version, &out.Exams, &out.Seats, &out.Duties)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, ErrNotFound
	}
	if err != nil {
		return out, err
	}
	for _, sp := range proofSpecs {
		t0 := time.Now()
		p := Proof{ID: sp.id, Task: sp.task, Title: sp.title, Mutation: sp.mutation}
		zero := make([]any, 0, sp.args+1)
		zero = append(zero, sid)
		for i := 0; i < sp.args; i++ {
			zero = append(zero, 0)
		}
		if err := tx.QueryRow(ctx, sp.sql, zero...).Scan(&p.Found); err != nil {
			return out, fmt.Errorf("%s: %w", sp.id, err)
		}
		margs, ok, err := sp.mutate(ctx, tx, sid)
		if err != nil {
			return out, fmt.Errorf("%s bozma: %w", sp.id, err)
		}
		if ok {
			if err := tx.QueryRow(ctx, sp.sql, append([]any{sid}, margs...)...).Scan(&p.Caught); err != nil {
				return out, fmt.Errorf("%s bozuk: %w", sp.id, err)
			}
		} else {
			p.Detail = "bu çizelgede bozma için uygun örnek yok"
		}
		p.OK = p.Found == 0 && (p.Caught > 0 || !ok)
		p.Ms = time.Since(t0).Milliseconds()
		out.Items = append(out.Items, p)
	}
	out.RanAt = time.Now()
	out.TotalMs = time.Since(start).Milliseconds()
	return out, nil
}

// ProveAppendOnly, uygulama rolünün denetim ve yoklama olay kayıtlarını DEĞİŞTİREMEDİĞİNİ canlı dener:
// geri alınan bir işlemde UPDATE/DELETE çalıştırılır, "permission denied" beklenir. Hiçbir satır değişmez
// (izin denetimi planlama aşamasında yapılır; ayrıca işlem her durumda geri alınır).
func ProveAppendOnly(ctx context.Context, pool *pgxpool.Pool) ([]Proof, error) {
	var out []Proof
	for _, c := range []struct{ id, table, stmt string }{
		{"T-5a", "audit_log", "UPDATE audit_log SET action = action WHERE false"},
		{"T-5b", "attendance_events", "DELETE FROM attendance_events WHERE false"},
	} {
		t0 := time.Now()
		p := Proof{ID: c.id, Task: "T-5", Title: c.table + " eklemeli-yalnız: uygulama rolü değiştiremez",
			Mutation: "uygulama rolüyle " + strings.Fields(c.stmt)[0] + " denendi (geri alınan işlemde)"}
		tx, err := pool.Begin(ctx)
		if err != nil {
			return out, err
		}
		_, err = tx.Exec(ctx, c.stmt)
		_ = tx.Rollback(ctx)
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "42501" { // insufficient_privilege
			p.Caught, p.Detail = 1, "reddedildi: "+pg.Message
		} else if err != nil {
			p.Detail = "beklenmeyen hata: " + err.Error()
		} else {
			p.Detail = "UPDATE/DELETE kabul edildi — yetki eksik değil!"
		}
		p.OK = p.Caught == 1
		p.Ms = time.Since(t0).Milliseconds()
		out = append(out, p)
	}
	return out, nil
}
