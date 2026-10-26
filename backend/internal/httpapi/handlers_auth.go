package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"github.com/AllenVB/sinavplan/backend/internal/auth"
	"github.com/go-chi/chi/v5"
)

func chiURLParam(r *http.Request, name string) string { return chi.URLParam(r, name) }

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type userView struct {
	ID         int64    `json:"id"`
	FullName   string   `json:"full_name,omitempty"`
	Roles      []string `json:"roles"`
	StudentID  *int64   `json:"student_id,omitempty"`
	LecturerID *int64   `json:"lecturer_id,omitempty"`
}

type tokenResponse struct {
	auth.Tokens
	TokenType string   `json:"token_type"`
	User      userView `json:"user"`
}

func viewOf(p auth.Principal) userView {
	v := userView{ID: p.UserID, Roles: p.Roles}
	if p.StudentID != 0 {
		v.StudentID = &p.StudentID
	}
	if p.LecturerID != 0 {
		v.LecturerID = &p.LecturerID
	}
	return v
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) error {
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	fields := map[string]string{}
	if strings.TrimSpace(req.Email) == "" {
		fields["email"] = "zorunlu"
	}
	if req.Password == "" {
		fields["password"] = "zorunlu"
	}
	if len(fields) > 0 {
		return Validation("e-posta ve parola zorunlu", fields)
	}
	t, err := s.auth.Login(r.Context(), strings.TrimSpace(req.Email), req.Password)
	if errors.Is(err, auth.ErrBadCredentials) {
		return Unauthorized("e-posta veya parola hatalı")
	}
	if err != nil {
		return err
	}
	WriteJSON(w, http.StatusOK, tokenResponse{Tokens: t, TokenType: "Bearer", User: viewOf(t.Principal)})
	return nil
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func (s *Server) refresh(w http.ResponseWriter, r *http.Request) error {
	var req refreshRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	if req.RefreshToken == "" {
		return Validation("refresh_token zorunlu", map[string]string{"refresh_token": "zorunlu"})
	}
	t, err := s.auth.Refresh(r.Context(), req.RefreshToken)
	if errors.Is(err, auth.ErrInvalidToken) {
		return Unauthorized("refresh token geçersiz, iptal edilmiş veya süresi dolmuş")
	}
	if err != nil {
		return err
	}
	WriteJSON(w, http.StatusOK, tokenResponse{Tokens: t, TokenType: "Bearer", User: viewOf(t.Principal)})
	return nil
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) error {
	var req refreshRequest
	if err := decodeJSON(r, &req); err != nil {
		return err
	}
	if req.RefreshToken == "" {
		return Validation("refresh_token zorunlu", map[string]string{"refresh_token": "zorunlu"})
	}
	if err := s.auth.Logout(r.Context(), req.RefreshToken); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) error {
	v := viewOf(mustPrincipal(r))
	if s.pool != nil { // ad yalnız görüntü içindir; okunamazsa /me yine de çalışır
		_ = s.pool.QueryRow(r.Context(), `SELECT full_name FROM "wm-uo-users" WHERE id = $1`, v.ID).Scan(&v.FullName)
	}
	WriteJSON(w, http.StatusOK, v)
	return nil
}
