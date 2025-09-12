-- Migration to add server_id and rename id to version_id  
-- This implements the server ID consistency changes from GitHub issue #396
-- 
-- Current schema: servers(id VARCHAR, value JSONB)
-- Target schema: servers(server_id UUID, version_id VARCHAR, value JSONB)
-- where server_id is consistent across versions, version_id is unique per version

BEGIN;

-- Add the new server_id column (UUID)
ALTER TABLE servers ADD COLUMN server_id UUID;

-- Add the new version_id column (will replace current id column) 
ALTER TABLE servers ADD COLUMN version_id VARCHAR(255);

-- Create a temporary function to generate consistent server_id values for existing records
-- All versions of the same server (same name) will get the same server_id
CREATE OR REPLACE FUNCTION migrate_to_server_version_ids()
RETURNS VOID AS $$
DECLARE
    rec RECORD;
    server_name TEXT;
    server_id_map JSONB := '{}';
    new_server_id UUID;
BEGIN
    -- Iterate through all records and assign server_id and version_id
    FOR rec IN SELECT id, value FROM servers ORDER BY id LOOP
        -- Extract server name from the JSONB value
        server_name := rec.value->>'name';
        
        -- Check if we already have a server_id for this server name
        IF (server_id_map ? server_name) THEN
            new_server_id := (server_id_map->>server_name)::UUID;
        ELSE
            -- Generate a new server_id for this server name
            new_server_id := uuid_generate_v4();
            server_id_map := jsonb_set(server_id_map, ARRAY[server_name], to_jsonb(new_server_id::TEXT));
        END IF;
        
        -- Update the record with server_id and version_id (version_id = old id)
        UPDATE servers 
        SET 
            server_id = new_server_id,
            version_id = rec.id
        WHERE id = rec.id;
    END LOOP;
END;
$$ LANGUAGE plpgsql;

-- Execute the migration function
SELECT migrate_to_server_version_ids();

-- Drop the temporary function
DROP FUNCTION migrate_to_server_version_ids();

-- Make both new columns NOT NULL now that all records have values
ALTER TABLE servers ALTER COLUMN server_id SET NOT NULL;
ALTER TABLE servers ALTER COLUMN version_id SET NOT NULL;

-- Drop the old id column (replaced by version_id)
ALTER TABLE servers DROP COLUMN id;

-- Make version_id the new primary key
ALTER TABLE servers ADD CONSTRAINT servers_pkey PRIMARY KEY (version_id);

-- Update existing indexes
DROP INDEX IF EXISTS idx_servers_id;

-- Create new indexes
CREATE INDEX idx_servers_server_id ON servers(server_id);
CREATE UNIQUE INDEX idx_servers_server_id_version ON servers(server_id, (value->>'version'));

-- Update the existing indexes to use the new column names
-- Note: The JSONB indexes remain the same since they reference the value column

COMMIT;