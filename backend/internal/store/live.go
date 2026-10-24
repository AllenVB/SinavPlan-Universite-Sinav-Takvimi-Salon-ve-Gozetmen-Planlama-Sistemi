package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// LiveSlot, canlı izlenen oturum. Mode: active (şu an sürüyor), upcoming (sıradaki) veya past (son biten).
type LiveSlot struct {
	ID       int64     `json:"id"`
	Day      int16     `json:"day"`
	Session  int16     `json:"session"`
	StartsAt time.Time `json:"starts_at"`
	EndsAt   time.Time `json:"ends_at"`
	Mode     string    `json:"mode"`
}

// LiveExam, bir salondaki sınav payı.
type LiveExam struct {
	ExamID     int64  `json:"exam_id"`
	CourseCode string `json:"course_code"`
	CourseName string `json:"course_name"`
	Seated     int    `json:"seated"`
}

// LiveInvigilator, salondaki gözetmen.
type LiveInvigilator struct {
	UserID   int64  `json:"user_id"`
	FullName string `json:"full_name"`
}

// LiveRoom, bir salonun oturum içindeki canlı yoklama durumu.
type LiveRoom struct {
	RoomID       int64             `json:"room_id"`
	RoomCode     string            `json:"room_code"`
	Building     string            `json:"building"`
	RoomType     string            `json:"room_type"`
	Capacity     int               `json:"capacity"`
	Seated       int               `json:"seated"`
	Present      int               `json:"present"`
	Absent       int               `json:"absent"`
	Unmarked     int               `json:"unmarked"`
	LastEventAt  *time.Time        `json:"last_event_at"`
	Devices      int               `json:"devices"`
	Exams        []LiveExam        `json:"exams"`
	Invigilators []LiveInvigilator `json:"invigilators"`
}

// LiveTotals, oturumun toplamları (salon satırlarından türetilir; ayrı sorgu değil).
type LiveTotals struct {
	Rooms    int `json:"rooms"`
	Seated   int `json:"seated"`
	Present  int `json:"present"`
	Absent   int `json:"absent"`
	Unmarked int `json:"unmarked"`
}

// LiveOverview, GET /schedules/{id}/live yanıtı.
type LiveOverview struct {
	Slot        LiveSlot   `json:"slot"`
	Totals      LiveTotals `json:"totals"`
	Rooms       []LiveRoom `json:"rooms"`
	GeneratedAt time.Time  `json:"generated_at"`
}

// PickLiveSlot, slotID verilmişse onu; verilmemişse şu an süren, yoksa sıradaki, yoksa son oturumu seçer.
func PickLiveSlot(ctx context.Context, pool *pgxpool.Pool, scheduleID, slotID int64, now time.Time) (LiveSlot, error) {
	q := `SELECT sl.id, sl.day_index, sl.session_index, sl.starts_at, sl.ends_at
		FROM slots sl WHERE sl.period_id = (SELECT period_id FROM schedules WHERE id = $1)
		AND EXISTS (SELECT 1 FROM schedule_exams se WHERE se.schedule_id = $1 AND se.slot_id = sl.id)`
	var args = []any{scheduleID}
	if slotID != 0 {
		q += ` AND sl.id = $2`
		args = append(args, slotID)
	}
	rows, err := pool.Query(ctx, q+` ORDER BY sl.starts_at`, args...)
	if err != nil {
		return LiveSlot{}, err
	}
	slots, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (LiveSlot, error) {
		var x LiveSlot
		err := r.Scan(&x.ID, &x.Day, &x.Session, &x.StartsAt, &x.EndsAt)
		return x, err
	})
	if err != nil {
		return LiveSlot{}, err
	}
	if len(slots) == 0 {
		return LiveSlot{}, ErrNotFound
	}
	modeOf := func(s LiveSlot) string {
		switch {
		case now.Before(s.StartsAt):
			return "upcoming"
		case now.Before(s.EndsAt):
			return "active"
		}
		return "past"
	}
	pick := slots[len(slots)-1] // hiçbiri sürmüyor ve sıradaki yoksa: son biten
	if slotID != 0 {
		pick = slots[0]
	} else {
		for _, s := range slots { // slots başlangıca göre sıralı: önce süren, yoksa ilk sıradaki
			if m := modeOf(s); m == "active" {
				pick = s
				break
			} else if m == "upcoming" && modeOf(pick) != "upcoming" {
				pick = s
			}
		}
	}
	mode := modeOf(pick)
	pick.Mode = mode
	return pick, nil
}

