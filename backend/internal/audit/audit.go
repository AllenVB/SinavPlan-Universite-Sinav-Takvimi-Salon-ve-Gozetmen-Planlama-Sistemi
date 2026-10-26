// Package audit, kritik işlemleri eklemeli-yalnız audit_log tablosuna yazar.
//
// Yazım çağıranın transaction'ı içinde yapılır: iş değişikliği geri alınırsa denetim
// kaydı da geri alınır, commit edilirse ikisi birlikte kalıcı olur. "İşlem oldu ama
// kaydı yok" veya "kayıt var ama işlem olmadı" durumu oluşamaz.
package audit

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"
)

// Execer, pgx.Tx ve pgxpool.Pool'un ortak alt kümesi. Fonksiyon bilerek bir
// transaction ister; havuz verilirse aynı-transaction garantisi kaybolur.
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Entry, bir denetim kaydıdır. Old/New JSON'a çevrilir; nil ise NULL yazılır.
type Entry struct {
	ActorID  int64 // 0 = sistem
	Action   string
	Entity   string
	EntityID string
	Old, New any
}

// Write, kaydı verilen transaction içinde ekler.
func Write(ctx context.Context, tx Execer, e Entry) error {
	if e.Action == "" || e.Entity == "" || e.EntityID == "" {
		return fmt.Errorf("denetim kaydı eksik: action, entity ve entity_id zorunlu")
	}
	oldJSON, err := toJSON(e.Old)
	if err != nil {
		return err
	}
	newJSON, err := toJSON(e.New)
	if err != nil {
		return err
	}
	var actor any
	if e.ActorID != 0 {
		actor = e.ActorID
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO "wm-audit_log" (actor_id, action, entity, entity_id, old_value, new_value)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		actor, e.Action, e.Entity, e.EntityID, oldJSON, newJSON)
	if err != nil {
		return fmt.Errorf("denetim kaydı yazılamadı: %w", err)
	}
	return nil
}

func toJSON(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("denetim değeri JSON'a çevrilemedi: %w", err)
	}
	return b, nil
}
