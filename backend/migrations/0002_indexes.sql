-- +goose Up

-- Öğrencinin sınav programı: enrollments(PK student_id,...) -> sections -> exams.
CREATE INDEX sections_course_idx   ON sections (course_id);
CREATE INDEX enrollments_section_idx ON enrollments (section_id);
CREATE INDEX exams_course_idx      ON exams (course_id);

-- Öğrencinin kendi koltuk/salon kaydı (öğrenci programı ve sınav kartı).
CREATE INDEX seat_assignments_student_idx ON seat_assignments (student_id, schedule_id);

-- Oturumdaki (slot) salon atamaları.
CREATE INDEX room_assignments_slot_idx ON room_assignments (schedule_id, slot_id, room_id);
CREATE INDEX schedule_exams_slot_idx   ON schedule_exams (schedule_id, slot_id);

-- Gözetmen yükü ve kişisel görev listesi.
CREATE INDEX invigilator_assignments_user_idx ON invigilator_assignments (user_id, schedule_id);
CREATE INDEX invigilator_assignments_room_idx ON invigilator_assignments (schedule_id, exam_id, room_id);

-- Yoklama olaylarını cihaz ve sınav bazında okuma.
CREATE INDEX attendance_events_device_exam_idx  ON attendance_events (device_id, exam_id, hlc_wall, hlc_counter);
CREATE INDEX attendance_events_exam_student_idx ON attendance_events (exam_id, student_id);

-- Mazeret kuyruğu, QR arama, refresh token temizliği, job takibi.
CREATE INDEX excuses_status_idx        ON excuses (status, created_at);
CREATE INDEX excuses_student_idx       ON excuses (student_id) WHERE student_id IS NOT NULL;
CREATE INDEX qr_tokens_exam_student_idx ON qr_tokens (exam_id, student_id);
CREATE INDEX refresh_tokens_user_idx   ON refresh_tokens (user_id) WHERE revoked_at IS NULL;
CREATE INDEX schedule_jobs_status_idx  ON schedule_jobs (status, created_at);
CREATE INDEX audit_log_entity_idx      ON audit_log (entity, entity_id, created_at);
CREATE INDEX idempotency_keys_created_idx ON idempotency_keys (created_at);
CREATE INDEX students_department_idx   ON students (department_id);

-- +goose Down
DROP INDEX IF EXISTS sections_course_idx, enrollments_section_idx, exams_course_idx,
    seat_assignments_student_idx, room_assignments_slot_idx, schedule_exams_slot_idx,
    invigilator_assignments_user_idx, invigilator_assignments_room_idx,
    attendance_events_device_exam_idx, attendance_events_exam_student_idx,
    excuses_status_idx, excuses_student_idx, qr_tokens_exam_student_idx,
    refresh_tokens_user_idx, schedule_jobs_status_idx, audit_log_entity_idx,
    idempotency_keys_created_idx, students_department_idx;