// Live, bir oturumdaki salonların yoklama özetini döner: oturan, gelen, gelmedi, işaretlenmemiş.
// Yoklama durumu attendance_state'ten (olaylardan türetilen son durum) okunur.
func Live(ctx context.Context, pool *pgxpool.Pool, scheduleID int64, slot LiveSlot) (LiveOverview, error) {
	out := LiveOverview{Slot: slot, Rooms: []LiveRoom{}, GeneratedAt: time.Now()}

	rows, err := pool.Query(ctx, `
		SELECT sa.room_id, r.code, r.building, r.type, r.capacity,
		       count(*)::int,
		       count(*) FILTER (WHERE st.status = 'present')::int,
		       count(*) FILTER (WHERE st.status = 'absent')::int,
		       max(st.updated_at),
		       count(DISTINCT st.device_id)::int
		FROM seat_assignments sa
		JOIN schedule_exams se ON se.schedule_id = sa.schedule_id AND se.exam_id = sa.exam_id
		JOIN rooms r ON r.id = sa.room_id
		LEFT JOIN attendance_state st ON st.exam_id = sa.exam_id AND st.student_id = sa.student_id
		WHERE sa.schedule_id = $1 AND se.slot_id = $2
		GROUP BY sa.room_id, r.code, r.building, r.type, r.capacity
		ORDER BY r.building, r.code`, scheduleID, slot.ID)
	if err != nil {
		return out, err
	}
	byRoom := map[int64]int{}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (LiveRoom, error) {
		var x LiveRoom
		err := r.Scan(&x.RoomID, &x.RoomCode, &x.Building, &x.RoomType, &x.Capacity, &x.Seated, &x.Present, &x.Absent, &x.LastEventAt, &x.Devices)
		x.Unmarked = x.Seated - x.Present - x.Absent
		x.Exams, x.Invigilators = []LiveExam{}, []LiveInvigilator{}
		return x, err
	})
	if err != nil {
		return out, err
	}
	for i, x := range list {
		byRoom[x.RoomID] = i
	}

	ex, err := pool.Query(ctx, `
		SELECT ra.room_id, ra.exam_id, c.code, c.name, ra.seat_count
		FROM room_assignments ra JOIN exams e ON e.id = ra.exam_id JOIN courses c ON c.id = e.course_id
		WHERE ra.schedule_id = $1 AND ra.slot_id = $2 ORDER BY ra.room_id, ra.exam_id`, scheduleID, slot.ID)
	if err != nil {
		return out, err
	}
	defer ex.Close()
	for ex.Next() {
		var room int64
		var e LiveExam
		if err := ex.Scan(&room, &e.ExamID, &e.CourseCode, &e.CourseName, &e.Seated); err != nil {
			return out, err
		}
		if i, ok := byRoom[room]; ok {
			list[i].Exams = append(list[i].Exams, e)
		}
	}
	if err := ex.Err(); err != nil {
		return out, err
	}

	iv, err := pool.Query(ctx, `
		SELECT ia.room_id, u.id, u.full_name FROM invigilator_assignments ia JOIN users u ON u.id = ia.user_id
		WHERE ia.schedule_id = $1 AND ia.slot_id = $2 ORDER BY ia.room_id, u.full_name`, scheduleID, slot.ID)
	if err != nil {
		return out, err
	}
	defer iv.Close()
	for iv.Next() {
		var room int64
		var v LiveInvigilator
		if err := iv.Scan(&room, &v.UserID, &v.FullName); err != nil {
			return out, err
		}
		if i, ok := byRoom[room]; ok {
			list[i].Invigilators = append(list[i].Invigilators, v)
		}
	}
	if err := iv.Err(); err != nil {
		return out, err
	}

	out.Rooms = list
	out.Totals.Rooms = len(list)
	for _, x := range list {
		out.Totals.Seated += x.Seated
		out.Totals.Present += x.Present
		out.Totals.Absent += x.Absent
		out.Totals.Unmarked += x.Unmarked
	}
	return out, nil
}

// ActivityFilter, denetim akışı süzgeci. After > 0: yalnız daha yeni kayıtlar (canlı akış);
// Before > 0: daha eski kayıtlar (sayfalama). İkisi birlikte verilmez.
type ActivityFilter struct {
	ActionPrefix string
	ActorID      int64
	After        int64
	Before       int64
	Limit        int
}

// ErrBadFilter, çelişkili süzgeç.
var ErrBadFilter = errors.New("after ve before birlikte kullanılamaz")

// ActivityPage, süzülmüş akış ve görülen en yeni kimlik (artımlı okuma için).
func ActivityPage(ctx context.Context, pool *pgxpool.Pool, f ActivityFilter) ([]Activity, int64, error) {
	if f.After > 0 && f.Before > 0 {
		return nil, 0, ErrBadFilter
	}
	rows, err := pool.Query(ctx, `
		SELECT a.id, a.action, a.entity, a.entity_id, u.full_name, coalesce(a.new_value, 'null'::jsonb), a.created_at
		FROM audit_log a LEFT JOIN users u ON u.id = a.actor_id
		WHERE ($1 = '' OR a.action LIKE $1 || '%') AND ($2 = 0 OR a.actor_id = $2)
		  AND ($3 = 0 OR a.id > $3) AND ($4 = 0 OR a.id < $4)
		ORDER BY a.id DESC LIMIT $5`, f.ActionPrefix, f.ActorID, f.After, f.Before, f.Limit)
	if err != nil {
		return nil, 0, err
	}
	items, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Activity, error) {
		var x Activity
		var d []byte
		err := r.Scan(&x.ID, &x.Action, &x.Entity, &x.EntityID, &x.Actor, &d, &x.CreatedAt)
		if string(d) != "null" {
			x.Detail = d
		}
		return x, err
	})
	var latest int64
	if err == nil {
		err = pool.QueryRow(ctx, `SELECT coalesce(max(id), 0) FROM audit_log`).Scan(&latest)
	}
	return items, latest, err
}

