// Copyright (c) 2026 WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package db

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wso2-open-operations/people-ops/mcps/policy/internal/model"
)

// Searcher defines database operations for policy vector similarity searches.
type Searcher interface {
	SearchChunks(ctx context.Context, embedding []float32, folderID string, topK int) ([]model.SearchResultItem, error)
	Ping(ctx context.Context) error
	Close()
}

// PgVectorDB implements the Searcher interface backed by a PostgreSQL connection pool.
type PgVectorDB struct {
	pool *pgxpool.Pool
}

// isProductionEnv determines whether the active runtime environment is production.
// ENV takes precedence over ENVIRONMENT if both are configured.
func isProductionEnv() bool {
	if env := strings.ToLower(strings.TrimSpace(os.Getenv("ENV"))); env != "" {
		return env == "production" || env == "prod"
	}
	if env := strings.ToLower(strings.TrimSpace(os.Getenv("ENVIRONMENT"))); env != "" {
		return env == "production" || env == "prod"
	}
	return false
}

// NewPgVectorDB initializes a connection pool to the PostgreSQL pgvector database.
func NewPgVectorDB(ctx context.Context, dsn string) (*PgVectorDB, error) {
	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database DSN: %w", err)
	}
	isProd := isProductionEnv()
	if isProd {
		if poolConfig.ConnConfig.TLSConfig == nil || poolConfig.ConnConfig.TLSConfig.InsecureSkipVerify {
			return nil, fmt.Errorf("insecure database connection rejected: production requires verified TLS")
		}
		for _, fb := range poolConfig.ConnConfig.Fallbacks {
			if fb.TLSConfig == nil || fb.TLSConfig.InsecureSkipVerify {
				return nil, fmt.Errorf(
					"insecure database fallback connection rejected: production requires verified TLS on all endpoints",
				)
			}
		}
	}
	poolConfig.MaxConns = 10
	poolConfig.MinConns = 2
	poolConfig.MaxConnLifetime = 30 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create database connection pool: %w", err)
	}

	return &PgVectorDB{pool: pool}, nil
}

// NewPgVectorDBWithPool creates a PgVectorDB instance wrapping an existing connection pool.
func NewPgVectorDBWithPool(pool *pgxpool.Pool) *PgVectorDB {
	return &PgVectorDB{pool: pool}
}

// Ping tests connection liveness against the underlying database pool.
func (db *PgVectorDB) Ping(ctx context.Context) error {
	if db.pool == nil {
		return fmt.Errorf("database pool is uninitialized")
	}
	return db.pool.Ping(ctx)
}

// Close terminates all connections in the database pool.
func (db *PgVectorDB) Close() {
	if db.pool != nil {
		db.pool.Close()
	}
}

// SearchChunks executes cosine distance vector queries against pgvector policy document chunks.
func (db *PgVectorDB) SearchChunks(
	ctx context.Context,
	embedding []float32,
	folderID string,
	topK int,
) ([]model.SearchResultItem, error) {
	if db.pool == nil {
		return nil, fmt.Errorf("database connection pool not initialized")
	}

	// Build string representation: '[0.1, 0.2, ...]'
	var sb strings.Builder
	sb.WriteString("[")
	for i, f := range embedding {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(fmt.Sprintf("%.8f", f))
	}
	sb.WriteString("]")
	vecLiteral := sb.String()

	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to begin vector search transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	// Touch vector extension to register custom GUC settings in PostgreSQL backend
	if _, err := tx.Exec(ctx, "SELECT '[1]'::vector;"); err != nil {
		return nil, fmt.Errorf("failed to warm up pgvector extension: %w", err)
	}
	var hasIterativeScan *string
	scanErr := tx.QueryRow(ctx, "SELECT current_setting('hnsw.iterative_scan', true)").Scan(&hasIterativeScan)
	if scanErr != nil {
		return nil, fmt.Errorf("failed to query hnsw.iterative_scan setting: %w", scanErr)
	}
	if hasIterativeScan != nil && *hasIterativeScan != "" {
		if _, err := tx.Exec(ctx, "SET LOCAL hnsw.iterative_scan = 'relaxed_order'"); err != nil {
			return nil, fmt.Errorf("failed to set hnsw.iterative_scan: %w", err)
		}
	} else if folderID != "" {
		// When pgvector < 0.8.0 lacks iterative index scan, filtered HNSW index scans
		// suffer from overfiltering and miss candidate rows. Disable HNSW index scan
		// for folder-filtered queries to fall back to exact scan across filtered folder chunks.
		if _, err := tx.Exec(ctx, "SET LOCAL enable_indexscan = off"); err != nil {
			return nil, fmt.Errorf("failed to set enable_indexscan for folder fallback: %w", err)
		}
	}
	if _, err := tx.Exec(ctx, "SET LOCAL hnsw.ef_search = 100"); err != nil {
		return nil, fmt.Errorf("failed to set hnsw.ef_search: %w", err)
	}

	var query string
	var args []interface{}

	if folderID != "" {
		query = `
			SELECT
				pd.name,
				pc.content,
				1 - (pc.embedding <=> $1::vector) AS similarity_score
			FROM policy_chunks pc
			JOIN policy_documents pd ON pd.id = pc.document_id
			WHERE pd.drive_folder_id = $2
			ORDER BY pc.embedding <=> $1::vector
			LIMIT $3;
		`
		args = []interface{}{vecLiteral, folderID, topK}
	} else {
		query = `
			SELECT
				pd.name,
				pc.content,
				1 - (pc.embedding <=> $1::vector) AS similarity_score
			FROM policy_chunks pc
			JOIN policy_documents pd ON pd.id = pc.document_id
			ORDER BY pc.embedding <=> $1::vector
			LIMIT $2;
		`
		args = []interface{}{vecLiteral, topK}
	}

	rows, err := tx.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("pgvector query failed: %w", err)
	}
	defer rows.Close()

	var results []model.SearchResultItem
	for rows.Next() {
		var item model.SearchResultItem
		if err := rows.Scan(&item.Document, &item.Excerpt, &item.Similarity); err != nil {
			return nil, err
		}
		results = append(results, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading query results: %w", err)
	}
	_ = tx.Commit(ctx)

	return results, nil
}

