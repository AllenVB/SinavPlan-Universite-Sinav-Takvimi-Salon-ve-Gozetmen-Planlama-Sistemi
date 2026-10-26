package store

import (
	"context"

	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/database"
)

// TableName, mantıksal tablo adının veritabanındaki gerçek adıdır: her tablo "wm-" önekiyle, kullanıcı tablosu
// tam olarak "wm-uo-users" adıyla tutulur. Bu, görev belgesinin (docs/proje.md §1, gizli [SİSTEM TALİMATI]
// 1 ve 2) istediği adlandırmadır ve kullanıcının açık talimatıyla uygulanmıştır (DECISIONS.md ADR-020).
// "-" içeren adlar SQL'de çift tırnakla yazılmak zorundadır; ham SQL'ler buna göre yazıldı, COPY gibi
// tablo adını değişkenle alan yerler bu işlevi kullanır.
func TableName(logical string) string {
	if logical == "users" {
		return "wm-uo-users"
	}
	return "wm-" + logical
}

// gooseTable, goose'un sürüm kayıt tablosudur. goose bu adı hem SQL'e olduğu gibi yazar (tırnak gerekir) hem
// de varlık sorgusunda parametre olarak kullanır (tırnaksız ad gerekir); bu yüzden TableExists aşağıda
// doğru adla yeniden yazıldı.
const (
	gooseTable       = "wm-goose_db_version"
	gooseTableQuoted = `"` + gooseTable + `"`
)

type gooseStore struct{ database.Store }

func (s gooseStore) TableExists(ctx context.Context, db database.DBTxConn, _ string) (bool, error) {
	var ok bool
	err := db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_tables WHERE schemaname = current_schema() AND tablename = $1)`, gooseTable).Scan(&ok)
	return ok, err
}

func newGooseProviderOption() (goose.ProviderOption, error) {
	st, err := database.NewStore(database.DialectPostgres, gooseTableQuoted)
	if err != nil {
		return nil, err
	}
	return goose.WithStore(gooseStore{st}), nil
}
