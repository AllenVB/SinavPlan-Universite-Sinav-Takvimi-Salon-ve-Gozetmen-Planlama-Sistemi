package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/AllenVB/sinavplan/backend/internal/audit"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ValidationError, girdi doğrulama hatası (alan bazlı).
type ValidationError struct{ Fields map[string]string }

func (e *ValidationError) Error() string { return "doğrulama hatası" }

// RoomInput, salon oluşturma/güncelleme girdisi. Koltuk düzeni: Rows×Cols ızgara;
// Inactive kullanılmayan, Accessible erişilebilir koltukların [satır, sütun] listesi.
type RoomInput struct {
	Code       string   `json:"code"`
	Building   string   `json:"building"`
	Floor      int      `json:"floor"`
	Type       string   `json:"type"`
	Rows       int      `json:"rows"`
	Cols       int      `json:"cols"`
	Inactive   [][2]int `json:"inactive_seats"`
	Accessible [][2]int `json:"accessible_seats"`
	// Kapı: boş bırakılırsa front/0. DoorPos duvar boyunca 0 tabanlı konumdur.
	DoorSide string `json:"door_side"`
	DoorPos  int    `json:"door_pos"`
}

// doorSide, boş değeri varsayılana çevirir.
func (in RoomInput) doorSide() string {
	if in.DoorSide == "" {
		return "front"
	}
	return in.DoorSide
}

// Validate, girdiyi denetler.
func (in RoomInput) Validate() error {
	f := map[string]string{}
	if in.Code == "" || len(in.Code) > 20 {
		f["code"] = "boş olamaz, en fazla 20 karakter"
	}
	if in.Building == "" || len(in.Building) > 50 {
		f["building"] = "boş olamaz, en fazla 50 karakter"
	}
	if in.Floor < -3 || in.Floor > 50 {
		f["floor"] = "-3 ile 50 arasında olmalı"
	}
	if in.Type != "classic" && in.Type != "lab" {
		f["type"] = "classic veya lab olmalı"
	}
	if in.Rows < 1 || in.Rows > 40 {
		f["rows"] = "1 ile 40 arasında olmalı"
	}
	if in.Cols < 1 || in.Cols > 40 {
		f["cols"] = "1 ile 40 arasında olmalı"
	}
	switch in.doorSide() {
	case "front", "back":
		if in.DoorPos < 0 || in.DoorPos >= in.Cols {
			f["door_pos"] = "ön/arka duvarda 0 ile sütun sayısı-1 arasında olmalı"
		}
	case "left", "right":
		if in.DoorPos < 0 || in.DoorPos >= in.Rows {
			f["door_pos"] = "sol/sağ duvarda 0 ile sıra sayısı-1 arasında olmalı"
		}
	default:
		f["door_side"] = "front, back, left veya right olmalı"
	}
	if len(f) == 0 {
		inactive := map[[2]int]bool{}
		for _, p := range in.Inactive {
			if p[0] < 0 || p[0] >= in.Rows || p[1] < 0 || p[1] >= in.Cols {
				f["inactive_seats"] = fmt.Sprintf("ızgara dışı koltuk %v", p)
				break
			}
			inactive[p] = true
		}
		for _, p := range in.Accessible {
			if p[0] < 0 || p[0] >= in.Rows || p[1] < 0 || p[1] >= in.Cols {
				f["accessible_seats"] = fmt.Sprintf("ızgara dışı koltuk %v", p)
				break
			}
			if inactive[p] {
				f["accessible_seats"] = fmt.Sprintf("erişilebilir koltuk %v kullanılmayan listesinde", p)
				break
			}
		}
		if len(f) == 0 && in.Rows*in.Cols-len(inactive) <= 0 {
			f["inactive_seats"] = "en az bir aktif koltuk kalmalı"
		}
	}
	if len(f) > 0 {
		return &ValidationError{f}
	}
	return nil
}

// Room, salon özeti.
type Room struct {
	ID       int64  `json:"id"`
	Code     string `json:"code"`
	Building string `json:"building"`
	Floor    int16  `json:"floor"`
	Type     string `json:"type"`
	Rows     int16  `json:"rows"`
	Cols     int16  `json:"cols"`
	Capacity int    `json:"capacity"`
	// Kapı konumu (krokide gösterilir).
	DoorSide string `json:"door_side"`
	DoorPos  int16  `json:"door_pos"`
	// Accessible: erişilebilir koltuk sayısı.
	Accessible int `json:"accessible_seats"`
}

// RoomSeat, koltuk.
type RoomSeat struct {
	ID         int64 `json:"id"`
	Row        int16 `json:"row"`
	Col        int16 `json:"col"`
	Active     bool  `json:"active"`
	Accessible bool  `json:"accessible"`
}

// RoomDetail, koltuk düzeniyle salon.
type RoomDetail struct {
	Room
	Seats []RoomSeat `json:"seats"`
}