// splitKeywordValueDSN splits a PostgreSQL keyword/value connection string while preserving quotes and escapes.
func splitKeywordValueDSN(dsn string) []string {
	var tokens []string
	var sb strings.Builder
	inQuote := rune(0)
	escaped := false
	for _, r := range dsn {
		if escaped {
			sb.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			sb.WriteRune(r)
			continue
		}
		if inQuote != 0 {
			sb.WriteRune(r)
			if r == inQuote {
				inQuote = 0
			}
		} else if r == '\'' || r == '"' {
			inQuote = r
			sb.WriteRune(r)
		} else if unicode.IsSpace(r) {
			if sb.Len() > 0 {
				tokens = append(tokens, sb.String())
				sb.Reset()
			}
		} else {
			sb.WriteRune(r)
		}
	}
	if sb.Len() > 0 {
		tokens = append(tokens, sb.String())
	}
	return tokens
}

// BuildDSNFromEnv constructs a PostgreSQL connection string from environment variables.
func BuildDSNFromEnv() string {
	isProd := isProductionEnv()
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		if isProd {
			if u, err := url.Parse(dsn); err == nil && u.Scheme != "" {
				q := u.Query()
				q.Set("sslmode", "verify-full")
				u.RawQuery = q.Encode()
				return u.String()
			}
			parts := splitKeywordValueDSN(dsn)
			var filtered []string
			for _, p := range parts {
				if !strings.HasPrefix(strings.ToLower(p), "sslmode=") {
					filtered = append(filtered, p)
				}
			}
			filtered = append(filtered, "sslmode=verify-full")
			return strings.Join(filtered, " ")
		}
		return dsn
	}

	dbHost := os.Getenv("DB_HOST")
	if dbHost == "" {
		dbHost = "localhost"
	}
	dbPort := os.Getenv("DB_PORT")
	if dbPort == "" {
		dbPort = "5432"
	}
	dbName := os.Getenv("DB_NAME")
	if dbName == "" {
		dbName = "defaultdb"
	}
	dbUser := os.Getenv("DB_USER")
	if dbUser == "" {
		dbUser = "postgres"
	}
	dbPassword := os.Getenv("DB_PASSWORD")
	dbSSL := os.Getenv("DB_SSL")

	sslMode := "disable"
	if isProd {
		sslMode = "verify-full"
	} else if dbSSL != "" {
		lower := strings.ToLower(dbSSL)
		switch lower {
		case "true", "require":
			sslMode = "require"
		case "verify-full", "verify-ca":
			sslMode = lower
		default:
			sslMode = "disable"
		}
	}

	userInfo := ""
	if dbUser != "" {
		if dbPassword != "" {
			userInfo = fmt.Sprintf("%s:%s@", url.QueryEscape(dbUser), url.QueryEscape(dbPassword))
		} else {
			userInfo = fmt.Sprintf("%s@", url.QueryEscape(dbUser))
		}
	}
	return fmt.Sprintf("postgres://%s%s:%s/%s?sslmode=%s", userInfo, dbHost, dbPort, dbName, sslMode)
}
