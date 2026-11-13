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

package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/wso2-open-operations/people-ops/mcps/policy/internal/auth"
	"github.com/wso2-open-operations/people-ops/mcps/policy/internal/buckets"
	"github.com/wso2-open-operations/people-ops/mcps/policy/internal/db"
	"github.com/wso2-open-operations/people-ops/mcps/policy/internal/embedding"
	"github.com/wso2-open-operations/people-ops/mcps/policy/internal/model"
)

var validToolNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Config encapsulates runtime configuration for policy tool dispatching.
type Config struct {
	PolicyDriveFolderID   string
	TrainingDriveFolderID string
	DevMockUserEnabled    bool
	SearchTimeout         time.Duration
}

// Handler coordinates HTTP routing, JWT identity validation, embedding, and vector querying.
type Handler struct {
	cfg      Config
	embedder embedding.Embedder
	searcher db.Searcher
}

// NewHandler initializes a new policy MCP request handler.
func NewHandler(cfg Config, embedder embedding.Embedder, searcher db.Searcher) *Handler {
	if cfg.SearchTimeout == 0 {
		cfg.SearchTimeout = 10 * time.Second
	}
	return &Handler{
		cfg:      cfg,
		embedder: embedder,
		searcher: searcher,
	}
}

// Health verifies system liveness and database connectivity.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if h.searcher == nil || h.searcher.Ping(ctx) != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"status":   "degraded",
			"service":  "policy-mcp",
			"database": "unreachable",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status":   "ok",
		"service":  "policy-mcp",
		"database": "connected",
	})
}

// Register handles capability discovery requests from Agent Core.
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode([]model.BucketKit{buckets.GetPolicyBucket()})
}

// Tools executes semantic policy and training queries.
func (h *Handler) Tools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	toolName := strings.TrimPrefix(r.URL.Path, "/tools/")
	if !validToolNameRegex.MatchString(toolName) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid tool name format"})
		return
	}

	if _, found := buckets.GetTool(toolName); !found {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{
			"error": fmt.Sprintf("Unknown tool '%s'", toolName),
		})
		return
	}

	// Extract user authentication for audit trail
	jwtToken := r.Header.Get("X-JWT-Assertion")
	if jwtToken == "" {
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			jwtToken = strings.TrimPrefix(authHeader, "Bearer ")
		}
	}
	resolvedToken := auth.ResolveToken(jwtToken)
	userEmail := auth.ExtractEmailFromJWT(r.Context(), resolvedToken)

	mockAllowed := auth.IsPermittedDevEnvironment() &&
		(h.cfg.DevMockUserEnabled || os.Getenv("DEV_MOCK_USER_ENABLED") == "true")
	if userEmail == "" && !mockAllowed {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "Unauthorized: valid user token required"})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1024*1024) // 1 MB limit
	var args map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&args); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Invalid request JSON or payload too large"})
		return
	}

	query, ok := args["query"].(string)
	if !ok || strings.TrimSpace(query) == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "Missing required argument 'query'"})
		return
	}

	if len(query) > 4096 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Query exceeds maximum allowed length of 4096 characters",
		})
		return
	}

	topK := 5
	if kVal, ok := args["top_k"].(float64); ok && kVal > 0 {
		topK = int(kVal)
		if topK > 10 {
			topK = 10
		}
	}

	var folderID string
	switch toolName {
	case "query_policy":
		folderID = h.cfg.PolicyDriveFolderID
	case "query_training":
		folderID = h.cfg.TrainingDriveFolderID
	}
	if folderID == "" {
		slog.Error("Target folder ID not configured for tool", "tool", toolName)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Target folder ID not configured for this tool"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.cfg.SearchTimeout)
	defer cancel()

	slog.Info("Executing policy search", "tool", toolName, "user", userEmail, "queryLength", len(query))

	if h.embedder == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Embedding service uninitialized"})
		return
	}

	embedding, err := h.embedder.EmbedQuery(ctx, query)
	if err != nil {
		slog.Error("Failed to embed query", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Failed to embed query"})
		return
	}

	if h.searcher == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Database service uninitialized"})
		return
	}

	results, err := h.searcher.SearchChunks(ctx, embedding, folderID, topK)
	if err != nil {
		slog.Error("Database vector search error", "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"error": "Vector search execution failed"})
		return
	}

	if results == nil {
		results = []model.SearchResultItem{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(model.ToolResponse{
		Query:       query,
		Results:     results,
		SourceCount: len(results),
	})
}
