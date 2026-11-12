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
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func createMockToken(claims map[string]interface{}) string {
	if _, ok := claims["exp"]; !ok {
		claims["exp"] = time.Now().Add(1 * time.Hour).Unix()
	}
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
	email := ExtractEmailFromJWT(context.Background(), token)
	if email != "alice@example.com" {
		t.Fatalf("expected alice@example.com, got %s", email)
	}
}

func testExtractEmailFromJWTSubFallback(t *testing.T) {
	token := createMockToken(map[string]interface{}{
		"sub": "bob@example.com",
	})
	email := ExtractEmailFromJWT(context.Background(), token)
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
	if em := ExtractEmailFromJWT(context.Background(), tok1); em != "choreo-user@example.com" {
		t.Fatalf("expected choreo-user@example.com, got %s", em)
	}

	// wso2 claim dialect
	tok2 := createMockToken(map[string]interface{}{
		"sub": "uuid-5678",
		"http://wso2.org/claims/emailaddress": "wso2-user@wso2.com",
	})
	if em := ExtractEmailFromJWT(context.Background(), tok2); em != "wso2-user@wso2.com" {
		t.Fatalf("expected wso2-user@wso2.com, got %s", em)
	}
}

func TestExtractEmail(t *testing.T) {
	testExtractEmailFromJWTValidEmail(t)
	testExtractEmailFromJWTSubFallback(t)
	testExtractEmailFromJWTOtherDialects(t)
}

func TestExtractEmailInvalidTokenReturnsEmpty(t *testing.T) {
	if email := ExtractEmailFromJWT(context.Background(), "invalid.jwt.token"); email != "" {
		t.Fatalf("expected empty string for invalid JWT, got %s", email)
	}
	if email := ExtractEmailFromJWT(context.Background(), ""); email != "" {
		t.Fatalf("expected empty string for empty token, got %s", email)
	}
}

