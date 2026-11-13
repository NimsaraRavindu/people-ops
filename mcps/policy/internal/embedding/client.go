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

package embedding

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"
)

// Embedder defines the contract for vector embedding generation.
type Embedder interface {
	EmbedQuery(ctx context.Context, text string) ([]float32, error)
}

// Config encapsulates configuration parameters for vector embedding clients.
type Config struct {
	IngestionURL      string
	IngestionSecret   string
	AllowMockFallback bool
	HTTPClient        *http.Client
}

// Client generates vector embeddings via the Ingestion Service.
type Client struct {
	cfg Config
}

// NewClient creates a new embedding client instance with validated defaults.
func NewClient(cfg Config) *Client {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 8 * time.Second}
	}
	return &Client{cfg: cfg}
}

// EmbedQuery converts natural language queries into 768-dimensional float32 vector embeddings.
func (c *Client) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	// 1. Try Ingestion Service endpoint if configured
	if c.cfg.IngestionURL != "" {
		emb, err := c.embedViaIngestion(ctx, text)
		if err == nil && len(emb) > 0 {
			return emb, nil
		}
		if !c.cfg.AllowMockFallback {
			return nil, fmt.Errorf("ingestion service embedding failed: %w", err)
		}
		slog.Warn("Ingestion service embedding failed, falling back to mock", "error", err)
	}

	// 2. Fallback to deterministic mock embedding for local/offline dev only
	if c.cfg.AllowMockFallback {
		slog.Warn("Using fallback mock embedding for local development (offline mode)")
		return GenerateMockEmbedding(text, 768), nil
	}

	return nil, fmt.Errorf("failed to generate vector embedding: POLICY_INGESTION_URL is not configured")
}

func (c *Client) embedViaIngestion(ctx context.Context, text string) ([]float32, error) {
	reqBody, _ := json.Marshal(map[string]string{"text": text})
	targetURL := strings.TrimRight(c.cfg.IngestionURL, "/") + "/embed"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.cfg.IngestionSecret != "" {
		req.Header.Set("X-Internal-Token", c.cfg.IngestionSecret)
	}

	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ingestion service returned status %d", resp.StatusCode)
	}

	var embedResp struct {
		Embedding []float32 `json:"embedding"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&embedResp); err != nil {
		return nil, err
	}
	if len(embedResp.Embedding) != 768 {
		return nil, fmt.Errorf(
			"unexpected embedding dimension from ingestion service: %d (expected 768)",
			len(embedResp.Embedding),
		)
	}
	return embedResp.Embedding, nil
}

// GenerateMockEmbedding constructs a deterministic normalized unit vector from SHA-256 text hash.
func GenerateMockEmbedding(text string, dim int) []float32 {
	h := sha256.Sum256([]byte(text))
	vec := make([]float32, dim)
	var sumSq float64
	for i := 0; i < dim; i++ {
		byteVal := h[(i*7)%len(h)]
		v := float32((float64(byteVal)/127.5) - 1.0)
		vec[i] = v
		sumSq += float64(v * v)
	}
	norm := float32(math.Sqrt(sumSq))
	if norm > 0 {
		for i := 0; i < dim; i++ {
			vec[i] /= norm
		}
	}
	return vec
}