const roomCols = `r.id, r.code, r.building, r.floor, r.type, r.seat_rows, r.seat_cols, r.capacity, r.door_side, r.door_pos,
	(SELECT count(*) FROM "wm-room_seats" s WHERE s.room_id = r.id AND s.is_accessible AND s.is_active)::int`

func scanRoom(row pgx.Row) (Room, error) {
	var r Room
	err := row.Scan(&r.ID, &r.Code, &r.Building, &r.Floor, &r.Type, &r.Rows, &r.Cols, &r.Capacity, &r.DoorSide, &r.DoorPos, &r.Accessible)
	return r, err
}

// ListRooms, salonları keyset sayfalı döner; typ boş değilse tipe göre süzer.
func ListRooms(ctx context.Context, pool *pgxpool.Pool, typ string, after int64, limit int) ([]Room, error) {
	rows, err := pool.Query(ctx, `SELECT `+roomCols+` FROM "wm-rooms" r WHERE r.id > $1 AND ($2 = '' OR r.type = $2) ORDER BY r.id LIMIT $3`, after, typ, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Room, error) { return scanRoom(r) })
}

// GetRoom, koltuk düzeniyle salonu döner.
func GetRoom(ctx context.Context, pool *pgxpool.Pool, id int64) (RoomDetail, error) {
	r, err := scanRoom(pool.QueryRow(ctx, `SELECT `+roomCols+` FROM "wm-rooms" r WHERE r.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return RoomDetail{}, ErrNotFound
	}
	if err != nil {
		return RoomDetail{}, err
	}
	rows, err := pool.Query(ctx, `SELECT id, row_no, col_no, is_active, is_accessible FROM "wm-room_seats" WHERE room_id = $1 ORDER BY row_no, col_no`, id)
	if err != nil {
		return RoomDetail{}, err
	}
	seats, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (RoomSeat, error) {
		var s RoomSeat
		err := r.Scan(&s.ID, &s.Row, &s.Col, &s.Active, &s.Accessible)
		return s, err
	})
	return RoomDetail{Room: r, Seats: seats}, err
}

func isUniqueViolation(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}

func isFKViolation(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23503"
}

func writeSeats(ctx context.Context, tx pgx.Tx, roomID int64, in RoomInput) (int, error) {
	inactive := map[[2]int]bool{}
	for _, p := range in.Inactive {
		inactive[p] = true
	}
	acc := map[[2]int]bool{}
	for _, p := range in.Accessible {
		acc[p] = true
	}
	var rows [][]any
	active := 0
	for r := 0; r < in.Rows; r++ {
		for c := 0; c < in.Cols; c++ {
			k := [2]int{r, c}
			if !inactive[k] {
				active++
			}
			rows = append(rows, []any{roomID, int16(r), int16(c), !inactive[k], acc[k]})
		}
	}
	_, err := tx.CopyFrom(ctx, pgx.Identifier{TableName("room_seats")}, []string{"room_id", "row_no", "col_no", "is_active", "is_accessible"}, pgx.CopyFromRows(rows))
	return active, err
}

// CreateRoom, salonu ve koltuk düzenini oluşturur. Kod tekrarında ErrConflict.
func CreateRoom(ctx context.Context, pool *pgxpool.Pool, in RoomInput, actor int64) (RoomDetail, error) {
	if err := in.Validate(); err != nil {
		return RoomDetail{}, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return RoomDetail{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO "wm-rooms" (code, building, floor, type, seat_rows, seat_cols, capacity, door_side, door_pos) VALUES ($1,$2,$3,$4,$5,$6,1,$7,$8) RETURNING id`,
		in.Code, in.Building, in.Floor, in.Type, in.Rows, in.Cols, in.doorSide(), in.DoorPos).Scan(&id)
	if isUniqueViolation(err) {
		return RoomDetail{}, ErrConflict
	}
	if err != nil {
		return RoomDetail{}, err
	}
	active, err := writeSeats(ctx, tx, id, in)
	if err != nil {
		return RoomDetail{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE "wm-rooms" SET capacity = $2 WHERE id = $1`, id, active); err != nil {
		return RoomDetail{}, err
	}
	if err := audit.Write(ctx, tx, audit.Entry{ActorID: actor, Action: "room.create", Entity: "room", EntityID: fmt.Sprint(id), New: in}); err != nil {
		return RoomDetail{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RoomDetail{}, err
	}
	return GetRoom(ctx, pool, id)
}

func roomReferenced(ctx context.Context, tx pgx.Tx, id int64) (bool, error) {
	var n int
	err := tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM "wm-room_assignments" WHERE room_id = $1) +
		(SELECT count(*) FROM "wm-seat_assignments" WHERE room_id = $1) + (SELECT count(*) FROM "wm-invigilator_assignments" WHERE room_id = $1) +
		(SELECT count(*) FROM "wm-room_change_events" WHERE from_room_id = $1 OR to_room_id = $1)`, id).Scan(&n)
	return n > 0, err
}

// UpdateRoom, salonu günceller. Koltuk düzeni (satır/sütun/pasif/erişilebilir) değişiyorsa
// ve salon herhangi bir çizelgede kullanılıyorsa ErrConflict: kullanılan salonun düzeni
// yayınlanmış oturma planlarını geçersiz kılar.
func UpdateRoom(ctx context.Context, pool *pgxpool.Pool, id int64, in RoomInput, actor int64) (RoomDetail, error) {
	if err := in.Validate(); err != nil {
		return RoomDetail{}, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return RoomDetail{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var oldRows, oldCols int
	if err := tx.QueryRow(ctx, `SELECT seat_rows, seat_cols FROM "wm-rooms" WHERE id = $1 FOR UPDATE`, id).Scan(&oldRows, &oldCols); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return RoomDetail{}, ErrNotFound
		}
		return RoomDetail{}, err
	}
	// Düzen değişti mi? Mevcut pasif/erişilebilir kümelerle karşılaştır.
	cur, err := tx.Query(ctx, `SELECT row_no, col_no, is_active, is_accessible FROM "wm-room_seats" WHERE room_id = $1`, id)
	if err != nil {
		return RoomDetail{}, err
	}
	curInactive, curAcc := map[[2]int]bool{}, map[[2]int]bool{}
	for cur.Next() {
		var r, c int
		var a, x bool
		if err := cur.Scan(&r, &c, &a, &x); err != nil {
			cur.Close()
			return RoomDetail{}, err
		}
		if !a {
			curInactive[[2]int{r, c}] = true
		}
		if x {
			curAcc[[2]int{r, c}] = true
		}
	}
	cur.Close()
	same := func(a map[[2]int]bool, b [][2]int) bool {
		m := map[[2]int]bool{}
		for _, p := range b {
			m[p] = true
		}
		if len(a) != len(m) {
			return false
		}
		for k := range a {
			if !m[k] {
				return false
			}
		}
		return true
	}
	layoutChanged := oldRows != in.Rows || oldCols != in.Cols || !same(curInactive, in.Inactive) || !same(curAcc, in.Accessible)
	if layoutChanged {
		ref, err := roomReferenced(ctx, tx, id)
		if err != nil {
			return RoomDetail{}, err
		}
		if ref {
			return RoomDetail{}, ErrConflict
		}
		if _, err := tx.Exec(ctx, `DELETE FROM "wm-room_seats" WHERE room_id = $1`, id); err != nil {
			return RoomDetail{}, err
		}
		active, err := writeSeats(ctx, tx, id, in)
		if err != nil {
			return RoomDetail{}, err
		}
		if _, err := tx.Exec(ctx, `UPDATE "wm-rooms" SET capacity = $2, seat_rows = $3, seat_cols = $4 WHERE id = $1`, id, active, in.Rows, in.Cols); err != nil {
			return RoomDetail{}, err
		}
	}
	if _, err := tx.Exec(ctx, `UPDATE "wm-rooms" SET code=$2, building=$3, floor=$4, type=$5, door_side=$6, door_pos=$7 WHERE id=$1`, id, in.Code, in.Building, in.Floor, in.Type, in.doorSide(), in.DoorPos); err != nil {
		if isUniqueViolation(err) {
			return RoomDetail{}, ErrConflict
		}
		return RoomDetail{}, err
	}
	if err := audit.Write(ctx, tx, audit.Entry{ActorID: actor, Action: "room.update", Entity: "room", EntityID: fmt.Sprint(id),
		New: map[string]any{"layout_changed": layoutChanged, "room": in}}); err != nil {
		return RoomDetail{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return RoomDetail{}, err
	}
	return GetRoom(ctx, pool, id)
}

// DeleteRoom, kullanılmayan salonu siler; çizelgelerde kullanılıyorsa ErrConflict.
func DeleteRoom(ctx context.Context, pool *pgxpool.Pool, id, actor int64) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var code string
	if err := tx.QueryRow(ctx, `SELECT code FROM "wm-rooms" WHERE id = $1 FOR UPDATE`, id).Scan(&code); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	ref, err := roomReferenced(ctx, tx, id)
	if err != nil {
		return err
	}
	if ref {
		return ErrConflict
	}
	if _, err := tx.Exec(ctx, `DELETE FROM "wm-rooms" WHERE id = $1`, id); err != nil {
		if isFKViolation(err) {
			return ErrConflict
		}
		return err
	}
	if err := audit.Write(ctx, tx, audit.Entry{ActorID: actor, Action: "room.delete", Entity: "room", EntityID: fmt.Sprint(id), Old: map[string]any{"code": code}}); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
