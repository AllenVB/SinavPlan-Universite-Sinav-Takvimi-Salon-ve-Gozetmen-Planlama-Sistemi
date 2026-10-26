-- name: ListExamStudents :many
-- Bir sınava giren öğrenciler (çizelgeleme girdisi). Şube üzerinden dersle birleşir.
SELECT en.student_id, st.disability_flag
FROM "wm-exams" e
JOIN "wm-sections" s     ON s.course_id = e.course_id
JOIN "wm-enrollments" en ON en.section_id = s.id
JOIN "wm-students" st    ON st.id = en.student_id
WHERE e.id = $1
ORDER BY en.student_id;

-- name: ListStudentExamSchedule :many
-- Öğrencinin yayınlanmış takvimdeki sınavları, slotu, salonu ve koltuğu.
SELECT e.id AS exam_id, c.code AS course_code, c.name AS course_name,
       sl.starts_at, sl.ends_at, e.duration_minutes,
       r.code AS room_code, r.building, rs.row_no, rs.col_no
FROM "wm-enrollments" en
JOIN "wm-sections" s            ON s.id = en.section_id
JOIN "wm-exams" e               ON e.course_id = s.course_id
JOIN "wm-courses" c             ON c.id = e.course_id
JOIN "wm-schedules" sch         ON sch.period_id = e.period_id AND sch.status = 'published'
JOIN "wm-schedule_exams" se     ON se.schedule_id = sch.id AND se.exam_id = e.id
JOIN "wm-slots" sl              ON sl.id = se.slot_id
LEFT JOIN "wm-seat_assignments" sa ON sa.schedule_id = sch.id AND sa.exam_id = e.id AND sa.student_id = en.student_id
LEFT JOIN "wm-rooms" r          ON r.id = sa.room_id
LEFT JOIN "wm-room_seats" rs    ON rs.id = sa.seat_id
WHERE en.student_id = $1
ORDER BY sl.starts_at;

-- name: ListSlotRoomAssignments :many
SELECT ra.exam_id, ra.room_id, ra.seat_count, r.code AS room_code, r.type AS room_type
FROM "wm-room_assignments" ra
JOIN "wm-rooms" r ON r.id = ra.room_id
WHERE ra.schedule_id = $1 AND ra.slot_id = $2
ORDER BY ra.room_id, ra.exam_id;

-- name: InvigilatorLoad :many
SELECT user_id, count(*) AS duties
FROM "wm-invigilator_assignments"
WHERE schedule_id = $1
GROUP BY user_id
ORDER BY duties DESC, user_id;
