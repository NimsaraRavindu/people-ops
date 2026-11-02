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

package auth

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func createMockToken(claims map[string]interface{}) string {
	header := map[string]string{"alg": "RS256", "typ": "JWT"}
	hBytes, _ := json.Marshal(header)
	cBytes, _ := json.Marshal(claims)

	hB64 := base64.RawURLEncoding.EncodeToString(hBytes)
	cB64 := base64.RawURLEncoding.EncodeToString(cBytes)

	return hB64 + "." + cB64 + ".mock-signature"
}

func testExtractEmailFromJWTValidEmail(t *testing.T) {
	token := createMockToken(map[string]interface{}{
		"email": "alice@example.com",
		"sub":   "user-123",
	})
	email := ExtractEmailFromJWT(token)
	if email != "alice@example.com" {
		t.Fatalf("expected alice@example.com, got %s", email)
	}
}

func testExtractEmailFromJWTSubFallback(t *testing.T) {
	token := createMockToken(map[string]interface{}{
		"sub": "bob@example.com",
	})
	email := ExtractEmailFromJWT(token)
	if email != "bob@example.com" {
		t.Fatalf("expected bob@example.com, got %s", email)
	}
}

func testExtractEmailFromJWTOtherDialects(t *testing.T) {
	// preferred_username
	tok1 := createMockToken(map[string]interface{}{
		"sub":                "uuid-1234",
		"preferred_username": "choreo-user@example.com",
	})
	if em := ExtractEmailFromJWT(tok1); em != "choreo-user@example.com" {
		t.Fatalf("expected choreo-user@example.com, got %s", em)
	}

	// wso2 claim dialect
	tok2 := createMockToken(map[string]interface{}{
		"sub": "uuid-5678",
		"http://wso2.org/claims/emailaddress": "wso2-user@wso2.com",
	})
	if em := ExtractEmailFromJWT(tok2); em != "wso2-user@wso2.com" {
		t.Fatalf("expected wso2-user@wso2.com, got %s", em)
	}
}

func TestExtractEmail(t *testing.T) {
	testExtractEmailFromJWTValidEmail(t)
	testExtractEmailFromJWTSubFallback(t)
	testExtractEmailFromJWTOtherDialects(t)
}

func TestExtractEmailInvalidTokenReturnsEmpty(t *testing.T) {
	if email := ExtractEmailFromJWT("invalid.jwt.token"); email != "" {
		t.Fatalf("expected empty string for invalid JWT, got %s", email)
	}
	if email := ExtractEmailFromJWT(""); email != "" {
		t.Fatalf("expected empty string for empty token, got %s", email)
	}
}
