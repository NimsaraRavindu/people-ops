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
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/wso2-open-operations/people-ops/mcps/policy/internal/auth"
	"github.com/wso2-open-operations/people-ops/mcps/policy/internal/db"
	"github.com/wso2-open-operations/people-ops/mcps/policy/internal/embedding"
	"github.com/wso2-open-operations/people-ops/mcps/policy/internal/handler"
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
		slog.Info("Connected to PostgreSQL pgvector database successfully.")
	}

	// Initialize embedding client
	embedder := embedding.NewClient(embedding.Config{
		IngestionURL:      os.Getenv("POLICY_INGESTION_URL"),
		IngestionSecret:   os.Getenv("POLICY_INGESTION_SECRET"),
		AllowMockFallback: auth.IsPermittedDevEnvironment(),
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

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		slog.Info("Policy MCP Server starting", "port", port, "env", os.Getenv("ENV"))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Server listen failed: %v", err)
		}
	}()

	<-stop
	slog.Info("Shutting down Policy MCP server gracefully...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("Server forced shutdown", "error", err)
	} else {
		slog.Info("Policy MCP server stopped cleanly")
	}

	if pgVectorDB != nil {
		pgVectorDB.Close()
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
