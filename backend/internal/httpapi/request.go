package httpapi

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
)

const maxJSONBody = 1 << 20

// decodeJSON, gövdeyi sıkı biçimde çözer: bilinmeyen alan ve çoklu JSON değeri
// reddedilir. Biçim hatası 400; anlam doğrulaması çağıranın işidir (422).
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxJSONBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var syn *json.SyntaxError
		var typ *json.UnmarshalTypeError
		switch {
		case errors.Is(err, io.EOF):
			return BadRequest("istek gövdesi boş")
		case errors.As(err, &syn):
			return BadRequest("geçersiz JSON")
		case errors.As(err, &typ):
			return Validation(fmt.Sprintf("%q alanı yanlış tipte", typ.Field), map[string]string{typ.Field: "yanlış tip"})
		case strings.HasPrefix(err.Error(), "json: unknown field"):
			return BadRequest("bilinmeyen alan: " + strings.TrimPrefix(err.Error(), "json: unknown field "))
		default:
			return BadRequest("istek gövdesi çözümlenemedi")
		}
	}
	if dec.More() {
		return BadRequest("gövdede tek bir JSON nesnesi olmalı")
	}
	return nil
}

func pathInt64(r *http.Request, name string) (int64, error) {
	v := chiURLParam(r, name)
	id, err := strconv.ParseInt(v, 10, 64)
	if err != nil || id <= 0 {
		return 0, Validation(fmt.Sprintf("%s pozitif bir tam sayı olmalı", name), map[string]string{name: "geçersiz"})
	}
	return id, nil
}

// --- Keyset sayfalama ---------------------------------------------------------

const (
	defaultLimit = 50
	maxLimit     = 200
)

// Page, ?limit=&after= parametreleri. After, son görülen kaydın anahtarıdır;
// OFFSET kullanılmaz, bu yüzden derin sayfalar da dizinden tek aramayla gelir.
type Page struct {
	Limit int
	After int64
}

// PageResponse, sayfalı yanıt zarfı.
type PageResponse[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

// parsePage, sorgu parametrelerini doğrular.
func parsePage(r *http.Request) (Page, error) {
	p := Page{Limit: defaultLimit}
	q := r.URL.Query()
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxLimit {
			return Page{}, Validation(fmt.Sprintf("limit 1 ile %d arasında olmalı", maxLimit),
				map[string]string{"limit": "geçersiz"})
		}
		p.Limit = n
	}
	if v := q.Get("after"); v != "" {
		id, err := decodeCursor(v)
		if err != nil {
			return Page{}, Validation("after imleci geçersiz", map[string]string{"after": "geçersiz"})
		}
		p.After = id
	}
	return p, nil
}

// İmleç opaktır (istemci içini yorumlamamalı); sürüm öneki biçimi ileride
// değiştirmeye izin verir.
func encodeCursor(id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte("v1:" + strconv.FormatInt(id, 10)))
}

func decodeCursor(s string) (int64, error) {
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return 0, err
	}
	rest, ok := strings.CutPrefix(string(b), "v1:")
	if !ok {
		return 0, errors.New("bilinmeyen imleç sürümü")
	}
	id, err := strconv.ParseInt(rest, 10, 64)
	if err != nil || id < 0 {
		return 0, errors.New("geçersiz imleç")
	}
	return id, nil
}

// paginate, limit+1 satırla çağrılan sorgunun sonucunu sayfaya çevirir.
func paginate[T any](rows []T, limit int, key func(T) int64) PageResponse[T] {
	resp := PageResponse[T]{Items: rows}
	if len(rows) > limit {
		resp.Items = rows[:limit]
		c := encodeCursor(key(rows[limit-1]))
		resp.NextCursor = &c
	}
	if resp.Items == nil {
		resp.Items = []T{}
	}
	return resp
}
