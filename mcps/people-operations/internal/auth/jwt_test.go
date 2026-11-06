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
	"time"
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

func TestExtractEmailSecurityChecks(t *testing.T) {
	// 1. Insecure alg: "none"
	noneHeader := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"email":"victim@wso2.com"}`))
	unsignedNone := noneHeader + "." + payload + "."
	if em := ExtractEmailFromJWT(unsignedNone); em != "" {
		t.Fatalf("expected alg: none to be rejected, got %s", em)
	}

	// 2. Missing signature (2 parts only)
	twoParts := noneHeader + "." + payload
	if em := ExtractEmailFromJWT(twoParts); em != "" {
		t.Fatalf("expected 2-part token to be rejected, got %s", em)
	}

	// 3. Empty signature segment with valid alg
	rsHeader := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	emptySig := rsHeader + "." + payload + "."
	if em := ExtractEmailFromJWT(emptySig); em != "" {
		t.Fatalf("expected empty signature to be rejected, got %s", em)
	}

	// 4. Expired token
	expiredPayload := base64.RawURLEncoding.EncodeToString(
		[]byte(`{"email":"victim@wso2.com","exp":1000000000}`),
	)
	expiredToken := rsHeader + "." + expiredPayload + ".mock-sig"
	if em := ExtractEmailFromJWT(expiredToken); em != "" {
		t.Fatalf("expected expired token to be rejected, got %s", em)
	}

	// 5. Symmetric algorithm HS256 rejected
	hsHeader := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	hsToken := hsHeader + "." + payload + ".mock-sig"
	if em := ExtractEmailFromJWT(hsToken); em != "" {
		t.Fatalf("expected HS256 to be rejected, got %s", em)
	}
}

func TestGetPublicKeyUnknownKidThrottled(t *testing.T) {
	jwksCacheLock.Lock()
	lastJWKSFetch = time.Now()
	jwksCacheLock.Unlock()

	_, err := getPublicKey("https://example.com/oauth2/jwks", "unknown-kid-xyz")
	if err == nil {
		t.Fatal("expected error for unknown kid during cooldown, got nil")
	}
}

func TestExtractEmailFailsClosedWhenJWKSUnsetInProduction(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("ASGARDEO_JWKS_URL", "")
	t.Setenv("CHOREO_JWKS_URL", "")

	token := createMockToken(map[string]interface{}{
		"email": "victim@wso2.com",
	})
	if em := ExtractEmailFromJWT(token); em != "" {
		t.Fatalf("expected fail-closed rejection in production with missing JWKS, got %s", em)
	}
}

func TestExtractEmailProductionOverridesValidatorDisabled(t *testing.T) {
	t.Setenv("ENV", "production")
	t.Setenv("TOKEN_VALIDATOR_ENABLED", "false")
	t.Setenv("ASGARDEO_JWKS_URL", "")

	token := createMockToken(map[string]interface{}{
		"email": "victim@wso2.com",
	})
	if em := ExtractEmailFromJWT(token); em != "" {
		t.Fatalf("expected production to mandate verification and fail-closed, got %s", em)
	}
}

func TestResolveTokenRejectsDevStagingInProduction(t *testing.T) {
	t.Setenv("ENV", "production")
	t.Setenv("DEV_STAGING_TOKEN", "mock-secret-token")

	if tok := ResolveToken(""); tok != "" {
		t.Fatalf("expected ResolveToken to reject DEV_STAGING_TOKEN in production, got %s", tok)
	}
}
