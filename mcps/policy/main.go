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

package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"

	"github.com/joho/godotenv"
	"github.com/wso2-enterprise/people-ops/mcps/policy/internal/db"
	"github.com/wso2-enterprise/people-ops/mcps/policy/internal/embedding"
	"github.com/wso2-enterprise/people-ops/mcps/policy/internal/handler"
)

func main() {
	if os.Getenv("ENV") != "production" {
		if err := godotenv.Load(); err != nil {
			slog.Info("No .env file found, relying on system environment variables.")
		}
	}

	ctx := context.Background()

	// Initialize pgvector database repository
	dsn := db.BuildDSNFromEnv()
	pgVectorDB, err := db.NewPgVectorDB(ctx, dsn)
	if err != nil {
		slog.Error("Database connection initialization warning (health check will report degraded)", "error", err)
	} else {
		defer pgVectorDB.Close()
		slog.Info("Connected to PostgreSQL pgvector database successfully.")
	}

	// Initialize embedding client
	isDev := os.Getenv("ENV") != "production"
	embedder := embedding.NewClient(embedding.Config{
		IngestionURL:      os.Getenv("POLICY_INGESTION_URL"),
		AllowMockFallback: isDev,
	})

	// Resolve Drive folder IDs
	policyFolderID := os.Getenv("POLICY_DRIVE_FOLDER_ID")
	if policyFolderID == "" {
		policyFolderID = os.Getenv("HR_POLICY_DRIVE_FOLDER_ID")
	}

	cfg := handler.Config{
		PolicyDriveFolderID:   policyFolderID,
		TrainingDriveFolderID: os.Getenv("TRAINING_DRIVE_FOLDER_ID"),
		DevMockUserEnabled:    os.Getenv("DEV_MOCK_USER_ENABLED") == "true",
	}

	h := handler.NewHandler(cfg, embedder, pgVectorDB)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", h.Health)
	mux.HandleFunc("/register", h.Register)
	mux.HandleFunc("/tools/", h.Tools)

	port := getEnv("PORT", "8003")
	slog.Info("Policy MCP Server starting", "port", port, "env", os.Getenv("ENV"))
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
