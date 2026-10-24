// Package migrations, goose SQL migrasyonlarını ikiliye gömer.
package migrations

import "embed"

// FS, *.sql migrasyon dosyalarını taşır.
//
//go:embed *.sql
var FS embed.FS
