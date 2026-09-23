#!/bin/bash
# Create multiple databases for different services

set -e
set -u

function create_user_and_database() {
    local database=$1
    # Validate identifier to prevent SQL injection (PostgreSQL identifier: letter/underscore + alnum/underscore)
    if ! [[ "$database" =~ ^[a-zA-Z_][a-zA-Z0-9_]*$ ]]; then
        echo "ERROR: invalid database name '$database' — must match ^[a-zA-Z_][a-zA-Z0-9_]*\$" >&2
        exit 1
    fi
    if [ ${#database} -gt 63 ]; then
        echo "ERROR: database name too long (max 63): $database" >&2
        exit 1
    fi
    echo "Creating user and database '$database'"
    # Generate a random password per database (never reuse identifier as password).
    # Uses openssl if available, otherwise fallback to /dev/urandom.
    local db_password
    if command -v openssl >/dev/null 2>&1; then
        db_password=$(openssl rand -hex 16)
    else
        db_password=$(tr -dc 'A-Za-z0-9' </dev/urandom | head -c 32)
    fi
    # Use psql variables + format() with %I/%L to safely quote identifiers/literals
    psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" \
        -v db="$database" -v pwd="$db_password" <<-'EOSQL'
        DO $$
        BEGIN
            IF NOT EXISTS (SELECT FROM pg_catalog.pg_roles WHERE rolname = :'db') THEN
                EXECUTE format('CREATE USER %I WITH PASSWORD %L', :'db', :'pwd');
            END IF;
        END
        $$;
        SELECT format('CREATE DATABASE %I OWNER %I', :'db', :'db')
        WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = :'db') \gexec
        GRANT ALL PRIVILEGES ON DATABASE :"db" TO :"db";
EOSQL
    echo "Database '$database' ready (owner: $database)"
}

if [ -n "$POSTGRES_MULTIPLE_DATABASES" ]; then
    echo "Multiple database creation requested: $POSTGRES_MULTIPLE_DATABASES"
    for db in $(echo "$POSTGRES_MULTIPLE_DATABASES" | tr ',' ' '); do
        # Trim whitespace
        db=$(echo "$db" | tr -d '[:space:]')
        [ -z "$db" ] && continue
        create_user_and_database "$db"
    done
    echo "Multiple databases created successfully"
fi
