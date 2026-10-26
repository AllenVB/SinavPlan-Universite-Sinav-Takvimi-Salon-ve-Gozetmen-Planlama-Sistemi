package store

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// FacultyDepartment, bir bölümün özeti: ekranda "hangi bölümler var" sorusunu yanıtlar.
type FacultyDepartment struct {
	ID       int64  `json:"id"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	Students int    `json:"students"`
	Courses  int    `json:"courses"`
	Years    []int  `json:"years"`
}

// FacultyLecturer, fakültenin öğretim üyesi.
type FacultyLecturer struct {
	ID       int64  `json:"id"`
	FullName string `json:"full_name"`
}

// FacultyInfo, fakülte → bölümler → öğretim üyeleri.
type FacultyInfo struct {
	ID          int64               `json:"id"`
	Code        string              `json:"code"`
	Name        string              `json:"name"`
	Departments []FacultyDepartment `json:"departments"`
	Lecturers   []FacultyLecturer   `json:"lecturers"`
}

// Faculties, tüm fakülteleri bölümleri ve öğretim üyeleriyle döner (sayılar tek sorguda).
func Faculties(ctx context.Context, pool *pgxpool.Pool) ([]FacultyInfo, error) {
	out := []FacultyInfo{}
	idx := map[int64]int{}
	rows, err := pool.Query(ctx, `SELECT id, code, name FROM "wm-faculties" ORDER BY id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var f FacultyInfo
		if err := rows.Scan(&f.ID, &f.Code, &f.Name); err != nil {
			rows.Close()
			return nil, err
		}
		f.Departments, f.Lecturers = []FacultyDepartment{}, []FacultyLecturer{}
		idx[f.ID] = len(out)
		out = append(out, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	drows, err := pool.Query(ctx, `
		SELECT d.faculty_id, d.id, d.code, d.name,
		       (SELECT count(*) FROM "wm-students" s WHERE s.department_id = d.id)::int,
		       (SELECT count(*) FROM "wm-courses" c WHERE c.department_id = d.id)::int,
		       coalesce((SELECT array_agg(DISTINCT c.year::int ORDER BY c.year::int) FROM "wm-courses" c WHERE c.department_id = d.id), '{}')
		FROM "wm-departments" d WHERE d.faculty_id IS NOT NULL ORDER BY d.id`)
	if err != nil {
		return nil, err
	}
	defer drows.Close()
	for drows.Next() {
		var fid int64
		var d FacultyDepartment
		if err := drows.Scan(&fid, &d.ID, &d.Code, &d.Name, &d.Students, &d.Courses, &d.Years); err != nil {
			return nil, err
		}
		if i, ok := idx[fid]; ok {
			out[i].Departments = append(out[i].Departments, d)
		}
	}
	if err := drows.Err(); err != nil {
		return nil, err
	}

	lrows, err := pool.Query(ctx, `
		SELECT d.faculty_id, l.id, u.full_name FROM "wm-lecturers" l JOIN "wm-uo-users" u ON u.id = l.user_id
		JOIN "wm-departments" d ON d.id = l.department_id WHERE d.faculty_id IS NOT NULL ORDER BY l.id`)
	if err != nil {
		return nil, err
	}
	defer lrows.Close()
	for lrows.Next() {
		var fid int64
		var l FacultyLecturer
		if err := lrows.Scan(&fid, &l.ID, &l.FullName); err != nil {
			return nil, err
		}
		if i, ok := idx[fid]; ok {
			out[i].Lecturers = append(out[i].Lecturers, l)
		}
	}
	return out, lrows.Err()
}