func TestExtractEmailSecurityChecks(t *testing.T) {
	// 1. Insecure alg: "none"
	noneHeader := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"email":"victim@wso2.com"}`))
	unsignedNone := noneHeader + "." + payload + "."
	if em := ExtractEmailFromJWT(context.Background(), unsignedNone); em != "" {
		t.Fatalf("expected alg: none to be rejected, got %s", em)
	}

	// 2. Missing signature (2 parts only)
	twoParts := noneHeader + "." + payload
	if em := ExtractEmailFromJWT(context.Background(), twoParts); em != "" {
		t.Fatalf("expected 2-part token to be rejected, got %s", em)
	}

	// 3. Empty signature segment with valid alg
	rsHeader := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	emptySig := rsHeader + "." + payload + "."
	if em := ExtractEmailFromJWT(context.Background(), emptySig); em != "" {
		t.Fatalf("expected empty signature to be rejected, got %s", em)
	}

	// 4. Expired token
	expiredPayload := base64.RawURLEncoding.EncodeToString(
		[]byte(`{"email":"victim@wso2.com","exp":1000000000}`),
	)
	expiredToken := rsHeader + "." + expiredPayload + ".mock-sig"
	if em := ExtractEmailFromJWT(context.Background(), expiredToken); em != "" {
		t.Fatalf("expected expired token to be rejected, got %s", em)
	}

	// 5. Symmetric algorithm HS256 rejected
	validExpPayload := base64.RawURLEncoding.EncodeToString(
		[]byte(fmt.Sprintf(`{"email":"victim@wso2.com","exp":%d}`, time.Now().Add(time.Hour).Unix())),
	)
	hsHeader := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	hsToken := hsHeader + "." + validExpPayload + ".mock-sig"
	if em := ExtractEmailFromJWT(context.Background(), hsToken); em != "" {
		t.Fatalf("expected HS256 to be rejected, got %s", em)
	}

	// 6. Missing exp claim
	noExpPayload := base64.RawURLEncoding.EncodeToString([]byte(`{"email":"victim@wso2.com"}`))
	noExpToken := rsHeader + "." + noExpPayload + ".mock-sig"
	if em := ExtractEmailFromJWT(context.Background(), noExpToken); em != "" {
		t.Fatalf("expected missing exp token to be rejected, got %s", em)
	}

	// 7. Non-positive exp claim
	zeroExpPayload := base64.RawURLEncoding.EncodeToString([]byte(`{"email":"victim@wso2.com","exp":0}`))
	zeroExpToken := rsHeader + "." + zeroExpPayload + ".mock-sig"
	if em := ExtractEmailFromJWT(context.Background(), zeroExpToken); em != "" {
		t.Fatalf("expected exp: 0 token to be rejected, got %s", em)
	}
}

func TestExtractEmailFailsClosedWhenJWKSUnsetInProduction(t *testing.T) {
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("CHOREO_JWKS_URL", "")

	token := createMockToken(map[string]interface{}{
		"email": "victim@wso2.com",
	})
	if em := ExtractEmailFromJWT(context.Background(), token); em != "" {
		t.Fatalf("expected fail-closed rejection in production with missing JWKS, got %s", em)
	}
}

func TestExtractEmailProductionOverridesValidatorDisabled(t *testing.T) {
	t.Setenv("ENV", "production")
	t.Setenv("TOKEN_VALIDATOR_ENABLED", "false")
	t.Setenv("CHOREO_JWKS_URL", "")

	token := createMockToken(map[string]interface{}{
		"email": "victim@wso2.com",
	})
	if em := ExtractEmailFromJWT(context.Background(), token); em != "" {
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

type testKeyEntry struct {
	Kid string
	Key *rsa.PublicKey
}

func makeJWKSResponse(entries []testKeyEntry) string {
	type jwkItem struct {
		Kty string `json:"kty"`
		Kid string `json:"kid,omitempty"`
		Use string `json:"use"`
		Alg string `json:"alg"`
		N   string `json:"n"`
		E   string `json:"e"`
	}
	var items []jwkItem
	for _, entry := range entries {
		nStr := base64.RawURLEncoding.EncodeToString(entry.Key.N.Bytes())
		eBytes := big.NewInt(int64(entry.Key.E)).Bytes()
		eStr := base64.RawURLEncoding.EncodeToString(eBytes)
		items = append(items, jwkItem{
			Kty: "RSA",
			Kid: entry.Kid,
			Use: "sig",
			Alg: "RS256",
			N:   nStr,
			E:   eStr,
		})
	}
	data, _ := json.Marshal(map[string]interface{}{"keys": items})
	return string(data)
}

func TestGetPublicKeyURLIsolationAndNoKidFallback(t *testing.T) {
	ResetJWKSCache()

	keyA, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate keyA: %v", err)
	}
	keyB, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate keyB: %v", err)
	}

	// Server A has 1 key with kid "common-kid"
	serverA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(makeJWKSResponse([]testKeyEntry{
			{Kid: "common-kid", Key: &keyA.PublicKey},
		})))
	}))
	defer serverA.Close()

	// Server B has 2 keys: "common-kid" (with keyB) and "other-kid" (with keyA)
	serverB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(makeJWKSResponse([]testKeyEntry{
			{Kid: "common-kid", Key: &keyB.PublicKey},
			{Kid: "other-kid", Key: &keyA.PublicKey},
		})))
	}))
	defer serverB.Close()

	// Server C has exactly 1 key without kid
	serverC := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(makeJWKSResponse([]testKeyEntry{
			{Kid: "", Key: &keyA.PublicKey},
		})))
	}))
	defer serverC.Close()

	ctx := context.Background()

	// 1. URL Isolation: Server A and Server B both use kid "common-kid", but return distinct keys
	pubA, err := getPublicKey(ctx, serverA.URL, "common-kid")
	if err != nil {
		t.Fatalf("expected key from serverA, got err: %v", err)
	}
	pubB, err := getPublicKey(ctx, serverB.URL, "common-kid")
	if err != nil {
		t.Fatalf("expected key from serverB, got err: %v", err)
	}
	if pubA.N.Cmp(pubB.N) == 0 {
		t.Fatal("expected serverA and serverB keys to differ, but got identical keys (collision!)")
	}

	// 2. Safe single-key fallback when token omits kid
	pubC1, err := getPublicKey(ctx, serverC.URL, "")
	if err != nil {
		t.Fatalf("expected single-key fallback to succeed on serverC, got err: %v", err)
	}
	if pubC1.N.Cmp(keyA.N) != 0 {
		t.Fatal("expected keyA from serverC single-key fallback")
	}

	// 3. Cooldown persistence: calling serverC again with empty kid during cooldown resolves from cache
	pubC2, err := getPublicKey(ctx, serverC.URL, "")
	if err != nil {
		t.Fatalf("expected single-key resolution to succeed during cooldown from cache, got err: %v", err)
	}
	if pubC2 != pubC1 {
		t.Fatal("expected same cached key instance during cooldown")
	}

	// 4. Token without kid fails when JWKS has multiple keys
	_, err = getPublicKey(ctx, serverB.URL, "")
	if err == nil {
		t.Fatal("expected token without kid to fail on serverB with multiple keys, got nil")
	}
}

func TestGetPublicKeyFailedFetchThrottled(t *testing.T) {
	ResetJWKSCache()

	var reqCount int32
	serverFail := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&reqCount, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer serverFail.Close()

	ctx := context.Background()

	// First attempt fails due to HTTP 500
	_, err1 := getPublicKey(ctx, serverFail.URL, "kid1")
	if err1 == nil {
		t.Fatal("expected error on HTTP 500, got nil")
	}
	if atomic.LoadInt32(&reqCount) != 1 {
		t.Fatalf("expected exactly 1 HTTP request, got %d", reqCount)
	}

	// Second attempt within cooldown is throttled immediately without hitting the network
	_, err2 := getPublicKey(ctx, serverFail.URL, "kid1")
	if err2 == nil {
		t.Fatal("expected throttled error on second attempt, got nil")
	}
	if atomic.LoadInt32(&reqCount) != 1 {
		t.Fatalf("expected no additional HTTP request during cooldown, got %d", reqCount)
	}
}

func TestGetPublicKeyContextCancellation(t *testing.T) {
	ResetJWKSCache()

	serverSlow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"keys":[]}`))
	}))
	defer serverSlow.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel context immediately before calling

	_, err := getPublicKey(ctx, serverSlow.URL, "kid1")
	if err == nil {
		t.Fatal("expected error on cancelled context, got nil")
	}
}

