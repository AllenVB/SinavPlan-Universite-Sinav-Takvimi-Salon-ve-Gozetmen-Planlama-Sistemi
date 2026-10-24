package httpapi

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestDBUnavailableIsMappedTo503(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"ağ hatası", &net.OpError{Op: "dial", Err: errors.New("connection refused")}, 503},
		{"bağlantı sınıfı sqlstate", &pgconn.PgError{Code: "08006"}, 503},
		{"sunucu kapanıyor", fmt.Errorf("sorgu: %w", &pgconn.PgError{Code: "57P01"}), 503},
		{"mantık hatası", &pgconn.PgError{Code: "23505"}, 500},
		{"bilinmeyen", errors.New("beklenmedik"), 500},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		WriteError(rec, httptest.NewRequest(http.MethodGet, "/x", nil), c.err)
		if rec.Code != c.want {
			t.Errorf("%s: %d, beklenen %d", c.name, rec.Code, c.want)
		}
	}
}
