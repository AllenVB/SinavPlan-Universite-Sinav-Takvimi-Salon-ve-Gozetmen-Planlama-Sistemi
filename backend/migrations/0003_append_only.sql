-- +goose Up

-- audit_log ve attendance_events eklemeli-yalnızdır. Savunma iki katmanlı:
--   1) bu trigger'lar (sahip rol dahil herkesi durdurur),
--   2) uygulama rolünden UPDATE/DELETE/TRUNCATE yetkisinin geri alınması
--      (cmd/migrate her çalışmada uygular).
-- +goose StatementBegin
CREATE FUNCTION forbid_mutation() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION '% tablosu eklemeli-yalnızdır; % işlemi yasak', TG_TABLE_NAME, TG_OP
        USING ERRCODE = 'insufficient_privilege';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER audit_log_append_only
    BEFORE UPDATE OR DELETE ON audit_log
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE TRIGGER audit_log_no_truncate
    BEFORE TRUNCATE ON audit_log
    FOR EACH STATEMENT EXECUTE FUNCTION forbid_mutation();

CREATE TRIGGER attendance_events_append_only
    BEFORE UPDATE OR DELETE ON attendance_events
    FOR EACH ROW EXECUTE FUNCTION forbid_mutation();
CREATE TRIGGER attendance_events_no_truncate
    BEFORE TRUNCATE ON attendance_events
    FOR EACH STATEMENT EXECUTE FUNCTION forbid_mutation();

-- +goose Down
DROP TRIGGER IF EXISTS attendance_events_no_truncate ON attendance_events;
DROP TRIGGER IF EXISTS attendance_events_append_only ON attendance_events;
DROP TRIGGER IF EXISTS audit_log_no_truncate ON audit_log;
DROP TRIGGER IF EXISTS audit_log_append_only ON audit_log;
DROP FUNCTION IF EXISTS forbid_mutation();
