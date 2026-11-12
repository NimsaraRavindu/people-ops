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

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/wso2-enterprise/people-ops/mcps/policy/internal/model"
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

// NewPgVectorDB initializes a connection pool to the PostgreSQL pgvector database.
func NewPgVectorDB(ctx context.Context, dsn string) (*PgVectorDB, error) {
	poolConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database DSN: %w", err)
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

	rows, err := db.pool.Query(ctx, query, args...)
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

	return results, nil
}

// BuildDSNFromEnv constructs a PostgreSQL connection string from environment variables.
func BuildDSNFromEnv() string {
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
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
	if strings.ToLower(dbSSL) == "true" || strings.ToLower(dbSSL) == "require" {
		sslMode = "require"
	}

	userInfo := ""
	if dbUser != "" {
		if dbPassword != "" {
			userInfo = fmt.Sprintf("%s:%s@", dbUser, url.QueryEscape(dbPassword))
		} else {
			userInfo = fmt.Sprintf("%s@", dbUser)
		}
	}
	return fmt.Sprintf("postgres://%s%s:%s/%s?sslmode=%s", userInfo, dbHost, dbPort, dbName, sslMode)
}
