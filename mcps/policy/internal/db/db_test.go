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
	"os"
	"strings"
	"testing"
)

func TestBuildDSNFromEnv(t *testing.T) {
	t.Run("Default development DSN has sslmode=disable", func(t *testing.T) {
		t.Setenv("ENV", "development")
		t.Setenv("ENVIRONMENT", "development")
		t.Setenv("DB_SSL", "")
		t.Setenv("DATABASE_URL", "")
		dsn := BuildDSNFromEnv()
		if !strings.Contains(dsn, "sslmode=disable") {
			t.Errorf("expected sslmode=disable, got: %s", dsn)
		}
	})

	t.Run("ENV=development takes precedence over ENVIRONMENT=production", func(t *testing.T) {
		t.Setenv("ENV", "development")
		t.Setenv("ENVIRONMENT", "production")
		t.Setenv("DB_SSL", "")
		t.Setenv("DATABASE_URL", "")
		dsn := BuildDSNFromEnv()
		if !strings.Contains(dsn, "sslmode=disable") {
			t.Errorf("expected sslmode=disable when ENV=development, got: %s", dsn)
		}
	})

	t.Run("ENVIRONMENT=production forces verify-full when ENV is unset", func(t *testing.T) {
		t.Setenv("ENV", "")
		t.Setenv("ENVIRONMENT", "production")
		t.Setenv("DB_SSL", "disable")
		t.Setenv("DATABASE_URL", "")
		dsn := BuildDSNFromEnv()
		if !strings.Contains(dsn, "sslmode=verify-full") {
			t.Errorf("expected sslmode=verify-full when ENVIRONMENT=production, got: %s", dsn)
		}
	})

	t.Run("Production forces sslmode=verify-full when DB_SSL is disable or unset", func(t *testing.T) {
		t.Setenv("ENV", "production")
		t.Setenv("ENVIRONMENT", "production")
		t.Setenv("DB_SSL", "disable")
		t.Setenv("DATABASE_URL", "")
		dsn := BuildDSNFromEnv()
		if !strings.Contains(dsn, "sslmode=verify-full") {
			t.Errorf("expected sslmode=verify-full in production, got: %s", dsn)
		}

		t.Setenv("DB_SSL", "require")
		dsnRequire := BuildDSNFromEnv()
		if !strings.Contains(dsnRequire, "sslmode=verify-full") {
			t.Errorf("expected sslmode=verify-full in production for DB_SSL=require, got: %s", dsnRequire)
		}
	})

	t.Run("Production forces verify-full on plaintext DATABASE_URL", func(t *testing.T) {
		t.Setenv("ENV", "production")
		t.Setenv("ENVIRONMENT", "production")
		t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db?sslmode=disable")
		dsn := BuildDSNFromEnv()
		if !strings.Contains(dsn, "sslmode=verify-full") {
			t.Errorf("expected sslmode=verify-full on production DATABASE_URL, got: %s", dsn)
		}

		t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db?sslmode=require")
		dsnRequire := BuildDSNFromEnv()
		if !strings.Contains(dsnRequire, "sslmode=verify-full") {
			t.Errorf("expected sslmode=verify-full on production DATABASE_URL with require, got: %s", dsnRequire)
		}
	})

	t.Run("Production forces verify-full on keyword-value DATABASE_URL", func(t *testing.T) {
		t.Setenv("ENV", "production")
		t.Setenv("ENVIRONMENT", "production")
		t.Setenv("DATABASE_URL", "host=db.example.com password=example%word sslmode=disable")
		dsn := BuildDSNFromEnv()
		if !strings.Contains(dsn, "sslmode=verify-full") {
			t.Errorf("expected sslmode=verify-full on keyword/value DATABASE_URL, got: %s", dsn)
		}
		if strings.Contains(dsn, "sslmode=disable") {
			t.Errorf("sslmode=disable was not stripped from keyword/value DATABASE_URL: %s", dsn)
		}
	})

	t.Run("Custom DB_SSL verify-ca is preserved", func(t *testing.T) {
		t.Setenv("ENV", "development")
		t.Setenv("ENVIRONMENT", "development")
		t.Setenv("DB_SSL", "verify-ca")
		t.Setenv("DATABASE_URL", "")
		dsn := BuildDSNFromEnv()
		if !strings.Contains(dsn, "sslmode=verify-ca") {
			t.Errorf("expected sslmode=verify-ca, got: %s", dsn)
		}
	})

	t.Run("Custom DB_SSL verify-full is preserved", func(t *testing.T) {
		t.Setenv("ENV", "development")
		t.Setenv("ENVIRONMENT", "development")
		t.Setenv("DB_SSL", "verify-full")
		t.Setenv("DATABASE_URL", "")
		dsn := BuildDSNFromEnv()
		if !strings.Contains(dsn, "sslmode=verify-full") {
			t.Errorf("expected sslmode=verify-full, got: %s", dsn)
		}
	})

	t.Run("Preserves whitespace inside quoted keyword/value DSN", func(t *testing.T) {
		t.Setenv("ENV", "production")
		t.Setenv("ENVIRONMENT", "production")
		t.Setenv("DATABASE_URL", "host=db.example.com password='a  b' sslmode=disable")
		dsn := BuildDSNFromEnv()
		if !strings.Contains(dsn, "password='a  b'") {
			t.Errorf("expected whitespace in quoted value preserved, got: %s", dsn)
		}
		if !strings.Contains(dsn, "sslmode=verify-full") {
			t.Errorf("expected sslmode=verify-full, got: %s", dsn)
		}
	})

	t.Run("Preserves escaped quotes and backslashes in quoted keyword/value DSN", func(t *testing.T) {
		t.Setenv("ENV", "production")
		t.Setenv("ENVIRONMENT", "production")
		t.Setenv("DATABASE_URL", `host=db.example.com password='a\'  b' sslmode=disable`)
		dsn := BuildDSNFromEnv()
		if !strings.Contains(dsn, `password='a\'  b'`) {
			t.Errorf("expected escaped quote preserved, got: %s", dsn)
		}
		if !strings.Contains(dsn, "sslmode=verify-full") {
			t.Errorf("expected sslmode=verify-full, got: %s", dsn)
		}

		t.Setenv("DATABASE_URL", `host=db.example.com password='a\\b' sslmode=disable`)
		dsnBackslash := BuildDSNFromEnv()
		if !strings.Contains(dsnBackslash, `password='a\\b'`) {
			t.Errorf("expected escaped backslash preserved, got: %s", dsnBackslash)
		}
	})
}