func TestGetPublicKeyCancellationDuringFetchDoesNotThrottleSubsequentRequests(t *testing.T) {
	ResetJWKSCache()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	fetchStarted := make(chan struct{}, 1)
	unblockServer := make(chan struct{})

	var fetchCount int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count := atomic.AddInt32(&fetchCount, 1)
		if count == 1 {
			select {
			case fetchStarted <- struct{}{}:
			default:
			}
			<-unblockServer
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(makeJWKSResponse([]testKeyEntry{
			{Kid: "key-1", Key: &key.PublicKey},
		})))
	}))
	defer server.Close()
	defer func() {
		select {
		case <-unblockServer:
		default:
			close(unblockServer)
		}
	}()

	ctx1, cancel1 := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		_, err := getPublicKey(ctx1, server.URL, "key-1")
		errCh <- err
	}()

	// Wait until the server receives caller 1's request
	<-fetchStarted
	// Cancel caller 1's context while the fetch is in-flight
	cancel1()

	err1 := <-errCh
	if err1 == nil || !errors.Is(err1, context.Canceled) {
		t.Fatalf("expected context.Canceled for caller 1, got %v", err1)
	}

	// Unblock server to finish the cancelled request in the background
	close(unblockServer)

	// Subsequent request with valid context on cold cache must succeed and not be throttled
	ctx2 := context.Background()
	pub, err2 := getPublicKey(ctx2, server.URL, "key-1")
	if err2 != nil {
		t.Fatalf("expected subsequent request to succeed without throttle, got %v", err2)
	}
	if pub.N.Cmp(key.N) != 0 {
		t.Fatal("expected matching public key for subsequent request")
	}
}

