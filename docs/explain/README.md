# EXPLAIN (ANALYZE) — dizinsiz / dizinli

Çizelge 4, öğrenci 77, şube 675, slot 1, gözetmen 3. Dizin transaction içinde düşürülüp ROLLBACK edilir.

## seat_by_student

`SELECT * FROM seat_assignments WHERE student_id = 77 AND schedule_id = 4`

| | düğüm | süre |
|---|---|---|
| dizinsiz | Index Scan using seat_assignments_schedule_id_exam_id_student_id_key on seat_assignments  | 6.197 ms |
| dizinli (`seat_assignments_student_idx`) | Bitmap Heap Scan on seat_assignments  | 0.172 ms |

Tam plan: [seat_by_student.txt](seat_by_student.txt)

## enrollments_by_section

`SELECT student_id FROM enrollments WHERE section_id = 675`

| | düğüm | süre |
|---|---|---|
| dizinsiz | Seq Scan on enrollments  | 2.564 ms |
| dizinli (`enrollments_section_idx`) | Bitmap Heap Scan on enrollments  | 0.522 ms |

Tam plan: [enrollments_by_section.txt](enrollments_by_section.txt)

## exams_in_slot

`SELECT exam_id FROM schedule_exams WHERE schedule_id = 4 AND slot_id = 1`

| | düğüm | süre |
|---|---|---|
| dizinsiz | Index Only Scan using schedule_exams_schedule_id_exam_id_slot_id_key on schedule_exams  | 0.139 ms |
| dizinli (`schedule_exams_slot_idx`) | Index Only Scan using schedule_exams_schedule_id_exam_id_slot_id_key on schedule_exams  | 0.111 ms |

Tam plan: [exams_in_slot.txt](exams_in_slot.txt)

## duties_by_user

`SELECT * FROM invigilator_assignments WHERE user_id = 3 AND schedule_id = 4`

| | düğüm | süre |
|---|---|---|
| dizinsiz | Index Scan using invigilator_assignments_schedule_id_slot_id_user_id_key on invigilator_assignments  | 0.281 ms |
| dizinli (`invigilator_assignments_user_idx`) | Bitmap Heap Scan on invigilator_assignments  | 0.165 ms |

Tam plan: [duties_by_user.txt](duties_by_user.txt)

