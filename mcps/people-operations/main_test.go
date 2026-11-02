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
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wso2-open-operations/people-ops/mcps/people-operations/internal/handler"
)

func TestMuxRouting(t *testing.T) {
	cfg := handler.Config{
		LeaveBackendURL: "http://localhost:9090",
	}
	h := handler.NewHandler(cfg)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", h.Health)
	mux.HandleFunc("/register", h.Register)
	mux.HandleFunc("/tools/", h.Tools)

	// Test health route
	reqHealth := httptest.NewRequest(http.MethodGet, "/health", nil)
	wHealth := httptest.NewRecorder()
	mux.ServeHTTP(wHealth, reqHealth)
	if wHealth.Code != http.StatusOK {
		t.Fatalf("expected 200 for /health, got %d", wHealth.Code)
	}

	// Test register route
	reqReg := httptest.NewRequest(http.MethodGet, "/register", nil)
	wReg := httptest.NewRecorder()
	mux.ServeHTTP(wReg, reqReg)
	if wReg.Code != http.StatusOK {
		t.Fatalf("expected 200 for /register, got %d", wReg.Code)
	}
}

func TestGetEnv(t *testing.T) {
	t.Setenv("TEST_KEY", "custom_val")
	if val := getEnv("TEST_KEY", "default"); val != "custom_val" {
		t.Fatalf("expected custom_val, got %s", val)
	}
	if val := getEnv("NON_EXISTENT_KEY", "default"); val != "default" {
		t.Fatalf("expected default, got %s", val)
	}
}
