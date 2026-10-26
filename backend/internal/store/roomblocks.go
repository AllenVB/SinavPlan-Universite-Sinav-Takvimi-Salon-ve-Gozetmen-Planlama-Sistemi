package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/AllenVB/sinavplan/backend/internal/audit"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RoomBlock, bir salonun bir oturumda kullanıma kapalı olduğunu söyler (bakım, başka etkinlik). Bir sonraki takvim/salon
// planı üretiminde T-2 bu salonu o oturumda kullanmaz; mevcut çizelgelere dokunmaz.
type RoomBlock struct {
	ID        int64     `json:"id"`
	RoomID    int64     `json:"room_id"`
	RoomCode  string    `json:"room_code"`
	Building  string    `json:"building"`
	SlotID    int64     `json:"slot_id"`
	Day       int       `json:"day"`
	Session   int       `json:"session"`
	StartsAt  time.Time `json:"starts_at"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
	// AffectedExams: yayındaki çizelgede bu salon + oturumda hâlen yerleşik sınav sayısı (yeni plan üretilene dek sürer).
	AffectedExams int `json:"affected_exams"`
}

const roomBlockSelect = `SELECT rb.id, rb.room_id, r.code, r.building, rb.slot_id, sl.day_index, sl.session_index, sl.starts_at, rb.reason, rb.created_at,
	(SELECT count(DISTINCT ra.exam_id) FROM "wm-room_assignments" ra JOIN "wm-schedules" sc ON sc.id = ra.schedule_id AND sc.status = 'published'
	 WHERE ra.room_id = rb.room_id AND ra.slot_id = rb.slot_id)
	FROM "wm-room_blocks" rb JOIN "wm-rooms" r ON r.id = rb.room_id JOIN "wm-slots" sl ON sl.id = rb.slot_id`

func scanRoomBlock(r pgx.CollectableRow) (RoomBlock, error) {
	var b RoomBlock
	var d, s int16
	err := r.Scan(&b.ID, &b.RoomID, &b.RoomCode, &b.Building, &b.SlotID, &d, &s, &b.StartsAt, &b.Reason, &b.CreatedAt, &b.AffectedExams)
	b.Day, b.Session = int(d), int(s)
	return b, err
}

// ListRoomBlocks, kapalı salon kayıtlarını tarih sırasıyla döner.
func ListRoomBlocks(ctx context.Context, pool *pgxpool.Pool) ([]RoomBlock, error) {
	rows, err := pool.Query(ctx, roomBlockSelect+` ORDER BY sl.starts_at, r.code`)
	if err != nil {
		return nil, err
	}
	items, err := pgx.CollectRows(rows, scanRoomBlock)
	if items == nil {
		items = []RoomBlock{}
	}
	return items, err
}

// CreateRoomBlock, salonu oturum için kapatır. Aynı (salon, oturum) ikinci kez eklenemez (ErrConflict).
func CreateRoomBlock(ctx context.Context, pool *pgxpool.Pool, roomID, slotID int64, reason string, actor int64) (RoomBlock, error) {
	if len([]rune(reason)) > 200 {
		return RoomBlock{}, &ValidationError{map[string]string{"reason": "en fazla 200 karakter"}}
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return RoomBlock{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id int64
	if err := tx.QueryRow(ctx, `INSERT INTO "wm-room_blocks" (room_id, slot_id, reason, created_by) VALUES ($1,$2,$3,$4) RETURNING id`, roomID, slotID, reason, actor).Scan(&id); err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) {
			switch pg.Code {
			case "23505":
				return RoomBlock{}, ErrConflict
			case "23503":
				return RoomBlock{}, ErrNotFound
			}
		}
		return RoomBlock{}, err
	}
	if err := audit.Write(ctx, tx, audit.Entry{ActorID: actor, Action: "room_block.create", Entity: "room_block", EntityID: fmt.Sprint(id),
		New: map[string]any{"room_id": roomID, "slot_id": slotID, "reason": reason}}); err != nil {
		return RoomBlock{}, err
	}
	rows, err := tx.Query(ctx, roomBlockSelect+` WHERE rb.id = $1`, id)
	if err != nil {
		return RoomBlock{}, err
	}
	b, err := pgx.CollectOneRow(rows, scanRoomBlock)
	if err != nil {
		return RoomBlock{}, err
	}
	return b, tx.Commit(ctx)
}

// DeleteRoomBlock, kapatmayı kaldırır.
func DeleteRoomBlock(ctx context.Context, pool *pgxpool.Pool, id, actor int64) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var roomID, slotID int64
	if err := tx.QueryRow(ctx, `DELETE FROM "wm-room_blocks" WHERE id = $1 RETURNING room_id, slot_id`, id).Scan(&roomID, &slotID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if err := audit.Write(ctx, tx, audit.Entry{ActorID: actor, Action: "room_block.delete", Entity: "room_block", EntityID: fmt.Sprint(id),
		Old: map[string]any{"room_id": roomID, "slot_id": slotID}}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
