package store

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// CheckinFailures, kullanıcının since anından bu yana kaydedilen hatalı kod denemesi sayısını döner.
func CheckinFailures(ctx context.Context, pool *pgxpool.Pool, userID int64, since time.Time) (int, error) {
	var n int
	err := pool.QueryRow(ctx, `SELECT count(*) FROM "wm-checkin_failures" WHERE user_id = $1 AND failed_at >= $2`, userID, since).Scan(&n)
	return n, err
}

// RecordCheckinFailure, hatalı denemeyi ekler ve aynı kullanıcının olderThan'dan eski kayıtlarını siler
// (sayaç penceresi dışında kalanlar artık gerekmez). Tek ifade: ekleme ve temizlik birlikte olur.
func RecordCheckinFailure(ctx context.Context, pool *pgxpool.Pool, userID int64, at, olderThan time.Time) error {
	_, err := pool.Exec(ctx, `
		WITH cleanup AS (DELETE FROM "wm-checkin_failures" WHERE user_id = $1 AND failed_at < $3)
		INSERT INTO "wm-checkin_failures" (user_id, failed_at) VALUES ($1, $2)`, userID, at, olderThan)
	return err
}
