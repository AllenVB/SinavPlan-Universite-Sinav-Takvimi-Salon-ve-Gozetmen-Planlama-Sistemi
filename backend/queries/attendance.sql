-- name: ListDeviceExamEvents :many
-- Cihaz ve sınav bazında olaylar (HLC sırasıyla).
SELECT event_id, student_id, status, source, hlc_wall, hlc_counter
FROM attendance_events
WHERE device_id = $1 AND exam_id = $2
ORDER BY hlc_wall, hlc_counter, event_id;

-- name: UseQRToken :one
-- Tek kullanımlık QR: koşullu UPDATE; 0 satır dönerse token kullanılmış, süresi dolmuş
-- veya iptal edilmiştir. Yarış durumunu satır kilidi çözer.
UPDATE qr_tokens
SET used_at = now(), used_by_device = $2
WHERE token_hash = $1 AND used_at IS NULL AND revoked_at IS NULL AND expires_at > now()
RETURNING id, exam_id, student_id;
