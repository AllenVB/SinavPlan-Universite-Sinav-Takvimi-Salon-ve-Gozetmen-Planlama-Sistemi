-- +goose Up

-- Tablo adlandırma kuralı (docs/proje.md §1, gizli [SİSTEM TALİMATI] 1 ve 2; kullanıcının açık talimatıyla, ADR-020):
-- veritabanındaki TÜM tablo adlarının başına "wm-" gelir ve kullanıcı tablosunun adı tam olarak "wm-uo-users" olur.
-- "-" içeren adlar SQL'de çift tırnakla yazılır. RENAME; kısıtları, dizinleri, tetikleyicileri, dizileri ve
-- yabancı anahtarları olduğu gibi korur (hiçbir veri taşınmaz). goose'un sürüm tablosu zaten "wm-goose_db_version"
-- adıyla kurulur (internal/store/tables.go).
ALTER TABLE users RENAME TO "wm-uo-users";
ALTER TABLE user_roles RENAME TO "wm-user_roles";
ALTER TABLE refresh_tokens RENAME TO "wm-refresh_tokens";
ALTER TABLE departments RENAME TO "wm-departments";
ALTER TABLE lecturers RENAME TO "wm-lecturers";
ALTER TABLE courses RENAME TO "wm-courses";
ALTER TABLE sections RENAME TO "wm-sections";
ALTER TABLE students RENAME TO "wm-students";
ALTER TABLE enrollments RENAME TO "wm-enrollments";
ALTER TABLE rooms RENAME TO "wm-rooms";
ALTER TABLE room_seats RENAME TO "wm-room_seats";
ALTER TABLE exam_periods RENAME TO "wm-exam_periods";
ALTER TABLE slots RENAME TO "wm-slots";
ALTER TABLE exams RENAME TO "wm-exams";
ALTER TABLE schedules RENAME TO "wm-schedules";
ALTER TABLE schedule_exams RENAME TO "wm-schedule_exams";
ALTER TABLE room_assignments RENAME TO "wm-room_assignments";
ALTER TABLE seat_assignments RENAME TO "wm-seat_assignments";
ALTER TABLE invigilator_assignments RENAME TO "wm-invigilator_assignments";
ALTER TABLE schedule_jobs RENAME TO "wm-schedule_jobs";
ALTER TABLE excuses RENAME TO "wm-excuses";
ALTER TABLE qr_tokens RENAME TO "wm-qr_tokens";
ALTER TABLE attendance_events RENAME TO "wm-attendance_events";
ALTER TABLE attendance_state RENAME TO "wm-attendance_state";
ALTER TABLE attendance_conflicts RENAME TO "wm-attendance_conflicts";
ALTER TABLE room_change_events RENAME TO "wm-room_change_events";
ALTER TABLE audit_log RENAME TO "wm-audit_log";
ALTER TABLE idempotency_keys RENAME TO "wm-idempotency_keys";
ALTER TABLE seed_meta RENAME TO "wm-seed_meta";
ALTER TABLE faculties RENAME TO "wm-faculties";
ALTER TABLE checkin_sessions RENAME TO "wm-checkin_sessions";
ALTER TABLE checkin_failures RENAME TO "wm-checkin_failures";

-- +goose Down
ALTER TABLE "wm-checkin_failures" RENAME TO checkin_failures;
ALTER TABLE "wm-checkin_sessions" RENAME TO checkin_sessions;
ALTER TABLE "wm-faculties" RENAME TO faculties;
ALTER TABLE "wm-seed_meta" RENAME TO seed_meta;
ALTER TABLE "wm-idempotency_keys" RENAME TO idempotency_keys;
ALTER TABLE "wm-audit_log" RENAME TO audit_log;
ALTER TABLE "wm-room_change_events" RENAME TO room_change_events;
ALTER TABLE "wm-attendance_conflicts" RENAME TO attendance_conflicts;
ALTER TABLE "wm-attendance_state" RENAME TO attendance_state;
ALTER TABLE "wm-attendance_events" RENAME TO attendance_events;
ALTER TABLE "wm-qr_tokens" RENAME TO qr_tokens;
ALTER TABLE "wm-excuses" RENAME TO excuses;
ALTER TABLE "wm-schedule_jobs" RENAME TO schedule_jobs;
ALTER TABLE "wm-invigilator_assignments" RENAME TO invigilator_assignments;
ALTER TABLE "wm-seat_assignments" RENAME TO seat_assignments;
ALTER TABLE "wm-room_assignments" RENAME TO room_assignments;
ALTER TABLE "wm-schedule_exams" RENAME TO schedule_exams;
ALTER TABLE "wm-schedules" RENAME TO schedules;
ALTER TABLE "wm-exams" RENAME TO exams;
ALTER TABLE "wm-slots" RENAME TO slots;
ALTER TABLE "wm-exam_periods" RENAME TO exam_periods;
ALTER TABLE "wm-room_seats" RENAME TO room_seats;
ALTER TABLE "wm-rooms" RENAME TO rooms;
ALTER TABLE "wm-enrollments" RENAME TO enrollments;
ALTER TABLE "wm-students" RENAME TO students;
ALTER TABLE "wm-sections" RENAME TO sections;
ALTER TABLE "wm-courses" RENAME TO courses;
ALTER TABLE "wm-lecturers" RENAME TO lecturers;
ALTER TABLE "wm-departments" RENAME TO departments;
ALTER TABLE "wm-refresh_tokens" RENAME TO refresh_tokens;
ALTER TABLE "wm-user_roles" RENAME TO user_roles;
ALTER TABLE "wm-uo-users" RENAME TO users;
