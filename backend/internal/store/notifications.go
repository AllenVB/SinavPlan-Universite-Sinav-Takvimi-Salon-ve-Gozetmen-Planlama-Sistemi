package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Notification, bildirim kutusundaki bir kayıt.
type Notification struct {
	ID        int64      `json:"id"`
	Kind      string     `json:"kind"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	Link      string     `json:"link"`
	CreatedAt time.Time  `json:"created_at"`
	ReadAt    *time.Time `json:"read_at"`
}

// execer: havuz ya da işlem; olay bildirimleri çağıranın işleminde yazılır (geri alınırsa kaybolur).
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Notify, bir kullanıcıya bildirim yazar; aynı (kullanıcı, anahtar) ikinci kez yazılmaz.
func Notify(ctx context.Context, db execer, userID int64, kind, title, body, link, key string) error {
	_, err := db.Exec(ctx, `INSERT INTO "wm-notifications" (user_id, kind, title, body, link, dedupe_key)
		VALUES ($1,$2,$3,$4,$5,$6) ON CONFLICT (user_id, dedupe_key) DO NOTHING`, userID, kind, title, body, link, key)
	return err
}

// ListNotifications, kullanıcının bildirimlerini (yeniden eskiye) ve okunmamış sayısını döner.
func ListNotifications(ctx context.Context, pool *pgxpool.Pool, userID int64, limit int) ([]Notification, int, error) {
	rows, err := pool.Query(ctx, `SELECT id, kind, title, body, link, created_at, read_at FROM "wm-notifications"
		WHERE user_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, 0, err
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Notification, error) {
		var n Notification
		return n, r.Scan(&n.ID, &n.Kind, &n.Title, &n.Body, &n.Link, &n.CreatedAt, &n.ReadAt)
	})
	if err != nil {
		return nil, 0, err
	}
	var unread int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM "wm-notifications" WHERE user_id = $1 AND read_at IS NULL`, userID).Scan(&unread); err != nil {
		return nil, 0, err
	}
	if items == nil {
		items = []Notification{}
	}
	return items, unread, nil
}

// MarkNotificationRead, tek bildirimi (id > 0) ya da tümünü (id = 0) okundu yapar; yalnız kendi kayıtları.
func MarkNotificationRead(ctx context.Context, pool *pgxpool.Pool, userID, id int64) (int64, error) {
	tag, err := pool.Exec(ctx, `UPDATE "wm-notifications" SET read_at = now() WHERE user_id = $1 AND read_at IS NULL AND ($2 = 0 OR id = $2)`, userID, id)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

const (
	// AbsentAfter: sınav başladıktan bu kadar sonra yoklaması işaretsiz öğrenciye bildirim gider.
	AbsentAfter = 15 * time.Minute
	// absentUntil: sınav bitişinden bu kadar sonra "gelmedin" bildirimi artık üretilmez (eski sınavlar için gürültü olmasın).
	absentUntil = 12 * time.Hour
	// ReminderWindow: sınav/görev bu süre içindeyse hatırlatma üretilir.
	ReminderWindow = 24 * time.Hour
)

func clock(t time.Time) string { return t.In(istanbul()).Format("02.01.2006 15:04") }

func istanbul() *time.Location {
	if l, err := time.LoadLocation("Europe/Istanbul"); err == nil {
		return l
	}
	return time.FixedZone("TRT", 3*3600)
}

// SyncDueNotifications, zamana bağlı bildirimleri bu kullanıcı için üretir (okuma sırasında; tekrar çağrı güvenli):
//   - öğrenci: 24 saat içindeki sınav hatırlatması; sınav başlayalı 15 dk geçtiği hâlde yoklamada "geldi" kaydı
//     olmayan ve onaylı mazereti olmayan sınav için "yoklamanız alınmadı" uyarısı;
//   - gözetmen: 24 saat içindeki görev hatırlatması.
//
// Yalnız yayındaki çizelge dikkate alınır. Zamanlayıcı/arka plan işi gerektirmez.
func SyncDueNotifications(ctx context.Context, pool *pgxpool.Pool, userID, studentID int64, isInvigilator bool, now time.Time) error {
	if studentID != 0 {
		rows, err := pool.Query(ctx, `
			SELECT e.id, co.code, co.name, sl.starts_at, sl.ends_at, r.code, rs.row_no, rs.col_no,
			       COALESCE((SELECT st.status FROM "wm-attendance_state" st WHERE st.exam_id = e.id AND st.student_id = sa.student_id), '') AS att,
			       EXISTS (SELECT 1 FROM "wm-excuses" x WHERE x.kind='student' AND x.student_id = sa.student_id AND x.exam_id = e.id AND x.status='approved') AS excused
			FROM "wm-seat_assignments" sa
			JOIN "wm-schedules" sc ON sc.id = sa.schedule_id AND sc.status = 'published'
			JOIN "wm-slots" sl ON sl.id = sa.slot_id
			JOIN "wm-exams" e ON e.id = sa.exam_id JOIN "wm-courses" co ON co.id = e.course_id
			JOIN "wm-rooms" r ON r.id = sa.room_id JOIN "wm-room_seats" rs ON rs.id = sa.seat_id
			WHERE sa.student_id = $1 AND sl.ends_at > $2::timestamptz - make_interval(secs => $3)
			  AND sl.starts_at < $2::timestamptz + make_interval(secs => $4)`,
			studentID, now, absentUntil.Seconds(), ReminderWindow.Seconds())
		if err != nil {
			return err
		}
		type ex struct {
			id               int64
			code, name, room string
			starts, ends     time.Time
			row, col         int16
			att              string
			excused          bool
		}
		list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ex, error) {
			var x ex
			return x, r.Scan(&x.id, &x.code, &x.name, &x.starts, &x.ends, &x.room, &x.row, &x.col, &x.att, &x.excused)
		})
		if err != nil {
			return err
		}
		for _, x := range list {
			seat := fmt.Sprintf("%c%d", 'A'+rune(x.row%26), x.col+1)
			switch {
			case x.starts.After(now): // henüz başlamadı → hatırlatma
				if err := Notify(ctx, pool, userID, "exam_reminder", fmt.Sprintf("Sınavınız yaklaşıyor: %s", x.code),
					fmt.Sprintf("%s · %s, %s salonu, koltuk %s. Kartınızı/QR'ınızı hazır bulundurun.", x.name, clock(x.starts), x.room, seat),
					"#/programim", fmt.Sprintf("exam-rem:%d", x.id)); err != nil {
					return err
				}
			case !x.excused && x.att != "present" && !now.Before(x.starts.Add(AbsentAfter)) && now.Before(x.ends.Add(absentUntil)):
				if err := Notify(ctx, pool, userID, "absent", fmt.Sprintf("Yoklamanız alınmadı: %s", x.code),
					fmt.Sprintf("%s sınavı %s'te başladı ve yoklamada geliş kaydınız yok. Salondaysanız (%s, koltuk %s) gözetmenin QR/kodunu girin; mazeretiniz varsa Mazeret ekranından bildirin.",
						x.name, clock(x.starts), x.room, seat),
					"#/programim", fmt.Sprintf("absent:%d", x.id)); err != nil {
					return err
				}
			}
		}
	}
	if isInvigilator {
		rows, err := pool.Query(ctx, `
			SELECT ia.id, sl.starts_at, r.code, string_agg(DISTINCT co.code, ', ')
			FROM "wm-invigilator_assignments" ia
			JOIN "wm-schedules" sc ON sc.id = ia.schedule_id AND sc.status = 'published'
			JOIN "wm-slots" sl ON sl.id = ia.slot_id JOIN "wm-rooms" r ON r.id = ia.room_id
			JOIN "wm-room_assignments" ra ON ra.schedule_id = ia.schedule_id AND ra.slot_id = ia.slot_id AND ra.room_id = ia.room_id
			JOIN "wm-exams" e ON e.id = ra.exam_id JOIN "wm-courses" co ON co.id = e.course_id
			WHERE ia.user_id = $1 AND sl.starts_at > $2::timestamptz AND sl.starts_at <= $2::timestamptz + make_interval(secs => $3)
			GROUP BY ia.id, sl.starts_at, r.code`, userID, now, ReminderWindow.Seconds())
		if err != nil {
			return err
		}
		type duty struct {
			id           int64
			starts       time.Time
			room, course string
		}
		list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (duty, error) {
			var d duty
			return d, r.Scan(&d.id, &d.starts, &d.room, &d.course)
		})
		if err != nil {
			return err
		}
		for _, d := range list {
			if err := Notify(ctx, pool, userID, "duty_reminder", fmt.Sprintf("Gözetmenlik görevi yaklaşıyor: %s", d.room),
				fmt.Sprintf("%s · %s salonu · %s. Yoklama ekranından QR/kod oluşturabilirsiniz.", clock(d.starts), d.room, d.course),
				"#/yoklama", fmt.Sprintf("duty-rem:%d", d.id)); err != nil {
				return err
			}
		}
	}
	return nil
}
