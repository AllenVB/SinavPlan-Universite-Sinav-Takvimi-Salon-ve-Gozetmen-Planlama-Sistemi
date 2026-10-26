#!/usr/bin/env bash
# Dizinsiz/dizinli EXPLAIN (ANALYZE, BUFFERS): her sorgu için dizin bir transaction içinde
# DROP edilir, plan alınır, ROLLBACK ile geri gelir (veri/şema kalıcı değişmez).
# Çıktı: docs/explain/<ad>.txt ve özet docs/explain/README.md. Önkoşul: yığın ayakta + çizelge (make verify).
set -euo pipefail
cd "$(dirname "$0")/../.."
[ -f .env ] && set -a && . ./.env && set +a
PSQL=(docker compose exec -T postgres psql -U "${POSTGRES_USER:-sinavplan}" -d "${POSTGRES_DB:-sinavplan}" -v ON_ERROR_STOP=1 -X -q -t -A)
out=docs/explain; mkdir -p "$out"

SCHED=$("${PSQL[@]}" -c "SELECT id FROM \"wm-schedules\" WHERE status='published' ORDER BY id DESC LIMIT 1")
[ -n "$SCHED" ] || { echo "yayınlanmış çizelge yok; önce make verify" >&2; exit 1; }
STUDENT=$("${PSQL[@]}" -c "SELECT student_id FROM \"wm-seat_assignments\" WHERE schedule_id=$SCHED ORDER BY student_id LIMIT 1 OFFSET 500")
SECTION=$("${PSQL[@]}" -c "SELECT section_id FROM \"wm-enrollments\" GROUP BY section_id ORDER BY count(*) DESC, section_id LIMIT 1")
SLOT=$("${PSQL[@]}" -c "SELECT slot_id FROM \"wm-schedule_exams\" WHERE schedule_id=$SCHED ORDER BY slot_id LIMIT 1")
INV=$("${PSQL[@]}" -c "SELECT user_id FROM \"wm-invigilator_assignments\" WHERE schedule_id=$SCHED ORDER BY user_id LIMIT 1")
"${PSQL[@]}" -c "ANALYZE" >/dev/null

# ad | dizin | sorgu
cases=(
"seat_by_student|seat_assignments_student_idx|SELECT * FROM \"wm-seat_assignments\" WHERE student_id = $STUDENT AND schedule_id = $SCHED"
"enrollments_by_section|enrollments_section_idx|SELECT student_id FROM \"wm-enrollments\" WHERE section_id = $SECTION"
"exams_in_slot|schedule_exams_slot_idx|SELECT exam_id FROM \"wm-schedule_exams\" WHERE schedule_id = $SCHED AND slot_id = $SLOT"
"duties_by_user|invigilator_assignments_user_idx|SELECT * FROM \"wm-invigilator_assignments\" WHERE user_id = $INV AND schedule_id = $SCHED"
)
plan() { # $1=dizin düşür mü, $2=sorgu
  local drop=""
  [ "$1" = "without" ] && drop="DROP INDEX $3;"
  "${PSQL[@]}" -c "BEGIN; $drop EXPLAIN (ANALYZE, BUFFERS, COSTS OFF, TIMING OFF) $2; ROLLBACK;" 2>&1 | grep -v -E '^(BEGIN|ROLLBACK|DROP INDEX)$'
}
{
  echo "# EXPLAIN (ANALYZE) — dizinsiz / dizinli"
  echo
  echo "Çizelge $SCHED, öğrenci $STUDENT, şube $SECTION, slot $SLOT, gözetmen $INV. Dizin transaction içinde düşürülüp ROLLBACK edilir."
  echo
} > "$out/README.md"
for c in "${cases[@]}"; do
  IFS='|' read -r name idx q <<<"$c"
  w=$(plan without "$q" "$idx"); i=$(plan with "$q" "$idx")
  { echo "-- $q"; echo "== DİZİNSİZ ($idx düşürüldü) =="; echo "$w"; echo; echo "== DİZİNLİ =="; echo "$i"; } > "$out/$name.txt"
  node_w=$(echo "$w" | grep -m1 -oE '(Seq Scan|Index Scan|Index Only Scan|Bitmap Heap Scan)[^(]*' || true)
  node_i=$(echo "$i" | grep -m1 -oE '(Seq Scan|Index Scan|Index Only Scan|Bitmap Heap Scan)[^(]*' || true)
  tw=$(echo "$w" | grep -oE 'Execution Time: [0-9.]+ ms' || true); ti=$(echo "$i" | grep -oE 'Execution Time: [0-9.]+ ms' || true)
  printf -- '## %s\n\n`%s`\n\n| | düğüm | süre |\n|---|---|---|\n| dizinsiz | %s | %s |\n| dizinli (`%s`) | %s | %s |\n\nTam plan: [%s.txt](%s.txt)\n\n' \
    "$name" "$q" "$node_w" "${tw#Execution Time: }" "$idx" "$node_i" "${ti#Execution Time: }" "$name" "$name" >> "$out/README.md"
done
cat "$out/README.md"