func TestGetPublicKeySemaphoreCancellationWhileWaiting(t *testing.T) {
	ResetJWKSCache()

	unblockFetch := make(chan struct{})
	fetchStarted := make(chan struct{}, 1)
	caller1Done := make(chan struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case fetchStarted <- struct{}{}:
		default:
		}
		<-unblockFetch
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"keys":[]}`))
	}))
	defer server.Close()

	// Caller 1 starts slow fetch and holds the semaphore
	go func() {
		defer close(caller1Done)
		_, _ = getPublicKey(context.Background(), server.URL, "key-1")
	}()

	<-fetchStarted

	// Caller 2 tries to acquire semaphore with a context that gets canceled
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()

	_, err2 := getPublicKey(ctx2, server.URL, "key-1")
	if !errors.Is(err2, context.Canceled) {
		t.Fatalf("expected context.Canceled while waiting for semaphore, got %v", err2)
	}

	// Unblock caller 1 and wait for it to release semaphore before ending test
	close(unblockFetch)
	<-caller1Done
}

func TestGetPublicKeyStaleKeyServedAcrossRepeatedRequestsDuringOutage(t *testing.T) {
	ResetJWKSCache()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	var failServer int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.LoadInt32(&failServer) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(makeJWKSResponse([]testKeyEntry{
			{Kid: "stale-kid", Key: &key.PublicKey},
		})))
	}))
	defer server.Close()

	ctx := context.Background()

	// Initial fetch succeeds and populates cache
	pub1, err := getPublicKey(ctx, server.URL, "stale-kid")
	if err != nil {
		t.Fatalf("expected successful initial fetch, got %v", err)
	}

	// Trigger outage and simulate expired cache
	atomic.StoreInt32(&failServer, 1)

	jwksCacheLock.Lock()
	cache := jwksURLCaches[server.URL]
	if cache != nil {
		cache.lastSuccess = time.Now().Add(-2 * time.Hour) // expired
		cache.lastAttempt = time.Time{}                     // ready to refetch
	}
	jwksCacheLock.Unlock()

	// 1st request after expiration triggers fetch, encounters 500, and returns stale key
	pub2, err := getPublicKey(ctx, server.URL, "stale-kid")
	if err != nil {
		t.Fatalf("expected stale key fallback on 1st failed refetch, got %v", err)
	}
	if pub2.N.Cmp(pub1.N) != 0 {
		t.Fatal("expected matching stale key on 1st failed refetch")
	}

	// 2nd request during cooldown MUST also return stale key (not throttled error)
	pub3, err := getPublicKey(ctx, server.URL, "stale-kid")
	if err != nil {
		t.Fatalf("expected stale key on 2nd request during cooldown, got %v", err)
	}
	if pub3.N.Cmp(pub1.N) != 0 {
		t.Fatal("expected matching stale key on 2nd request during cooldown")
	}

	// 3rd request during cooldown MUST also return stale key
	pub4, err := getPublicKey(ctx, server.URL, "stale-kid")
	if err != nil {
		t.Fatalf("expected stale key on 3rd request during cooldown, got %v", err)
	}
	if pub4.N.Cmp(pub1.N) != 0 {
		t.Fatal("expected matching stale key on 3rd request during cooldown")
	}
}

func TestEnvironmentPolicyQAAndUnrecognizedRequireVerification(t *testing.T) {
	token := createMockToken(map[string]interface{}{
		"email": "user@wso2.com",
	})

	testCases := []struct {
		name string
		env  string
	}{
		{name: "QA environment", env: "qa"},
		{name: "UAT environment", env: "uat"},
		{name: "Unrecognized environment", env: "custom_cloud_staging"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ENVIRONMENT", tc.env)
			t.Setenv("TOKEN_VALIDATOR_ENABLED", "false")
			t.Setenv("CHOREO_JWKS_URL", "")
			t.Setenv("DEV_STAGING_TOKEN", "mock-staging-secret")

			// Verification must not be skipped even with TOKEN_VALIDATOR_ENABLED=false
			if email := ExtractEmailFromJWT(context.Background(), token); email != "" {
				t.Fatalf("expected verification required in %s, got %s", tc.env, email)
			}

			// Fallback token must not be permitted in non-dev environment
			if tok := ResolveToken(""); tok != "" {
				t.Fatalf("expected ResolveToken to reject fallback in %s, got %s", tc.env, tok)
			}
		})
	}
}

func TestEnvironmentPolicyUnsetEnvironmentDefaultDeny(t *testing.T) {
	t.Setenv("ENVIRONMENT", "")
	t.Setenv("ENV", "")
	t.Setenv("APP_ENV", "")
	t.Setenv("FORCE_NON_DEV_ENV", "true")
	t.Setenv("TOKEN_VALIDATOR_ENABLED", "false")
	t.Setenv("CHOREO_JWKS_URL", "")
	t.Setenv("DEV_STAGING_TOKEN", "mock-staging-secret")

	token := createMockToken(map[string]interface{}{
		"email": "user@wso2.com",
	})

	if email := ExtractEmailFromJWT(context.Background(), token); email != "" {
		t.Fatalf("expected verification required when environment is unset, got %s", email)
	}

	if tok := ResolveToken(""); tok != "" {
		t.Fatalf("expected ResolveToken to reject fallback when environment is unset, got %s", tok)
	}
}

func TestExtractEmailForgedTokenWithConfiguredJWKSRejected(t *testing.T) {
	ResetJWKSCache()

	realKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate real key: %v", err)
	}
	attackerKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate attacker key: %v", err)
	}

	// JWKS endpoint serves realKey with kid "trusted-kid"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(makeJWKSResponse([]testKeyEntry{
			{Kid: "trusted-kid", Key: &realKey.PublicKey},
		})))
	}))
	defer server.Close()

	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("CHOREO_JWKS_URL", server.URL)

	// Attacker creates a token signed with attackerKey but referencing "trusted-kid"
	forgedToken := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"email": "forged-admin@wso2.com",
		"sub":   "admin-id",
		"exp":   time.Now().Add(1 * time.Hour).Unix(),
	})
	forgedToken.Header["kid"] = "trusted-kid"
	forgedTokenString, err := forgedToken.SignedString(attackerKey)
	if err != nil {
		t.Fatalf("failed to sign forged token: %v", err)
	}

	// Cryptographic verification against JWKS public key must fail and reject the forged token
	if em := ExtractEmailFromJWT(context.Background(), forgedTokenString); em != "" {
		t.Fatalf("expected forged token to be rejected, got %s", em)
	}

	// Legitimate token signed with realKey must be accepted
	validToken := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
		"email": "legit-user@wso2.com",
		"sub":   "user-id",
		"exp":   time.Now().Add(1 * time.Hour).Unix(),
	})
	validToken.Header["kid"] = "trusted-kid"
	validTokenString, err := validToken.SignedString(realKey)
	if err != nil {
		t.Fatalf("failed to sign legitimate token: %v", err)
	}

	if em := ExtractEmailFromJWT(context.Background(), validTokenString); em != "legit-user@wso2.com" {
		t.Fatalf("expected legitimate token to return legit-user@wso2.com, got %s", em)
	}
}
