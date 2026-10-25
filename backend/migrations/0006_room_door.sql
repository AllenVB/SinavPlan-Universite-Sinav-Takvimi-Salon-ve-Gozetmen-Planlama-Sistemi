-- +goose Up

-- Kapının hangi duvarda ve o duvarın neresinde olduğu (oturma krokisinde gösterilir).
-- front: 0. sıranın bulunduğu duvar (erişilebilir koltuklar kapıya yakın olsun diye varsayılan), back: son sıra,
-- left: 0. sütun, right: son sütun. door_pos, duvar boyunca 0 tabanlı konum (front/back için sütun, left/right için sıra).
ALTER TABLE rooms
    ADD COLUMN door_side text     NOT NULL DEFAULT 'front',
    ADD COLUMN door_pos  smallint NOT NULL DEFAULT 0,
    ADD CONSTRAINT rooms_door_side_check CHECK (door_side IN ('front','back','left','right')),
    ADD CONSTRAINT rooms_door_pos_check  CHECK (door_pos >= 0);

-- +goose Down
ALTER TABLE rooms DROP CONSTRAINT IF EXISTS rooms_door_pos_check, DROP CONSTRAINT IF EXISTS rooms_door_side_check,
    DROP COLUMN IF EXISTS door_pos, DROP COLUMN IF EXISTS door_side;
