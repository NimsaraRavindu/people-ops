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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wso2-open-operations/people-ops/mcps/policy/internal/model"
)

type mockEmbedder struct {
	err error
	vec []float32
}

func (m *mockEmbedder) EmbedQuery(ctx context.Context, text string) ([]float32, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.vec != nil {
		return m.vec, nil
	}
	return []float32{0.1, 0.2, 0.3}, nil
}

type mockSearcher struct {
	pingErr          error
	searchErr        error
	results          []model.SearchResultItem
	capturedFolderID string
	capturedTopK     int
}

func (m *mockSearcher) Ping(ctx context.Context) error {
	return m.pingErr
}

func (m *mockSearcher) Close() {}

func (m *mockSearcher) SearchChunks(
	ctx context.Context,
	embedding []float32,
	folderID string,
	topK int,
) ([]model.SearchResultItem, error) {
	m.capturedFolderID = folderID
	m.capturedTopK = topK
	if m.searchErr != nil {
		return nil, m.searchErr
	}
	return m.results, nil
}

func newTestHandler(searcher *mockSearcher, embedder *mockEmbedder) *Handler {
	return NewHandler(
		Config{
			PolicyDriveFolderID:   "policy-folder-123",
			TrainingDriveFolderID: "training-folder-456",
			DevMockUserEnabled:    true,
		},
		embedder,
		searcher,
	)
}

func TestHealthConnected(t *testing.T) {
	searcher := &mockSearcher{}
	h := newTestHandler(searcher, &mockEmbedder{})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	h.Health(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["database"] != "connected" {
		t.Fatalf("expected database connected, got %s", body["database"])
	}
}

func TestHealthDegradedWhenDBDown(t *testing.T) {
	searcher := &mockSearcher{pingErr: fmt.Errorf("connection refused")}
	h := newTestHandler(searcher, &mockEmbedder{})

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	h.Health(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 Service Unavailable, got %d", resp.StatusCode)
	}

	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["database"] != "unreachable" {
		t.Fatalf("expected database unreachable, got %s", body["database"])
	}
}

func TestRegisterEndpoint(t *testing.T) {
	h := newTestHandler(&mockSearcher{}, &mockEmbedder{})

	req := httptest.NewRequest(http.MethodGet, "/register", nil)
	w := httptest.NewRecorder()
	h.Register(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var buckets []model.BucketKit
	json.NewDecoder(resp.Body).Decode(&buckets)
	if len(buckets) != 1 || buckets[0].Bucket != "PO_POLICIES" {
		t.Fatalf("unexpected buckets response: %v", buckets)
	}
}

func TestUnknownToolReturns404(t *testing.T) {
	h := newTestHandler(&mockSearcher{}, &mockEmbedder{})

	req := httptest.NewRequest(http.MethodPost, "/tools/unknown_tool", bytes.NewBufferString(`{"query":"test"}`))
	req.Header.Set("Authorization", "Bearer valid-token")
	w := httptest.NewRecorder()
	h.Tools(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found, got %d", resp.StatusCode)
	}
}

func TestToolsMissingTokenReturns401(t *testing.T) {
	h := NewHandler(
		Config{DevMockUserEnabled: false},
		&mockEmbedder{},
		&mockSearcher{},
	)
	t.Setenv("DEV_MOCK_USER_ENABLED", "false")

	req := httptest.NewRequest(http.MethodPost, "/tools/query_policy", bytes.NewBufferString(`{"query":"test"}`))
	w := httptest.NewRecorder()
	h.Tools(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", resp.StatusCode)
	}
}

func TestQueryPolicySuccess(t *testing.T) {
	searcher := &mockSearcher{
		results: []model.SearchResultItem{
			{Document: "LeavePolicy.pdf", Excerpt: "Casual leave info", Similarity: 0.89},
		},
	}
	h := newTestHandler(searcher, &mockEmbedder{})

	payload := `{"query":"What is the casual leave policy?","top_k":3}`
	req := httptest.NewRequest(http.MethodPost, "/tools/query_policy", bytes.NewBufferString(payload))
	req.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	h.Tools(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	if searcher.capturedFolderID != "policy-folder-123" {
		t.Fatalf("expected folder policy-folder-123, got %s", searcher.capturedFolderID)
	}
	if searcher.capturedTopK != 3 {
		t.Fatalf("expected topK 3, got %d", searcher.capturedTopK)
	}

	var res model.ToolResponse
	json.NewDecoder(resp.Body).Decode(&res)
	if res.SourceCount != 1 || res.Results[0].Document != "LeavePolicy.pdf" {
		t.Fatalf("unexpected search response: %v", res)
	}
}

func TestQueryTrainingSuccess(t *testing.T) {
	searcher := &mockSearcher{
		results: []model.SearchResultItem{
			{Document: "BallerinaPlaybook.pdf", Excerpt: "Exam guide info", Similarity: 0.92},
		},
	}
	h := newTestHandler(searcher, &mockEmbedder{})

	payload := `{"query":"How do I get a certification voucher?","top_k":8}`
	req := httptest.NewRequest(http.MethodPost, "/tools/query_training", bytes.NewBufferString(payload))
	req.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	h.Tools(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	if searcher.capturedFolderID != "training-folder-456" {
		t.Fatalf("expected folder training-folder-456, got %s", searcher.capturedFolderID)
	}
	if searcher.capturedTopK != 8 {
		t.Fatalf("expected topK 8, got %d", searcher.capturedTopK)
	}
}
