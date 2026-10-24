package audit_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/AllenVB/sinavplan/backend/internal/audit"
	"github.com/AllenVB/sinavplan/backend/internal/testdb"
)

func count(t *testing.T, env *testdb.Env) int {
	var n int
	if err := env.App.Pool.QueryRow(context.Background(), `SELECT count(*) FROM audit_log`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Denetim kaydı iş işlemiyle aynı transaction'da: geri alınırsa kayıt da yok.
func TestAuditEntryFollowsTransactionOutcome(t *testing.T) {
	env := testdb.New(t)
	ctx := context.Background()
	e := audit.Entry{ActorID: 1, Action: "schedule.publish", Entity: "schedule", EntityID: "9",
		Old: map[string]string{"status": "draft"}, New: map[string]string{"status": "published"}}

	tx, _ := env.App.Pool.Begin(ctx)
	if err := audit.Write(ctx, tx, e); err != nil {
		t.Fatal(err)
	}
	_ = tx.Rollback(ctx)
	if n := count(t, env); n != 0 {
		t.Fatalf("geri alınan transaction'ın kaydı kalmamalı: %d", n)
	}

	tx, _ = env.App.Pool.Begin(ctx)
	if err := audit.Write(ctx, tx, e); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var oldV, newV []byte
	var actor int64
	_ = env.App.Pool.QueryRow(ctx, `SELECT actor_id, old_value, new_value FROM audit_log`).Scan(&actor, &oldV, &newV)
	var o, n map[string]string
	_ = json.Unmarshal(oldV, &o)
	_ = json.Unmarshal(newV, &n)
	if actor != 1 || o["status"] != "draft" || n["status"] != "published" {
		t.Fatalf("kayıt içeriği yanlış: actor=%d old=%s new=%s", actor, oldV, newV)
	}
}

func TestAuditRejectsIncompleteEntry(t *testing.T) {
	env := testdb.New(t)
	if err := audit.Write(context.Background(), env.App.Pool, audit.Entry{Action: "x"}); err == nil {
		t.Fatal("eksik kayıt reddedilmeli")
	}
}
