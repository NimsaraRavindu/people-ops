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
	"io"
	"net/http"
	"testing"
)

type roundTripFunc func(req *http.Request) *http.Response

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req), nil
}

func TestGenerateMockEmbedding(t *testing.T) {
	vec := GenerateMockEmbedding("test query", 768)
	if len(vec) != 768 {
		t.Fatalf("expected 768 dimensions, got %d", len(vec))
	}
}

func TestEmbedViaIngestion(t *testing.T) {
	var capturedToken string
	mockRT := roundTripFunc(func(req *http.Request) *http.Response {
		capturedToken = req.Header.Get("X-Internal-Token")
		if req.URL.Path != "/embed" {
			return &http.Response{StatusCode: http.StatusNotFound}
		}
		vec := make([]float32, 768)
		vec[0] = 0.1
		var buf bytes.Buffer
		buf.WriteString(`{"embedding":[0.1`)
		for i := 1; i < 768; i++ {
			buf.WriteString(`,0.0`)
		}
		buf.WriteString(`]}`)
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(&buf),
			Header:     make(http.Header),
		}
	})

	client := NewClient(Config{
		IngestionURL:    "http://mock-ingestion:8080",
		IngestionSecret: "secret-test-key",
		HTTPClient:      &http.Client{Transport: mockRT},
	})

	vec, err := client.EmbedQuery(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vec) != 768 || vec[0] != 0.1 {
		t.Fatalf("unexpected embedding vector length: %d", len(vec))
	}
	if capturedToken != "secret-test-key" {
		t.Fatalf("expected X-Internal-Token header, got %s", capturedToken)
	}
}

func TestFallbackMockEmbeddingDisabledInProduction(t *testing.T) {
	client := NewClient(Config{
		AllowMockFallback: false,
	})
	_, err := client.EmbedQuery(context.Background(), "test")
	if err == nil {
		t.Fatalf("expected error when no provider is configured and mock is disabled")
	}
}
