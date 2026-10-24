// Package export, dışa aktarma biçimlerini üretir: .xlsx (bağımlılıksız, en küçük geçerli
// OOXML), CSV (Excel'in Türkçe karakterleri doğru açması için UTF-8 BOM'lu) ve iCalendar.
package export

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
)

// Sheet, bir çalışma sayfası: ilk satır başlıktır (kalın yazılır). Hücre değerleri
// string, tamsayı veya kayan nokta olabilir.
type Sheet struct {
	Name string
	Rows [][]any
}

// xml 1.0'da geçersiz denetim karakterlerini atar (hücre metni dosyayı bozmasın).
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' || r >= 0x20 && r != 0xFFFE && r != 0xFFFF {
			return r
		}
		return -1
	}, s)
}

func esc(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(clean(s)))
	return b.String()
}

// colName: 0 -> A, 25 -> Z, 26 -> AA.
func colName(i int) string {
	s := ""
	for i++; i > 0; i = (i - 1) / 26 {
		s = string(rune('A'+(i-1)%26)) + s
	}
	return s
}

// sheetName, Excel kurallarına uydurur (≤31 karakter, \ / ? * [ ] : yasak).
func sheetName(n string, i int) string {
	n = strings.Map(func(r rune) rune {
		if strings.ContainsRune(`\/?*[]:`, r) {
			return '_'
		}
		return r
	}, clean(n))
	if r := []rune(n); len(r) > 31 {
		n = string(r[:31])
	}
	if n == "" {
		n = fmt.Sprintf("Sayfa%d", i+1)
	}
	return n
}

// WriteXLSX, sayfaları geçerli bir .xlsx olarak yazar.
func WriteXLSX(w io.Writer, sheets []Sheet) error {
	if len(sheets) == 0 {
		return fmt.Errorf("en az bir sayfa gerekli")
	}
	zw := zip.NewWriter(w)
	put := func(name, body string) error {
		f, err := zw.Create(name)
		if err != nil {
			return err
		}
		_, err = io.WriteString(f, body)
		return err
	}
	const hdr = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"

	var ct, wb, rels strings.Builder
	ct.WriteString(hdr + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/>`)
	wb.WriteString(hdr + `<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets>`)
	rels.WriteString(hdr + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">`)
	seen := map[string]bool{}
	for i, sh := range sheets {
		name := sheetName(sh.Name, i)
		for base, n := name, 2; seen[strings.ToLower(name)]; n++ { // sayfa adları benzersiz olmalı
			name = fmt.Sprintf("%s (%d)", string([]rune(base)[:min(len([]rune(base)), 27)]), n)
		}
		seen[strings.ToLower(name)] = true
		fmt.Fprintf(&ct, `<Override PartName="/xl/worksheets/sheet%d.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/>`, i+1)
		fmt.Fprintf(&wb, `<sheet name="%s" sheetId="%d" r:id="rId%d"/>`, esc(name), i+1, i+1)
		fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet%d.xml"/>`, i+1, i+1)

		var sb strings.Builder
		sb.WriteString(hdr + `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetViews><sheetView workbookViewId="0"><pane ySplit="1" topLeftCell="A2" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews><sheetData>`)
		for r, row := range sh.Rows {
			fmt.Fprintf(&sb, `<row r="%d">`, r+1)
			for c, v := range row {
				ref := fmt.Sprintf("%s%d", colName(c), r+1)
				style := ""
				if r == 0 {
					style = ` s="1"`
				}
				switch x := v.(type) {
				case int:
					fmt.Fprintf(&sb, `<c r="%s"%s><v>%d</v></c>`, ref, style, x)
				case int64:
					fmt.Fprintf(&sb, `<c r="%s"%s><v>%d</v></c>`, ref, style, x)
				case float64:
					fmt.Fprintf(&sb, `<c r="%s"%s><v>%g</v></c>`, ref, style, x)
				case nil:
				default:
					fmt.Fprintf(&sb, `<c r="%s"%s t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, ref, style, esc(fmt.Sprint(x)))
				}
			}
			sb.WriteString(`</row>`)
		}
		sb.WriteString(`</sheetData></worksheet>`)
		if err := put(fmt.Sprintf("xl/worksheets/sheet%d.xml", i+1), sb.String()); err != nil {
			return err
		}
	}
	ct.WriteString(`</Types>`)
	wb.WriteString(`</sheets></workbook>`)
	fmt.Fprintf(&rels, `<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`, len(sheets)+1)

	parts := []struct{ name, body string }{
		{"[Content_Types].xml", ct.String()},
		{"_rels/.rels", hdr + `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`},
		{"xl/workbook.xml", wb.String()},
		{"xl/_rels/workbook.xml.rels", rels.String()},
		{"xl/styles.xml", hdr + `<styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><fonts count="2"><font><sz val="11"/><name val="Calibri"/></font><font><b/><sz val="11"/><name val="Calibri"/></font></fonts><fills count="2"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill></fills><borders count="1"><border><left/><right/><top/><bottom/><diagonal/></border></borders><cellStyleXfs count="1"><xf numFmtId="0" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="2"><xf numFmtId="0" fontId="0" fillId="0" borderId="0" xfId="0"/><xf numFmtId="0" fontId="1" fillId="0" borderId="0" xfId="0" applyFont="1"/></cellXfs></styleSheet>`},
	}
	for _, p := range parts {
		if err := put(p.name, p.body); err != nil {
			return err
		}
	}
	return zw.Close()
}

// WriteCSV, ilk sayfayı UTF-8 BOM'lu, noktalı virgül ayraçlı (Türkçe Excel) CSV olarak yazar.
func WriteCSV(w io.Writer, rows [][]any) error {
	if _, err := w.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return err
	}
	cw := csv.NewWriter(w)
	cw.Comma = ';'
	for _, r := range rows {
		rec := make([]string, len(r))
		for i, v := range r {
			s := ""
			if v != nil {
				s = clean(fmt.Sprint(v))
			}
			// CSV enjeksiyonu: =,+,-,@ ile başlayan metin Excel'de formül olarak çalışmasın.
			if _, isStr := v.(string); isStr && s != "" && strings.ContainsRune("=+-@", rune(s[0])) {
				s = "'" + s
			}
			rec[i] = s
		}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}
