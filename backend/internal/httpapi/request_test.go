package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParsePageDefaultsAndBounds(t *testing.T) {
	p, err := parsePage(httptest.NewRequest(http.MethodGet, "/x", nil))
	if err != nil || p.Limit != defaultLimit || p.After != 0 {
		t.Fatalf("varsayılan: %+v %v", p, err)
	}
	for _, q := range []string{"limit=0", "limit=201", "limit=-1", "limit=abc", "after=!!!", "after=" + "dg"} {
		_, err := parsePage(httptest.NewRequest(http.MethodGet, "/x?"+q, nil))
		ae, ok := err.(*APIError)
		if !ok || ae.Status != http.StatusUnprocessableEntity {
			t.Errorf("%q 422 vermeliydi: %v", q, err)
		}
	}
}

func TestCursorRoundTrip(t *testing.T) {
	for _, id := range []int64{0, 1, 8000, 1 << 40} {
		got, err := decodeCursor(encodeCursor(id))
		if err != nil || got != id {
			t.Errorf("imleç %d -> %d (%v)", id, got, err)
		}
	}
	// v1 öneki olmayan (istemcinin uydurduğu) imleç reddedilir.
	if _, err := decodeCursor("MTIz"); err == nil { // "123"
		t.Error("önekli olmayan imleç reddedilmeli")
	}
}

func TestPaginateSetsNextCursorOnlyWhenMoreRows(t *testing.T) {
	key := func(v int64) int64 { return v }
	full := paginate([]int64{1, 2, 3}, 2, key) // limit+1 satır geldi -> sonraki sayfa var
	if len(full.Items) != 2 || full.NextCursor == nil {
		t.Fatalf("sonraki sayfa imleci bekleniyordu: %+v", full)
	}
	if id, _ := decodeCursor(*full.NextCursor); id != 2 {
		t.Fatalf("imleç son gösterilen öğe olmalı (2), %d", id)
	}
	last := paginate([]int64{4}, 2, key)
	if len(last.Items) != 1 || last.NextCursor != nil {
		t.Fatalf("son sayfada imleç olmamalı: %+v", last)
	}
	empty := paginate([]int64(nil), 2, key)
	if empty.Items == nil {
		t.Fatal("boş sayfa null değil [] olmalı")
	}
}

func TestDecodeJSONStrictness(t *testing.T) {
	type body struct {
		Email string `json:"email"`
	}
	cases := map[string]int{
		``:                           http.StatusBadRequest,
		`{`:                          http.StatusBadRequest,
		`{"email":"a","extra":1}`:    http.StatusBadRequest,
		`{"email":1}`:                http.StatusUnprocessableEntity,
		`{"email":"a"}{"email":"b"}`: http.StatusBadRequest,
	}
	for in, want := range cases {
		var b body
		err := decodeJSON(httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(in)), &b)
		ae, ok := err.(*APIError)
		if !ok || ae.Status != want {
			t.Errorf("%q: %v, beklenen %d", in, err, want)
		}
	}
	var b body
	if err := decodeJSON(httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"email":"a"}`)), &b); err != nil || b.Email != "a" {
		t.Fatalf("geçerli gövde: %v %+v", err, b)
	}
}
