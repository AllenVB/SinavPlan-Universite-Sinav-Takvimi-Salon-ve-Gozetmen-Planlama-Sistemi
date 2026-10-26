# EXPLAIN (ANALYZE) — dizinsiz / dizinli

Çizelge 6, öğrenci 94, şube 369, slot 1, gözetmen 3. Dizin transaction içinde düşürülüp ROLLBACK edilir.

## seat_by_student

`SELECT * FROM "wm-seat_assignments" WHERE student_id = 94 AND schedule_id = 6`

| | düğüm | süre |
|---|---|---|
| dizinsiz | Index Scan using seat_assignments_schedule_id_exam_id_student_id_key on "wm-seat_assignments"  | 2.104 ms |
| dizinli (`seat_assignments_student_idx`) | Bitmap Heap Scan on "wm-seat_assignments"  | 0.420 ms |

Tam plan: [seat_by_student.txt](seat_by_student.txt)

## enrollments_by_section

`SELECT student_id FROM "wm-enrollments" WHERE section_id = 369`

| | düğüm | süre |
|---|---|---|
| dizinsiz | Seq Scan on "wm-enrollments"  | 2.028 ms |
| dizinli (`enrollments_section_idx`) | Bitmap Heap Scan on "wm-enrollments"  | 0.348 ms |

Tam plan: [enrollments_by_section.txt](enrollments_by_section.txt)

## exams_in_slot

`SELECT exam_id FROM "wm-schedule_exams" WHERE schedule_id = 6 AND slot_id = 1`

| | düğüm | süre |
|---|---|---|
| dizinsiz | Index Only Scan using schedule_exams_schedule_id_exam_id_slot_id_key on "wm-schedule_exams"  | 0.128 ms |
| dizinli (`schedule_exams_slot_idx`) | Bitmap Heap Scan on "wm-schedule_exams"  | 0.126 ms |

Tam plan: [exams_in_slot.txt](exams_in_slot.txt)

## duties_by_user

`SELECT * FROM "wm-invigilator_assignments" WHERE user_id = 3 AND schedule_id = 6`

| | düğüm | süre |
|---|---|---|
| dizinsiz | Index Scan using invigilator_assignments_schedule_id_slot_id_user_id_key on "wm-invigilator_assignments"  | 0.201 ms |
| dizinli (`invigilator_assignments_user_idx`) | Bitmap Heap Scan on "wm-invigilator_assignments"  | 0.200 ms |

Tam plan: [duties_by_user.txt](duties_by_user.txt)