func TestNewPgVectorDB_ProductionSecurity(t *testing.T) {
	t.Setenv("ENV", "production")
	t.Setenv("ENVIRONMENT", "production")
	ctx := t.Context()

	// Reject plaintext in production
	_, err := NewPgVectorDB(ctx, "postgres://user:pass@localhost:5432/db?sslmode=disable")
	if err == nil || !strings.Contains(err.Error(), "production requires verified TLS") {
		t.Errorf("expected rejection for sslmode=disable in production, got: %v", err)
	}

	// Reject unverified TLS (require mode lacks root cert verification by default)
	_, err = NewPgVectorDB(ctx, "postgres://user:pass@localhost:5432/db?sslmode=require")
	if err == nil || !strings.Contains(err.Error(), "production requires verified TLS") {
		t.Errorf("expected rejection for sslmode=require in production, got: %v", err)
	}

	// Reject verify-ca in production because it skips hostname verification
	_, err = NewPgVectorDB(ctx, "postgres://user:pass@localhost:5432/db?sslmode=verify-ca")
	if err == nil || !strings.Contains(err.Error(), "production requires verified TLS") {
		t.Errorf("expected rejection for sslmode=verify-ca in production, got: %v", err)
	}

	// Reject non-existent root certificate file
	_, err = NewPgVectorDB(ctx, "host=db.example.com sslmode=verify-full sslrootcert=/nonexistent/ca.crt")
	if err == nil {
		t.Errorf("expected rejection for non-existent root cert, got nil")
	}

	// Reject unencrypted socket fallback in production
	_, err = NewPgVectorDB(ctx, "host=db.example.com,/tmp sslmode=verify-full")
	if err == nil || !strings.Contains(err.Error(), "production requires verified TLS") {
		t.Errorf("expected rejection for insecure fallback in production, got: %v", err)
	}

	// Reject malformed DSN
	_, err = NewPgVectorDB(ctx, "://invalid-dsn")
	if err == nil || !strings.Contains(err.Error(), "failed to parse database DSN") {
		t.Errorf("expected DSN parse failure, got: %v", err)
	}
}

func TestPgVectorDB_VectorSearchIntegration(t *testing.T) {
	testDSN := os.Getenv("TEST_DATABASE_URL")
	if testDSN == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping database integration test")
	}

	ctx := t.Context()
	db, err := NewPgVectorDB(ctx, testDSN)
	if err != nil {
		t.Fatalf("failed to connect to test database: %v", err)
	}
	defer db.Close()

	if err := db.Ping(ctx); err != nil {
		t.Fatalf("failed to ping test database: %v", err)
	}

	var dim int
	dimQuery := "SELECT atttypmod FROM pg_attribute WHERE attrelid = 'policy_chunks'::regclass AND attname = 'embedding'"
	row := db.pool.QueryRow(ctx, dimQuery)
	if err := row.Scan(&dim); err != nil || dim <= 0 {
		dim = 768
	}
	embedding := make([]float32, dim)
	embedding[0] = 0.5
	embedding[1] = 0.5

	results, err := db.SearchChunks(ctx, embedding, "", 5)
	if err != nil {
		t.Fatalf("SearchChunks failed without folder filter: %v", err)
	}
	t.Logf("SearchChunks returned %d results without folder filter", len(results))

	resultsFiltered, err := db.SearchChunks(ctx, embedding, "non-existent-folder-id", 5)
	if err != nil {
		t.Fatalf("SearchChunks failed with folder filter: %v", err)
	}
	t.Logf("SearchChunks returned %d results with non-existent folder filter", len(resultsFiltered))

	var existingFolder string
	query := "SELECT drive_folder_id FROM policy_documents WHERE drive_folder_id IS NOT NULL LIMIT 1"
	if err := db.pool.QueryRow(ctx, query).Scan(&existingFolder); err == nil && existingFolder != "" {
		resultsExisting, err := db.SearchChunks(ctx, embedding, existingFolder, 5)
		if err != nil {
			t.Fatalf("SearchChunks failed with existing folder %s: %v", existingFolder, err)
		}
		t.Logf("SearchChunks returned %d results with existing folder", len(resultsExisting))
	}
}
