package store

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/AllenVB/sinavplan/backend/internal/auth"
	"github.com/AllenVB/sinavplan/backend/internal/simdata"
	"github.com/jackc/pgx/v5"
)

// identityTables, kimlikleri açıkça yazılan tablolar. Yazımdan sonra dizileri
// en büyük kimliğe ayarlıyoruz; aksi halde API'nin ilk INSERT'ü çakışır.
var identityTables = []string{
	"departments", "users", "lecturers", "courses", "sections", "students",
	"rooms", "room_seats", "exam_periods", "slots", "exams",
}

// Seed, veri kümesini tek transaction içinde yazar. Aynı tohum ve aynı
// veri zaten yazılmışsa hiçbir şey yapmaz (compose her `up`'ta seed'i yeniden
// çalıştırabilir); farklı bir tohumla yazılmışsa hata verir.
func Seed(ctx context.Context, conn *pgx.Conn, ds *simdata.Dataset, demoPassword string, log *slog.Logger) error {
	hash := ds.Hash()

	var seededSeed int64
	var seededHash string
	err := conn.QueryRow(ctx, `SELECT seed, dataset_hash FROM seed_meta`).Scan(&seededSeed, &seededHash)
	switch {
	case err == nil && seededHash == hash:
		log.Info("veri zaten yüklü, atlanıyor", slog.Int64("seed", seededSeed), slog.String("hash", hash[:12]))
		return nil
	case err == nil:
		return fmt.Errorf("veritabanında farklı bir veri kümesi var (seed=%d, hash=%s…); "+
			"sıfırlamak için `make reset-demo`", seededSeed, seededHash[:12])
	case err != pgx.ErrNoRows:
		return fmt.Errorf("seed_meta okunamadı: %w", err)
	}

	pwHash, err := auth.HashPassword(demoPassword)
	if err != nil {
		return err
	}

	start := time.Now()
	tx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	copyRows := func(table string, cols []string, n int, row func(i int) []any) error {
		_, err := tx.CopyFrom(ctx, pgx.Identifier{table}, cols, pgx.CopyFromSlice(n, func(i int) ([]any, error) {
			return row(i), nil
		}))
		if err != nil {
			return fmt.Errorf("%s yazılamadı: %w", table, err)
		}
		log.Info("yazıldı", slog.String("tablo", table), slog.Int("satır", n))
		return nil
	}

	steps := []func() error{
		func() error {
			return copyRows("departments", []string{"id", "code", "name"}, len(ds.Departments),
				func(i int) []any { x := ds.Departments[i]; return []any{x.ID, x.Code, x.Name} })
		},
		func() error {
			return copyRows("users", []string{"id", "email", "password_hash", "full_name"}, len(ds.Users),
				func(i int) []any { x := ds.Users[i]; return []any{x.ID, x.Email, pwHash, x.FullName} })
		},
		func() error {
			n := 0
			for _, u := range ds.Users {
				n += len(u.Roles)
			}
			flat := make([][2]any, 0, n)
			for _, u := range ds.Users {
				for _, r := range u.Roles {
					flat = append(flat, [2]any{u.ID, r})
				}
			}
			return copyRows("user_roles", []string{"user_id", "role"}, len(flat),
				func(i int) []any { return []any{flat[i][0], flat[i][1]} })
		},
		func() error {
			return copyRows("lecturers", []string{"id", "user_id", "department_id"}, len(ds.Lecturers),
				func(i int) []any { x := ds.Lecturers[i]; return []any{x.ID, x.UserID, x.DepartmentID} })
		},
		func() error {
			return copyRows("courses", []string{"id", "department_id", "code", "name", "year", "requires_lab"}, len(ds.Courses),
				func(i int) []any {
					x := ds.Courses[i]
					return []any{x.ID, x.DepartmentID, x.Code, x.Name, int16(x.Year), x.RequiresLab}
				})
		},
		func() error {
			return copyRows("sections", []string{"id", "course_id", "code", "lecturer_id"}, len(ds.Sections),
				func(i int) []any { x := ds.Sections[i]; return []any{x.ID, x.CourseID, x.Code, x.LecturerID} })
		},
		func() error {
			return copyRows("students", []string{"id", "user_id", "student_no", "full_name", "department_id", "disability_flag"},
				len(ds.Students), func(i int) []any {
					x := ds.Students[i]
					var uid any
					if x.UserID != 0 {
						uid = x.UserID
					}
					return []any{x.ID, uid, x.No, x.FullName, x.DepartmentID, x.Disabled}
				})
		},
		func() error {
			return copyRows("enrollments", []string{"student_id", "section_id"}, len(ds.Enrollments),
				func(i int) []any { x := ds.Enrollments[i]; return []any{x.StudentID, x.SectionID} })
		},
		func() error {
			return copyRows("rooms", []string{"id", "code", "building", "floor", "type", "seat_rows", "seat_cols", "capacity"},
				len(ds.Rooms), func(i int) []any {
					x := ds.Rooms[i]
					return []any{x.ID, x.Code, x.Building, int16(x.Floor), x.Type, int16(x.Rows), int16(x.Cols), x.Capacity}
				})
		},
		func() error {
			return copyRows("room_seats", []string{"id", "room_id", "row_no", "col_no", "is_active", "is_accessible"},
				len(ds.Seats), func(i int) []any {
					x := ds.Seats[i]
					return []any{x.ID, x.RoomID, int16(x.Row), int16(x.Col), x.Active, x.Accessible}
				})
		},
		func() error {
			p := ds.Period
			return copyRows("exam_periods", []string{"id", "name", "kind", "start_date", "days", "sessions_per_day"}, 1,
				func(int) []any {
					return []any{p.ID, p.Name, p.Kind, p.StartDate, int16(p.Days), int16(p.SessionsPerDay)}
				})
		},
		func() error {
			return copyRows("slots", []string{"id", "period_id", "day_index", "session_index", "starts_at", "ends_at"},
				len(ds.Slots), func(i int) []any {
					x := ds.Slots[i]
					return []any{x.ID, x.PeriodID, int16(x.Day), int16(x.Session), x.StartsAt, x.EndsAt}
				})
		},
		func() error {
			return copyRows("exams", []string{"id", "period_id", "course_id", "duration_minutes", "requires_lab"},
				len(ds.Exams), func(i int) []any {
					x := ds.Exams[i]
					return []any{x.ID, x.PeriodID, x.CourseID, int16(x.DurationMinutes), x.RequiresLab}
				})
		},
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}

	for _, t := range identityTables {
		q := fmt.Sprintf(`SELECT setval(pg_get_serial_sequence('%s','id'), (SELECT max(id) FROM %s))`, t, t)
		if _, err := tx.Exec(ctx, q); err != nil {
			return fmt.Errorf("%s dizisi ayarlanamadı: %w", t, err)
		}
	}

	if _, err := tx.Exec(ctx, `INSERT INTO seed_meta (seed, dataset_hash) VALUES ($1, $2)`, ds.Config.Seed, hash); err != nil {
		return fmt.Errorf("seed_meta yazılamadı: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	log.Info("seed tamamlandı",
		slog.Int64("seed", ds.Config.Seed), slog.String("hash", hash[:12]),
		slog.Duration("süre", time.Since(start).Round(time.Millisecond)))
	return nil
}