// LiveEvent, canlı olay akışındaki bir yoklama olayı (cihazdan gelen ham olay, geliş sırasıyla).
type LiveEvent struct {
	EventID    string    `json:"event_id"`
	ReceivedAt time.Time `json:"received_at"`
	Status     string    `json:"status"`
	Source     string    `json:"source"`
	DeviceID   string    `json:"device_id"`
	ExamID     int64     `json:"exam_id"`
	CourseCode string    `json:"course_code"`
	StudentNo  string    `json:"student_no"`
	FullName   string    `json:"full_name"`
	RoomCode   *string   `json:"room_code"`
}

// LiveEvents, bir oturumdaki yoklama olaylarını (yeniden eskiye) döner; since verilirse yalnız daha yenileri.
func LiveEvents(ctx context.Context, pool *pgxpool.Pool, scheduleID, slotID int64, since *time.Time, limit int) ([]LiveEvent, error) {
	rows, err := pool.Query(ctx, `
		SELECT ev.event_id::text, ev.received_at, ev.status, ev.source, ev.device_id, ev.exam_id, c.code, st.student_no, st.full_name, r.code
		FROM attendance_events ev
		JOIN schedule_exams se ON se.schedule_id = $1 AND se.exam_id = ev.exam_id
		JOIN exams e ON e.id = ev.exam_id JOIN courses c ON c.id = e.course_id
		JOIN students st ON st.id = ev.student_id
		LEFT JOIN seat_assignments sa ON sa.schedule_id = $1 AND sa.exam_id = ev.exam_id AND sa.student_id = ev.student_id
		LEFT JOIN rooms r ON r.id = sa.room_id
		WHERE se.slot_id = $2 AND ($3::timestamptz IS NULL OR ev.received_at > $3)
		ORDER BY ev.received_at DESC, ev.event_id DESC LIMIT $4`, scheduleID, slotID, since, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (LiveEvent, error) {
		var x LiveEvent
		err := r.Scan(&x.EventID, &x.ReceivedAt, &x.Status, &x.Source, &x.DeviceID, &x.ExamID, &x.CourseCode, &x.StudentNo, &x.FullName, &x.RoomCode)
		return x, err
	})
}

// SeatMap, öğrencinin kendi salonunun krokisi: yalnız düzen ve kendi koltuğu (başka öğrenci verisi yok).
type SeatMap struct {
	RoomCode string        `json:"room_code"`
	Building string        `json:"building"`
	Rows     int16         `json:"rows"`
	Cols     int16         `json:"cols"`
	Seats    []SeatMapCell `json:"seats"`
	MineRow  int16         `json:"mine_row"`
	MineCol  int16         `json:"mine_col"`
}

// SeatMapCell, krokideki bir koltuk.
type SeatMapCell struct {
	Row        int16 `json:"row"`
	Col        int16 `json:"col"`
	Active     bool  `json:"active"`
	Accessible bool  `json:"accessible"`
}

// StudentSeatMap, öğrencinin yayınlanmış çizelgedeki koltuğunun salon krokisini döner (yoksa ErrNotFound).
func StudentSeatMap(ctx context.Context, pool *pgxpool.Pool, studentID, examID int64) (SeatMap, error) {
	var m SeatMap
	var roomID int64
	err := pool.QueryRow(ctx, `
		SELECT r.id, r.code, r.building, r.seat_rows, r.seat_cols, rs.row_no, rs.col_no
		FROM seat_assignments sa
		JOIN schedules sch ON sch.id = sa.schedule_id AND sch.status = 'published'
		JOIN rooms r ON r.id = sa.room_id JOIN room_seats rs ON rs.id = sa.seat_id
		WHERE sa.student_id = $1 AND sa.exam_id = $2`, studentID, examID).
		Scan(&roomID, &m.RoomCode, &m.Building, &m.Rows, &m.Cols, &m.MineRow, &m.MineCol)
	if errors.Is(err, pgx.ErrNoRows) {
		return SeatMap{}, ErrNotFound
	}
	if err != nil {
		return SeatMap{}, err
	}
	rows, err := pool.Query(ctx, `SELECT row_no, col_no, is_active, is_accessible FROM room_seats WHERE room_id = $1 ORDER BY row_no, col_no`, roomID)
	if err != nil {
		return SeatMap{}, err
	}
	m.Seats, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (SeatMapCell, error) {
		var c SeatMapCell
		err := r.Scan(&c.Row, &c.Col, &c.Active, &c.Accessible)
		return c, err
	})
	return m, err
}
