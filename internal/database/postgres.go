package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// PostgreSQL is an implementation of the Database interface using PostgreSQL
type PostgreSQL struct {
	pool *pgxpool.Pool
}

// NewPostgreSQL creates a new instance of the PostgreSQL database
func NewPostgreSQL(ctx context.Context, connectionURI string) (*PostgreSQL, error) {
	// Parse connection config for pool settings
	config, err := pgxpool.ParseConfig(connectionURI)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PostgreSQL config: %w", err)
	}

	// Configure pool for stability-focused defaults
	config.MaxConns = 30                      // Handle good concurrent load
	config.MinConns = 5                       // Keep connections warm for fast response
	config.MaxConnIdleTime = 30 * time.Minute // Keep connections available for bursts
	config.MaxConnLifetime = 2 * time.Hour    // Refresh connections regularly for stability

	// Create connection pool with configured settings
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create PostgreSQL pool: %w", err)
	}

	// Test the connection
	if err = pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping PostgreSQL: %w", err)
	}

	// Run migrations using a single connection from the pool
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire connection for migrations: %w", err)
	}
	defer conn.Release()
	
	migrator := NewMigrator(conn.Conn())
	if err := migrator.Migrate(ctx); err != nil {
		return nil, fmt.Errorf("failed to run database migrations: %w", err)
	}

	return &PostgreSQL{
		pool: pool,
	}, nil
}

//nolint:cyclop // Database filtering logic is inherently complex but clear
func (db *PostgreSQL) List(
	ctx context.Context,
	filter *ServerFilter,
	cursor string,
	limit int,
) ([]*apiv0.ServerJSON, string, error) {
	if limit <= 0 {
		limit = 10
	}

	if ctx.Err() != nil {
		return nil, "", ctx.Err()
	}

	// Build WHERE clause for filtering
	var whereConditions []string
	args := []any{}
	argIndex := 1

	// Add filters using JSON operators
	if filter != nil {
		if filter.Name != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("value->>'name' = $%d", argIndex))
			args = append(args, *filter.Name)
			argIndex++
		}
		if filter.RemoteURL != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("EXISTS (SELECT 1 FROM jsonb_array_elements(value->'remotes') AS remote WHERE remote->>'url' = $%d)", argIndex))
			args = append(args, *filter.RemoteURL)
			argIndex++
		}
		if filter.UpdatedSince != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("(value->'_meta'->'io.modelcontextprotocol.registry/official'->>'updated_at')::timestamp > $%d", argIndex))
			args = append(args, *filter.UpdatedSince)
			argIndex++
		}
		if filter.SubstringName != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("value->>'name' ILIKE $%d", argIndex))
			args = append(args, "%"+*filter.SubstringName+"%")
			argIndex++
		}
		if filter.Version != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("(value->'version_detail'->>'version') = $%d", argIndex))
			args = append(args, *filter.Version)
			argIndex++
		}
		if filter.IsLatest != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("(value->'_meta'->'io.modelcontextprotocol.registry/official'->>'is_latest')::boolean = $%d", argIndex))
			args = append(args, *filter.IsLatest)
			argIndex++
		}
	}

	// Add cursor pagination using primary key ID
	if cursor != "" {
		if _, err := uuid.Parse(cursor); err != nil {
			return nil, "", fmt.Errorf("invalid cursor format: %w", err)
		}
		whereConditions = append(whereConditions, fmt.Sprintf("id > $%d", argIndex))
		args = append(args, cursor)
		argIndex++
	}

	// Build the WHERE clause
	whereClause := ""
	if len(whereConditions) > 0 {
		whereClause = "WHERE " + strings.Join(whereConditions, " AND ")
	}

	// Simple query on servers table
	query := fmt.Sprintf(`
        SELECT value
        FROM servers
        %s
        ORDER BY id
        LIMIT $%d
    `, whereClause, argIndex)
	args = append(args, limit)

	rows, err := db.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("failed to query servers: %w", err)
	}
	defer rows.Close()

	var results []*apiv0.ServerJSON
	for rows.Next() {
		var valueJSON []byte

		err := rows.Scan(&valueJSON)
		if err != nil {
			return nil, "", fmt.Errorf("failed to scan server row: %w", err)
		}

		// Parse the complete ServerJSON from JSONB
		var serverJSON apiv0.ServerJSON
		if err := json.Unmarshal(valueJSON, &serverJSON); err != nil {
			return nil, "", fmt.Errorf("failed to unmarshal server JSON: %w", err)
		}

		results = append(results, &serverJSON)
	}

	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("error iterating rows: %w", err)
	}

	// Determine next cursor using registry metadata ID
	nextCursor := ""
	if len(results) > 0 && len(results) >= limit {
		lastResult := results[len(results)-1]
		if lastResult.Meta != nil && lastResult.Meta.Official != nil {
			nextCursor = lastResult.Meta.Official.ID
		}
	}

	return results, nextCursor, nil
}

func (db *PostgreSQL) GetByID(ctx context.Context, id string) (*apiv0.ServerJSON, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	query := `
		SELECT value
		FROM servers
		WHERE id = $1
	`

	var valueJSON []byte

	err := db.pool.QueryRow(ctx, query, id).Scan(&valueJSON)

	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get server by ID: %w", err)
	}

	// Parse the complete ServerJSON from JSONB
	var serverJSON apiv0.ServerJSON
	if err := json.Unmarshal(valueJSON, &serverJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal server JSON: %w", err)
	}

	return &serverJSON, nil
}

