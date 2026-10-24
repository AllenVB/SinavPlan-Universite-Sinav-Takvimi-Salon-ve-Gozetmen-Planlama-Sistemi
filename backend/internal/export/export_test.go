package export

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"regexp"
	"strings"
	"testing"
	"time"
)

func readZip(t *testing.T, b []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatalf("geçerli zip değil: %v", err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		body, _ := io.ReadAll(rc)
		rc.Close()
		out[f.Name] = string(body)
	}
	return out
}

func TestXLSXIsWellFormedAndCarriesValues(t *testing.T) {
	var buf bytes.Buffer
	err := WriteXLSX(&buf, []Sheet{
		{Name: "Takvim", Rows: [][]any{{"Ders", "Öğrenci"}, {"BLM <101> & \"x\"", 42}, {"ğüşiöç\x01", nil, 1.5}}},
		{Name: "Takvim", Rows: [][]any{{"a"}}}, // yinelenen ad
		{Name: "Çok/uzun:ad?*[]" + strings.Repeat("x", 40), Rows: [][]any{{"b"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	files := readZip(t, buf.Bytes())
	for name, body := range files {
		var v struct{}
		if err := xml.Unmarshal([]byte(body), &v); err != nil {
			t.Errorf("%s iyi biçimli XML değil: %v", name, err)
		}
	}
	s1 := files["xl/worksheets/sheet1.xml"]
	for _, want := range []string{"BLM &lt;101&gt; &amp; &#34;x&#34;", "<v>42</v>", "ğüşiöç", "<v>1.5</v>", `s="1"`} {
		if !strings.Contains(s1, want) {
			t.Errorf("sheet1 %q içermiyor", want)
		}
	}
	if strings.Contains(s1, "\x01") {
		t.Error("geçersiz denetim karakteri yazıldı")
	}
	wb := files["xl/workbook.xml"]
	if strings.Count(wb, "<sheet ") != 3 || strings.Contains(wb, `name="Takvim" sheetId="2"`) {
		t.Errorf("sayfa adları benzersiz/geçerli değil: %s", wb)
	}
	for _, m := range regexp.MustCompile(`<sheet name="([^"]*)"`).FindAllStringSubmatch(wb, -1) {
		if strings.ContainsAny(m[1], `\/?*[]:`) || len([]rune(m[1])) > 31 {
			t.Errorf("geçersiz sayfa adı: %q", m[1])
		}
	}
	if err := WriteXLSX(&buf, nil); err == nil {
		t.Error("sayfasız çalışma kitabı reddedilmeli")
	}
}

func TestColName(t *testing.T) {
	for i, want := range map[int]string{0: "A", 25: "Z", 26: "AA", 27: "AB", 701: "ZZ", 702: "AAA"} {
		if got := colName(i); got != want {
			t.Errorf("colName(%d)=%s, beklenen %s", i, got, want)
		}
	}
}

func TestCSVHasBOMSemicolonAndNeutralisesFormulas(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteCSV(&buf, [][]any{{"Ad", "Not"}, {"=HYPERLINK(\"x\")", 3}, {"a;b", "-5"}}); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	if !bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}) {
		t.Error("UTF-8 BOM yok")
	}
	s := string(b[3:])
	if !strings.Contains(s, "'=HYPERLINK") || !strings.Contains(s, `"a;b";'-5`) {
		t.Errorf("formül/ayraç işlenmedi: %q", s)
	}
}

func TestICSFormat(t *testing.T) {
	var buf bytes.Buffer
	start := time.Date(2027, 1, 11, 6, 0, 0, 0, time.UTC)
	err := WriteICS(&buf, "Sınavlarım", []Event{{UID: "exam-1@sinavplan", Start: start, End: start.Add(90 * time.Minute),
		Summary: "BLM101, Veri; Yapıları", Location: "D-01 (A Blok)", Desc: "satır1\nsatır2 " + strings.Repeat("uzun", 40)}}, start)
	if err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	for _, want := range []string{"BEGIN:VCALENDAR\r\n", "UID:exam-1@sinavplan\r\n", "DTSTART:20270111T060000Z\r\n", "DTEND:20270111T073000Z\r\n",
		`SUMMARY:BLM101\, Veri\; Yapıları`, `\n`, "END:VCALENDAR\r\n"} {
		if !strings.Contains(s, want) {
			t.Errorf("ics %q içermiyor:\n%s", want, s)
		}
	}
	for _, l := range strings.Split(strings.TrimSuffix(s, "\r\n"), "\r\n") {
		if len(l) > 75 {
			t.Errorf("satır 75 oktetten uzun (%d): %q", len(l), l)
		}
	}
	if strings.Contains(strings.ReplaceAll(s, "\r\n", ""), "\n") {
		t.Error("çıplak LF var (yalnız CRLF olmalı)")
	}
}
