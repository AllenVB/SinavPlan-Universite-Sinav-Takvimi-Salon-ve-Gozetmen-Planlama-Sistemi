-- name: GetUserByEmail :one
SELECT id, email, password_hash, full_name, active FROM users WHERE email = lower($1);

-- name: GetUserByID :one
SELECT id, email, full_name, active FROM users WHERE id = $1;

-- name: ListUserRoles :many
SELECT role FROM user_roles WHERE user_id = $1 ORDER BY role;

-- name: GetStudentByUserID :one
SELECT id, student_no, full_name, department_id, disability_flag FROM students WHERE user_id = $1;

-- name: GetLecturerByUserID :one
SELECT id, department_id FROM lecturers WHERE user_id = $1;

-- name: CreateRefreshToken :one
INSERT INTO refresh_tokens (user_id, token_hash, family_id, expires_at)
VALUES ($1, $2, $3, $4) RETURNING id;

-- name: GetRefreshTokenForUpdate :one
SELECT id, user_id, family_id, expires_at, revoked_at, replaced_by
FROM refresh_tokens WHERE token_hash = $1 FOR UPDATE;

-- name: RevokeRefreshToken :exec
UPDATE refresh_tokens SET revoked_at = now(), replaced_by = $2 WHERE id = $1 AND revoked_at IS NULL;

-- name: RevokeRefreshTokenFamily :exec
UPDATE refresh_tokens SET revoked_at = now() WHERE family_id = $1 AND revoked_at IS NULL;

-- name: ListStudentsAfter :many
-- Keyset sayfalama: OFFSET yok; son görülen id'den sonrası. Bir fazla satır
-- istenir ki sonraki sayfanın varlığı ek sorgu olmadan bilinsin.
SELECT id, student_no, full_name, department_id, disability_flag
FROM students WHERE id > $1 ORDER BY id LIMIT $2;
