-- 1. Drop the old string default so Postgres doesn't panic
ALTER TABLE charges ALTER COLUMN status DROP DEFAULT;

-- 2. Convert the column data to the new ENUM
ALTER TABLE charges ALTER COLUMN status TYPE charge_status USING status::charge_status;

-- 3. Re-apply the default, explicitly using the ENUM type
ALTER TABLE charges ALTER COLUMN status SET DEFAULT 'created'::charge_status;