// CreateServer adds a new server to the database
func (db *PostgreSQL) CreateServer(ctx context.Context, server *apiv0.ServerJSON) (*apiv0.ServerJSON, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Get the ID from the registry metadata
	if server.Meta == nil || server.Meta.Official == nil {
		return nil, fmt.Errorf("server must have registry metadata with ID")
	}

	id := server.Meta.Official.ID

	// Marshal the complete server to JSONB
	valueJSON, err := json.Marshal(server)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal server JSON: %w", err)
	}

	// Insert into simple servers table
	query := `
		INSERT INTO servers (id, value)
		VALUES ($1, $2)
	`

	_, err = db.pool.Exec(ctx, query, id, valueJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to insert server: %w", err)
	}

	return server, nil
}

// UpdateServer updates an existing server record with new server details
func (db *PostgreSQL) UpdateServer(ctx context.Context, id string, server *apiv0.ServerJSON) (*apiv0.ServerJSON, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Validate that meta structure exists and ID matches path
	if server.Meta == nil || server.Meta.Official == nil || server.Meta.Official.ID != id {
		return nil, fmt.Errorf("%w: io.modelcontextprotocol.registry/official.id must match path id (%s)", ErrInvalidInput, id)
	}

	// Marshal updated server
	valueJSON, err := json.Marshal(server)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal updated server: %w", err)
	}

	// Update the complete server record in simple table
	query := `
		UPDATE servers 
		SET value = $1
		WHERE id = $2
	`

	result, err := db.pool.Exec(ctx, query, valueJSON, id)
	if err != nil {
		return nil, fmt.Errorf("failed to update server: %w", err)
	}

	if result.RowsAffected() == 0 {
		return nil, ErrNotFound
	}

	return server, nil
}

// IncrementPublishCount atomically increments the publish count for an authenticated user today
func (db *PostgreSQL) IncrementPublishCount(ctx context.Context, authMethodSubject string) error {
	query := `
		INSERT INTO publish_attempts (auth_method_subject, attempt_date, attempt_count, first_attempt_at, last_attempt_at)
		VALUES ($1, CURRENT_DATE, 1, NOW(), NOW())
		ON CONFLICT (auth_method_subject, attempt_date)
		DO UPDATE SET 
			attempt_count = publish_attempts.attempt_count + 1,
			last_attempt_at = NOW()
	`
	_, err := db.pool.Exec(ctx, query, authMethodSubject)
	if err != nil {
		return fmt.Errorf("failed to increment publish count: %w", err)
	}
	return nil
}

// GetPublishCount returns the number of publishes for an authenticated user on a specific date
func (db *PostgreSQL) GetPublishCount(ctx context.Context, authMethodSubject string, date time.Time) (int, error) {
	var count int
	query := `
		SELECT COALESCE(attempt_count, 0) 
		FROM publish_attempts 
		WHERE auth_method_subject = $1 AND attempt_date = $2
	`
	err := db.pool.QueryRow(ctx, query, authMethodSubject, date.Format(time.DateOnly)).Scan(&count)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil // No attempts yet
		}
		return 0, fmt.Errorf("failed to get publish count: %w", err)
	}
	return count, nil
}

// CheckAndIncrementPublishCount atomically checks if the count is under the limit and increments if so
func (db *PostgreSQL) CheckAndIncrementPublishCount(ctx context.Context, authMethodSubject string, limit int) (currentCount int, incrementSuccessful bool, err error) {
	// Use a CTE to atomically check and increment
	query := `
		WITH current_state AS (
			SELECT COALESCE(attempt_count, 0) as count
			FROM publish_attempts
			WHERE auth_method_subject = $1 AND attempt_date = CURRENT_DATE
			UNION ALL
			SELECT 0 WHERE NOT EXISTS (
				SELECT 1 FROM publish_attempts
				WHERE auth_method_subject = $1 AND attempt_date = CURRENT_DATE
			)
			LIMIT 1
		),
		increment AS (
			INSERT INTO publish_attempts (auth_method_subject, attempt_date, attempt_count, first_attempt_at, last_attempt_at)
			SELECT $1, CURRENT_DATE, 1, NOW(), NOW()
			WHERE (SELECT count FROM current_state) < $2
			ON CONFLICT (auth_method_subject, attempt_date)
			DO UPDATE SET 
				attempt_count = publish_attempts.attempt_count + 1,
				last_attempt_at = NOW()
			WHERE publish_attempts.attempt_count < $2
			RETURNING attempt_count
		)
		SELECT 
			COALESCE((SELECT attempt_count FROM increment), (SELECT count FROM current_state)) as final_count,
			EXISTS (SELECT 1 FROM increment) as was_incremented
	`
	
	err = db.pool.QueryRow(ctx, query, authMethodSubject, limit).Scan(&currentCount, &incrementSuccessful)
	if err != nil {
		return 0, false, fmt.Errorf("failed to check and increment publish count: %w", err)
	}
	
	return currentCount, incrementSuccessful, nil
}

// Close closes the database connection
func (db *PostgreSQL) Close() error {
	db.pool.Close()
	return nil
}
