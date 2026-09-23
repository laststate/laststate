-- v0.5 device DNA
-- Captures per-device "DNA" — fingerprint, MCU UUID, boot statistics, flash wear,
-- bootloader signature. Used for hardware-level deduplication and similarity
-- matching across fleets (e.g., detecting clones or firmware leaks).

CREATE TABLE IF NOT EXISTS device_dna (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_id UUID NOT NULL UNIQUE REFERENCES devices(id) ON DELETE CASCADE,
    project_id UUID NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    fingerprint TEXT NOT NULL DEFAULT '',
    mcu_uuid TEXT NOT NULL DEFAULT '',
    mcu_serial TEXT NOT NULL DEFAULT '',
    boot_cycle_count BIGINT NOT NULL DEFAULT 0,
    total_uptime_seconds BIGINT NOT NULL DEFAULT 0,
    avg_boot_time_ms INTEGER NOT NULL DEFAULT 0,
    min_boot_time_ms INTEGER NOT NULL DEFAULT 0,
    max_boot_time_ms INTEGER NOT NULL DEFAULT 0,
    flash_wear_percent DOUBLE PRECISION NOT NULL DEFAULT 0.0,
    flash_total_blocks INTEGER NOT NULL DEFAULT 0,
    flash_free_blocks INTEGER NOT NULL DEFAULT 0,
    flash_write_cycle_count BIGINT NOT NULL DEFAULT 0,
    bootloader_signature TEXT NOT NULL DEFAULT '',
    bootloader_version TEXT NOT NULL DEFAULT '',
    hardware_revision TEXT NOT NULL DEFAULT '',
    captured_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS device_dna_fingerprint_idx ON device_dna(fingerprint);
CREATE INDEX IF NOT EXISTS device_dna_mcu_idx ON device_dna(mcu_uuid);
CREATE INDEX IF NOT EXISTS device_dna_project_idx ON device_dna(project_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS device_dna_flash_wear_idx ON device_dna(flash_wear_percent);
CREATE INDEX IF NOT EXISTS device_dna_bootloader_idx ON device_dna(bootloader_signature);

COMMENT ON TABLE device_dna IS 'Hardware-level DNA: fingerprint, MCU UUID, flash wear, bootloader signature per device';

-- Similarity score table populated by offline job that compares fingerprints.
CREATE TABLE IF NOT EXISTS device_dna_similarity (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    device_a_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    device_b_id UUID NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
    similarity DOUBLE PRECISION NOT NULL,   -- 0.0 .. 1.0
    matched_fields TEXT[] NOT NULL DEFAULT ARRAY[]::TEXT[],
    computed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (device_a_id, device_b_id)
);
CREATE INDEX IF NOT EXISTS device_dna_sim_ab_idx ON device_dna_similarity(device_a_id, device_b_id);
CREATE INDEX IF NOT EXISTS device_dna_sim_score_idx ON device_dna_similarity(similarity DESC);

COMMENT ON TABLE device_dna_similarity IS 'Pairwise hardware similarity scores for clone/leak detection';
