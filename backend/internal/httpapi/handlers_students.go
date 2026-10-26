package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/AllenVB/sinavplan/backend/internal/auth"
	"github.com/AllenVB/sinavplan/backend/internal/store"
	"github.com/AllenVB/sinavplan/backend/internal/store/sqlc"
)

type studentView struct {
	ID           int64  `json:"id"`
	StudentNo    string `json:"student_no"`
	FullName     string `json:"full_name"`
	DepartmentID int64  `json:"department_id"`
	Disability   bool   `json:"disability_flag"`
}

// listStudents: keyset sayfalı öğrenci listesi (?limit=&after=).
func (s *Server) listStudents(w http.ResponseWriter, r *http.Request) error {
	page, err := parsePage(r)
	if err != nil {
		return err
	}
	rows, err := sqlc.New(s.pool).ListStudentsAfter(r.Context(), sqlc.ListStudentsAfterParams{
		ID: page.After, Limit: int32(page.Limit + 1)})
	if err != nil {
		return err
	}
	views := make([]studentView, len(rows))
	for i, x := range rows {
		views[i] = studentView{x.ID, x.StudentNo, x.FullName, x.DepartmentID, x.DisabilityFlag}
	}
	WriteJSON(w, http.StatusOK, paginate(views, page.Limit, func(v studentView) int64 { return v.ID }))
	return nil
}

type examScheduleItem struct {
	ExamID          int64     `json:"exam_id"`
	CourseCode      string    `json:"course_code"`
	CourseName      string    `json:"course_name"`
	StartsAt        time.Time `json:"starts_at"`
	EndsAt          time.Time `json:"ends_at"`
	DurationMinutes int16     `json:"duration_minutes"`
	RoomCode        *string   `json:"room_code"`
	Building        *string   `json:"building"`
	SeatRow         *int16    `json:"seat_row"`
	SeatCol         *int16    `json:"seat_col"`
}

// studentExams: öğrencinin yayınlanmış takvimdeki sınavları. Öğrenci yalnızca
// kendisininkini görür (sahiplik kontrolü sunucuda).
func (s *Server) studentExams(w http.ResponseWriter, r *http.Request) error {
	id, err := pathInt64(r, "id")
	if err != nil {
		return err
	}
	if err := requireStudentAccess(mustPrincipal(r), id, auth.RoleSecretary, auth.RoleStudentAffairs); err != nil {
		return err
	}
	return s.writeStudentExams(w, r, id)
}

// myExams: öğrenci için kısayol; kimliği token'dan alır.
func (s *Server) myExams(w http.ResponseWriter, r *http.Request) error {
	p := mustPrincipal(r)
	if p.StudentID == 0 {
		return Forbidden("bu hesap bir öğrenci kaydına bağlı değil")
	}
	return s.writeStudentExams(w, r, p.StudentID)
}

func (s *Server) writeStudentExams(w http.ResponseWriter, r *http.Request, id int64) error {
	items, err := s.studentExamItems(r, id)
	if err != nil {
		return err
	}
	WriteJSON(w, http.StatusOK, map[string]any{"student_id": id, "items": items})
	return nil
}

func (s *Server) studentExamItems(r *http.Request, id int64) ([]examScheduleItem, error) {
	rows, err := sqlc.New(s.pool).ListStudentExamSchedule(r.Context(), id)
	if err != nil {
		return nil, err
	}
	items := make([]examScheduleItem, len(rows))
	for i, x := range rows {
		it := examScheduleItem{ExamID: x.ExamID, CourseCode: x.CourseCode, CourseName: x.CourseName,
			StartsAt: x.StartsAt.Time, EndsAt: x.EndsAt.Time, DurationMinutes: x.DurationMinutes}
		if x.RoomCode.Valid {
			it.RoomCode, it.Building = &x.RoomCode.String, &x.Building.String
			it.SeatRow, it.SeatCol = &x.RowNo.Int16, &x.ColNo.Int16
		}
		items[i] = it
	}
	return items, nil
}

// --- Faz 22 A3: komut paleti araması ve "Öğrenci 360" --------------------------------------

// search: GET /search?q= — öğrenci (numara/ad), ders (kod/ad), salon (kod). En az 2, en çok 64 karakter.
func (s *Server) search(w http.ResponseWriter, r *http.Request) error {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if n := len([]rune(q)); n > 64 {
		return Validation("arama en çok 64 karakter", map[string]string{"q": "en çok 64 karakter"})
	}
	hits, err := store.Search(r.Context(), s.pool, q, 6)
	if err != nil {
		return err
	}
	WriteJSON(w, http.StatusOK, map[string]any{"q": q, "items": hits})
	return nil
}

// studentOverview: GET /students/{id}/overview — kimlik, bölüm/fakülte, yayınlanmış programdaki sınavlar
// (salon + koltuk), sınav başına yoklama durumu ve mazeretler. Öğrenci yalnız kendisininkini görür.
func (s *Server) studentOverview(w http.ResponseWriter, r *http.Request) error {
	id, err := pathInt64(r, "id")
	if err != nil {
		return err
	}
	if err := requireStudentAccess(mustPrincipal(r), id, auth.RoleSecretary, auth.RoleStudentAffairs); err != nil {
		return err
	}
	o, err := store.GetStudentOverview(r.Context(), s.pool, id)
	if err != nil {
		return storeError(err)
	}
	items, err := s.studentExamItems(r, id)
	if err != nil {
		return err
	}
	WriteJSON(w, http.StatusOK, map[string]any{"student": o, "exams": items})
	return nil
}
