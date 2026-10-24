package store

import (
	"context"
	"encoding/csv"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/AllenVB/sinavplan/backend/internal/audit"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ImportKind, içe aktarılabilir veri türleri.
var ImportKinds = []string{"students", "courses", "sections", "enrollments"}

// RowError, tek bir CSV satırının hatası (line: dosyadaki satır, başlık = 1).
type RowError struct {
	Line    int    `json:"line"`
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
}

// ImportReport, içe aktarma sonucu. Geçerli satırlar yazılır, geçersizler raporlanır
// (kısmi başarı); aynı dosya tekrar yüklendiğinde created=0 olur (idempotent upsert).
type ImportReport struct {
	Kind      string     `json:"kind"`
	Rows      int        `json:"rows"`      // veri satırı sayısı
	Valid     int        `json:"valid"`     // doğrulamayı geçen
	Created   int        `json:"created"`   // yeni eklenen
	Updated   int        `json:"updated"`   // mevcut, güncellenen (veya değişmeden yeniden yazılan)
	Unchanged int        `json:"unchanged"` // yalnız enrollments: zaten kayıtlı
	Errors    []RowError `json:"errors"`
	ErrorsCut bool       `json:"errors_truncated,omitempty"`
}

const (
	maxImportRows = 200_000
	maxReported   = 200
)

var (
	studentNoRe = regexp.MustCompile(`^[A-Za-z0-9]{3,20}$`)
	codeRe      = regexp.MustCompile(`^[A-Za-z0-9._-]{1,30}$`)
)

type csvTable struct {
	col  map[string]int
	rows [][]string
	line []int
}

// readCSV, başlığı doğrular (zorunlu sütunlar, büyük/küçük harf duyarsız, sıra serbest).
func readCSV(r io.Reader, required []string) (*csvTable, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1 // satır uzunluğunu kendimiz denetleyip satır bazlı raporlarız
	cr.TrimLeadingSpace = true
	head, err := cr.Read()
	if err == io.EOF {
		return nil, fmt.Errorf("dosya boş")
	}
	if err != nil {
		return nil, fmt.Errorf("başlık okunamadı: %v", err)
	}
	t := &csvTable{col: map[string]int{}}
	for i, h := range head {
		h = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(h, "\ufeff"))) // Excel BOM
		if h != "" {
			t.col[h] = i
		}
	}
	var missing []string
	for _, c := range required {
		if _, ok := t.col[c]; !ok {
			missing = append(missing, c)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("zorunlu sütunlar eksik: %s", strings.Join(missing, ", "))
	}
	line := 1
	for {
		rec, err := cr.Read()
		line++
		if err == io.EOF {
			break
		}
		if err != nil {
			t.rows, t.line = append(t.rows, nil), append(t.line, line)
			continue // bozuk satır: nil kayıt, doğrulamada hata olur
		}
		if len(t.rows) >= maxImportRows {
			return nil, fmt.Errorf("en fazla %d satır içe aktarılabilir", maxImportRows)
		}
		t.rows, t.line = append(t.rows, rec), append(t.line, line)
	}
	return t, nil
}

func (t *csvTable) get(i int, name string) string {
	if t.rows[i] == nil {
		return ""
	}
	c := t.col[name]
	if c >= len(t.rows[i]) {
		return ""
	}
	return strings.TrimSpace(t.rows[i][c])
}

type errCollector struct {
	list []RowError
	cut  bool
	bad  map[int]bool
}

func (e *errCollector) add(line int, field, msg string) {
	if e.bad == nil {
		e.bad = map[int]bool{}
	}
	e.bad[line] = true
	if len(e.list) >= maxReported {
		e.cut = true
		return
	}
	e.list = append(e.list, RowError{line, field, msg})
}

func parseBool(s string) (bool, bool) {
	switch strings.ToLower(s) {
	case "", "0", "false", "no", "hayır", "hayir", "h":
		return false, true
	case "1", "true", "yes", "evet", "e":
		return true, true
	}
	return false, false
}

