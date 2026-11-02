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
	"github.com/wso2-open-operations/people-ops/mcps/people-operations/internal/handler"
)

func main() {
	if os.Getenv("ENV") != "production" {
		if err := godotenv.Load(); err != nil {
			slog.Info("No .env file found, relying on system environment variables.")
		}
	}

	cfg := handler.Config{
		LeaveBackendURL:        os.Getenv("LEAVE_BACKEND_URL"),
		PeopleBackendURL:       os.Getenv("PEOPLE_BACKEND_URL"),
		LetterRequestFormURL:   os.Getenv("LETTER_REQUEST_FORM_URL"),
		MenuBackendURL:         os.Getenv("MENU_BACKEND_URL"),
		SubscriptionBackendURL: os.Getenv("SUBSCRIPTION_BACKEND_URL"),
	}

	h := handler.NewHandler(cfg)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", h.Health)
	mux.HandleFunc("/register", h.Register)
	mux.HandleFunc("/tools/", h.Tools)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

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
		slog.Info("People Operations MCP starting", "port", port, "env", os.Getenv("ENV"))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Server listen failed: %v", err)
		}
	}()

	<-stop
	slog.Info("Shutting down People Operations MCP server gracefully...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("Server forced shutdown", "error", err)
	} else {
		slog.Info("People Operations MCP server stopped cleanly")
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}