// Import, CSV'yi doğrular ve geçerli satırları TEK transaction'da COPY + upsert ile yazar.
func Import(ctx context.Context, pool *pgxpool.Pool, kind string, r io.Reader, actor int64) (ImportReport, error) {
	required := map[string][]string{
		"students":    {"student_no", "full_name", "department_code"},
		"courses":     {"code", "name", "department_code", "year"},
		"sections":    {"course_code", "section_code", "lecturer_email"},
		"enrollments": {"student_no", "course_code", "section_code"},
	}[kind]
	if required == nil {
		return ImportReport{}, fmt.Errorf("bilinmeyen tür %q", kind)
	}
	t, err := readCSV(r, required)
	if err != nil {
		return ImportReport{}, err
	}
	rep := ImportReport{Kind: kind, Rows: len(t.rows), Errors: []RowError{}}
	ec := &errCollector{}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return rep, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	lookup := func(q string) (map[string]int64, error) {
		rows, err := tx.Query(ctx, q)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		m := map[string]int64{}
		for rows.Next() {
			var k string
			var v int64
			if err := rows.Scan(&k, &v); err != nil {
				return nil, err
			}
			m[k] = v
		}
		return m, rows.Err()
	}

	var valid [][]any // satıra göre COPY değerleri
	seen := map[string]int{}
	dup := func(i int, key string) bool {
		if first, ok := seen[key]; ok {
			ec.add(t.line[i], "", fmt.Sprintf("satır %d ile aynı kayıt (dosyada tekrar)", first))
			return true
		}
		seen[key] = t.line[i]
		return false
	}
	var table string
	var cols []string
	var upsert string

	switch kind {
	case "students":
		depts, err := lookup(`SELECT code, id FROM departments`)
		if err != nil {
			return rep, err
		}
		for i := range t.rows {
			if t.rows[i] == nil {
				ec.add(t.line[i], "", "satır okunamadı (bozuk CSV)")
				continue
			}
			no, name, dc := t.get(i, "student_no"), t.get(i, "full_name"), t.get(i, "department_code")
			ok := true
			if !studentNoRe.MatchString(no) {
				ec.add(t.line[i], "student_no", "3-20 harf/rakam olmalı")
				ok = false
			}
			if name == "" || len(name) > 120 {
				ec.add(t.line[i], "full_name", "boş olamaz, en fazla 120 karakter")
				ok = false
			}
			did, dok := depts[dc]
			if !dok {
				ec.add(t.line[i], "department_code", fmt.Sprintf("bilinmeyen bölüm %q", dc))
				ok = false
			}
			dis := false
			if c, has := t.col["disability"]; has && c >= 0 {
				var pok bool
				if dis, pok = parseBool(t.get(i, "disability")); !pok {
					ec.add(t.line[i], "disability", "true/false olmalı")
					ok = false
				}
			}
			if ok && !dup(i, no) {
				valid = append(valid, []any{no, name, did, dis})
			}
		}
		table, cols = "tmp_students", []string{"student_no", "full_name", "department_id", "disability_flag"}
		_, err = tx.Exec(ctx, `CREATE TEMP TABLE tmp_students (student_no text, full_name text, department_id bigint, disability_flag boolean) ON COMMIT DROP`)
		upsert = `INSERT INTO students (student_no, full_name, department_id, disability_flag)
			SELECT student_no, full_name, department_id, disability_flag FROM tmp_students
			ON CONFLICT (student_no) DO UPDATE SET full_name = EXCLUDED.full_name, department_id = EXCLUDED.department_id,
				disability_flag = EXCLUDED.disability_flag RETURNING (xmax = 0)`
	case "courses":
		depts, err2 := lookup(`SELECT code, id FROM departments`)
		if err2 != nil {
			return rep, err2
		}
		for i := range t.rows {
			if t.rows[i] == nil {
				ec.add(t.line[i], "", "satır okunamadı (bozuk CSV)")
				continue
			}
			code, name, dc := t.get(i, "code"), t.get(i, "name"), t.get(i, "department_code")
			year, yerr := strconv.Atoi(t.get(i, "year"))
			ok := true
			if !codeRe.MatchString(code) {
				ec.add(t.line[i], "code", "geçersiz ders kodu")
				ok = false
			}
			if name == "" || len(name) > 200 {
				ec.add(t.line[i], "name", "boş olamaz, en fazla 200 karakter")
				ok = false
			}
			did, dok := depts[dc]
			if !dok {
				ec.add(t.line[i], "department_code", fmt.Sprintf("bilinmeyen bölüm %q", dc))
				ok = false
			}
			if yerr != nil || year < 1 || year > 6 {
				ec.add(t.line[i], "year", "1 ile 6 arasında olmalı")
				ok = false
			}
			lab := false
			if _, has := t.col["requires_lab"]; has {
				var pok bool
				if lab, pok = parseBool(t.get(i, "requires_lab")); !pok {
					ec.add(t.line[i], "requires_lab", "true/false olmalı")
					ok = false
				}
			}
			if ok && !dup(i, code) {
				valid = append(valid, []any{code, name, did, int16(year), lab})
			}
		}
		table, cols = "tmp_courses", []string{"code", "name", "department_id", "year", "requires_lab"}
		_, err = tx.Exec(ctx, `CREATE TEMP TABLE tmp_courses (code text, name text, department_id bigint, year smallint, requires_lab boolean) ON COMMIT DROP`)
		upsert = `INSERT INTO courses (code, name, department_id, year, requires_lab)
			SELECT code, name, department_id, year, requires_lab FROM tmp_courses
			ON CONFLICT (code) DO UPDATE SET name = EXCLUDED.name, department_id = EXCLUDED.department_id,
				year = EXCLUDED.year, requires_lab = EXCLUDED.requires_lab RETURNING (xmax = 0)`
	case "sections":
		courses, err2 := lookup(`SELECT code, id FROM courses`)
		if err2 != nil {
			return rep, err2
		}
		lecs, err2 := lookup(`SELECT u.email, l.id FROM lecturers l JOIN users u ON u.id = l.user_id`)
		if err2 != nil {
			return rep, err2
		}
		for i := range t.rows {
			if t.rows[i] == nil {
				ec.add(t.line[i], "", "satır okunamadı (bozuk CSV)")
				continue
			}
			cc, sc, em := t.get(i, "course_code"), t.get(i, "section_code"), strings.ToLower(t.get(i, "lecturer_email"))
			ok := true
			cid, cok := courses[cc]
			if !cok {
				ec.add(t.line[i], "course_code", fmt.Sprintf("bilinmeyen ders %q", cc))
				ok = false
			}
			if sc == "" || len(sc) > 10 {
				ec.add(t.line[i], "section_code", "boş olamaz, en fazla 10 karakter")
				ok = false
			}
			lid, lok := lecs[em]
			if !lok {
				ec.add(t.line[i], "lecturer_email", fmt.Sprintf("öğretim üyesi bulunamadı %q", em))
				ok = false
			}
			if ok && !dup(i, cc+"|"+sc) {
				valid = append(valid, []any{cid, sc, lid})
			}
		}
		table, cols = "tmp_sections", []string{"course_id", "code", "lecturer_id"}
		_, err = tx.Exec(ctx, `CREATE TEMP TABLE tmp_sections (course_id bigint, code text, lecturer_id bigint) ON COMMIT DROP`)
		upsert = `INSERT INTO sections (course_id, code, lecturer_id) SELECT course_id, code, lecturer_id FROM tmp_sections
			ON CONFLICT (course_id, code) DO UPDATE SET lecturer_id = EXCLUDED.lecturer_id RETURNING (xmax = 0)`
	case "enrollments":
		studs, err2 := lookup(`SELECT student_no, id FROM students`)
		if err2 != nil {
			return rep, err2
		}
		secs, err2 := lookup(`SELECT c.code || '|' || s.code, s.id FROM sections s JOIN courses c ON c.id = s.course_id`)
		if err2 != nil {
			return rep, err2
		}
		for i := range t.rows {
			if t.rows[i] == nil {
				ec.add(t.line[i], "", "satır okunamadı (bozuk CSV)")
				continue
			}
			no, cc, sc := t.get(i, "student_no"), t.get(i, "course_code"), t.get(i, "section_code")
			ok := true
			sid, sok := studs[no]
			if !sok {
				ec.add(t.line[i], "student_no", fmt.Sprintf("bilinmeyen öğrenci %q", no))
				ok = false
			}
			secID, cok := secs[cc+"|"+sc]
			if !cok {
				ec.add(t.line[i], "section_code", fmt.Sprintf("bilinmeyen şube %s/%s", cc, sc))
				ok = false
			}
			if ok && !dup(i, no+"|"+cc+"|"+sc) {
				valid = append(valid, []any{sid, secID})
			}
		}
		table, cols = "tmp_enrollments", []string{"student_id", "section_id"}
		_, err = tx.Exec(ctx, `CREATE TEMP TABLE tmp_enrollments (student_id bigint, section_id bigint) ON COMMIT DROP`)
		upsert = `INSERT INTO enrollments (student_id, section_id) SELECT student_id, section_id FROM tmp_enrollments
			ON CONFLICT DO NOTHING RETURNING true`
	}
	if err != nil {
		return rep, err
	}
	rep.Valid = len(valid)

	if len(valid) > 0 {
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{table}, cols, pgx.CopyFromRows(valid)); err != nil {
			return rep, fmt.Errorf("toplu yazım (COPY) başarısız: %w", err)
		}
		rows, err := tx.Query(ctx, upsert)
		if err != nil {
			return rep, fmt.Errorf("upsert başarısız: %w", err)
		}
		for rows.Next() {
			var inserted bool
			if err := rows.Scan(&inserted); err != nil {
				rows.Close()
				return rep, err
			}
			if inserted {
				rep.Created++
			} else {
				rep.Updated++
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return rep, err
		}
		if kind == "enrollments" {
			rep.Unchanged, rep.Updated = rep.Valid-rep.Created, 0
		}
	}
	rep.Errors, rep.ErrorsCut = ec.list, ec.cut
	if rep.Errors == nil {
		rep.Errors = []RowError{}
	}
	if err := audit.Write(ctx, tx, audit.Entry{ActorID: actor, Action: "import." + kind, Entity: "import", EntityID: kind,
		New: map[string]any{"rows": rep.Rows, "valid": rep.Valid, "created": rep.Created, "updated": rep.Updated, "errors": len(ec.bad)}}); err != nil {
		return rep, err
	}
	return rep, tx.Commit(ctx)
}
