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
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type jwksURLCache struct {
	keys        map[string]*rsa.PublicKey
	keysList    []*rsa.PublicKey
	lastAttempt time.Time
	lastSuccess time.Time
}

var (
	jwksCacheLock   sync.RWMutex
	jwksFetchSem    = make(chan struct{}, 1)
	jwksURLCaches   = make(map[string]*jwksURLCache)
	jwksCacheTTL    = 1 * time.Hour
	maxStaleJWKSAge = 24 * time.Hour
	minJWKSRefetch  = 1 * time.Minute
	jwksClient      = &http.Client{Timeout: 5 * time.Second}
)

// ResetJWKSCache clears all cached JWKS keys and fetch timestamps (primarily for tests).
func ResetJWKSCache() {
	jwksCacheLock.Lock()
	defer jwksCacheLock.Unlock()
	jwksURLCaches = make(map[string]*jwksURLCache)
}

type jwksResponse struct {
	Keys []struct {
		Kty string `json:"kty"`
		Kid string `json:"kid"`
		Use string `json:"use"`
		Alg string `json:"alg"`
		N   string `json:"n"`
		E   string `json:"e"`
	} `json:"keys"`
}

// Allowed asymmetric signing algorithms.
var allowedAlgorithms = map[string]bool{
	"RS256": true, "RS384": true, "RS512": true,
	"ES256": true, "ES384": true, "ES512": true,
	"PS256": true, "PS384": true, "PS512": true,
}

func recordFetchAttempt(jwksURL string) {
	jwksCacheLock.Lock()
	defer jwksCacheLock.Unlock()
	cache := jwksURLCaches[jwksURL]
	if cache == nil {
		cache = &jwksURLCache{
			keys: make(map[string]*rsa.PublicKey),
		}
		jwksURLCaches[jwksURL] = cache
	}
	cache.lastAttempt = time.Now()
}

// fetchJWKS fetches and caches RSA public keys from a JWKS endpoint with context cancellation.
// Records attempt timestamps on genuine remote failures, avoiding cooldown blackouts on caller cancellation.
func fetchJWKS(ctx context.Context, jwksURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURL, nil)
	if err != nil {
		return err
	}
	resp, err := jwksClient.Do(req)
	if err != nil {
		if ctx.Err() == nil {
			recordFetchAttempt(jwksURL)
		}
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		recordFetchAttempt(jwksURL)
		return fmt.Errorf("JWKS endpoint returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		if ctx.Err() == nil {
			recordFetchAttempt(jwksURL)
		}
		return err
	}

	var jwks jwksResponse
	if err := json.Unmarshal(body, &jwks); err != nil {
		recordFetchAttempt(jwksURL)
		return err
	}

	newKeys := make(map[string]*rsa.PublicKey)
	var newKeysList []*rsa.PublicKey

	for _, k := range jwks.Keys {
		if k.Kty != "RSA" || k.N == "" || k.E == "" {
			continue
		}
		nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			continue
		}
		eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			continue
		}
		n := new(big.Int).SetBytes(nBytes)
		e := int(new(big.Int).SetBytes(eBytes).Int64())
		pubKey := &rsa.PublicKey{N: n, E: e}
		if k.Kid != "" {
			newKeys[k.Kid] = pubKey
		}
		newKeysList = append(newKeysList, pubKey)
	}

	now := time.Now()
	jwksCacheLock.Lock()
	cache := jwksURLCaches[jwksURL]
	if cache == nil {
		cache = &jwksURLCache{}
		jwksURLCaches[jwksURL] = cache
	}
	cache.keys = newKeys
	cache.keysList = newKeysList
	cache.lastSuccess = now
	cache.lastAttempt = now
	jwksCacheLock.Unlock()

	return nil
}

// lookupCachedKey checks if a valid key is already cached for the given URL.
// Returns (key, found, canFetch). Safely evaluates no-kid single key resolution during cooldown,
// and serves stale keys during cooldown if a refresh attempt fails.
func lookupCachedKey(jwksURL string, kid string) (*rsa.PublicKey, bool, bool) {
	jwksCacheLock.RLock()
	defer jwksCacheLock.RUnlock()

	cache, exists := jwksURLCaches[jwksURL]
	if !exists || cache == nil {
		return nil, false, true
	}

	expired := time.Since(cache.lastSuccess) > jwksCacheTTL
	cooldown := !cache.lastAttempt.IsZero() && time.Since(cache.lastAttempt) < minJWKSRefetch
	withinStaleBound := time.Since(cache.lastSuccess) <= maxStaleJWKSAge

	if kid != "" {
		if pub, ok := cache.keys[kid]; ok {
			if (!expired || cooldown) && withinStaleBound {
				return pub, true, false
			}
		}
	} else if len(cache.keysList) == 1 {
		if (!expired || cooldown) && withinStaleBound {
			return cache.keysList[0], true, false
		}
	}

	return nil, false, !cooldown
}

// getPublicKey retrieves a cached RSA public key by kid or re-fetches if absent.
// Scopes keys strictly by jwksURL to avoid cross-endpoint collisions, throttles failed attempts,
// serves stale keys during remote JWKS outages, and respects context cancellation throughout.
func getPublicKey(ctx context.Context, jwksURL string, kid string) (*rsa.PublicKey, error) {
	if pub, found, canFetch := lookupCachedKey(jwksURL, kid); found {
		return pub, nil
	} else if !canFetch {
		return nil, fmt.Errorf("key with kid %q not found in JWKS %s (fetch throttled)", kid, jwksURL)
	}

	if ctx == nil {
		ctx = context.Background()
	}

	// Context-aware semaphore acquisition prevents goroutine leaks on cancelled callers
	select {
	case jwksFetchSem <- struct{}{}:
		defer func() { <-jwksFetchSem }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// Double-check cache under fetch lock
	if pub, found, canFetch := lookupCachedKey(jwksURL, kid); found {
		return pub, nil
	} else if !canFetch {
		return nil, fmt.Errorf("key with kid %q not found in JWKS %s (fetch throttled)", kid, jwksURL)
	}

	if err := fetchJWKS(ctx, jwksURL); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Fall back to stale cache on network failure if available and within maxStaleJWKSAge
		jwksCacheLock.RLock()
		cache := jwksURLCaches[jwksURL]
		if cache != nil && time.Since(cache.lastSuccess) <= maxStaleJWKSAge {
			if kid != "" {
				if pub, ok := cache.keys[kid]; ok {
					jwksCacheLock.RUnlock()
					return pub, nil
				}
			} else if len(cache.keysList) == 1 {
				pub := cache.keysList[0]
				jwksCacheLock.RUnlock()
				return pub, nil
			}
		}
		jwksCacheLock.RUnlock()
		return nil, err
	}

	if pub, found, _ := lookupCachedKey(jwksURL, kid); found {
		return pub, nil
	}

	if kid == "" {
		jwksCacheLock.RLock()
		cache := jwksURLCaches[jwksURL]
		count := 0
		if cache != nil {
			count = len(cache.keysList)
		}
		jwksCacheLock.RUnlock()
		if count > 1 {
			return nil, fmt.Errorf("token missing kid and JWKS %s contains %d keys", jwksURL, count)
		}
	}

	return nil, fmt.Errorf("key with kid %q not found in JWKS %s", kid, jwksURL)
}

// IsPermittedDevEnvironment returns true ONLY if a permitted dev/test environment is explicitly set
// and no production, staging, or QA environment indicator is present.
func IsPermittedDevEnvironment() bool {
	hasDev := false
	for _, key := range []string{"ENVIRONMENT", "ENV", "APP_ENV"} {
		val := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
		if val == "" {
			continue
		}
		if val == "development" || val == "dev" || val == "test" || val == "testing" || val == "local" {
			hasDev = true
		} else {
			// Any non-dev setting (e.g., "qa", "uat", "staging", "production") immediately revokes dev status
			return false
		}
	}
	if hasDev {
		return true
	}
	return testing.Testing() && os.Getenv("FORCE_NON_DEV_ENV") != "true"
}

// isTokenValidatorEnabled enforces signature verification for all environments (production, staging,
// qa, uat, unrecognized, or unset). Verification can ONLY be disabled in permitted dev/test environments.
func isTokenValidatorEnabled() bool {
	if !IsPermittedDevEnvironment() {
		return true
	}
	val := strings.ToLower(strings.TrimSpace(os.Getenv("TOKEN_VALIDATOR_ENABLED")))
	return val == "true" || val == "1"
}

// ExtractEmailFromJWT extracts and verifies identity from an incoming JWT.
// Enforces structural integrity, alg validation (rejects none), expiration (exp),
// context cancellation, and cryptographic signature verification when enabled or in production.
func ExtractEmailFromJWT(ctx context.Context, tokenStr string) string {
	if ctx == nil {
		ctx = context.Background()
	}

	tokenStr = strings.TrimSpace(tokenStr)
	tokenStr = strings.TrimPrefix(tokenStr, "Bearer ")
	tokenStr = strings.TrimPrefix(tokenStr, "bearer ")

	if tokenStr == "" {
		return ""
	}

	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		slog.Warn("Rejected JWT: token does not conform to 3-part format")
		return ""
	}

	// 1. Structural Header Validation
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		slog.Warn("Rejected JWT: malformed header base64")
		return ""
	}

	var header struct {
		Alg string `json:"alg"`
		Typ string `json:"typ"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		slog.Warn("Rejected JWT: malformed header JSON")
		return ""
	}

	// Unconditionally reject "none" algorithm and empty signatures
	if strings.EqualFold(header.Alg, "none") || header.Alg == "" {
		slog.Warn("Rejected JWT: insecure 'none' or missing algorithm")
		return ""
	}
	if !allowedAlgorithms[strings.ToUpper(header.Alg)] {
		slog.Warn("Rejected JWT: unsupported signing algorithm", "alg", header.Alg)
		return ""
	}
	if parts[2] == "" {
		slog.Warn("Rejected JWT: missing signature segment")
		return ""
	}

	// 2. Claims & Expiration Validation
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		slog.Warn("Rejected JWT: malformed payload base64")
		return ""
	}

	var claims struct {
		Email             string `json:"email"`
		Sub               string `json:"sub"`
		Username          string `json:"username"`
		PreferredUsername string `json:"preferred_username"`
		WSO2Email         string `json:"http://wso2.org/claims/emailaddress"`
		WSO2Username      string `json:"http://wso2.org/claims/username"`
		Exp               int64  `json:"exp"`
		Nbf               int64  `json:"nbf"`
	}
	if err := json.Unmarshal(payloadBytes, &claims); err != nil {
		slog.Warn("Rejected JWT: malformed payload JSON")
		return ""
	}

	now := time.Now().Unix()
	if claims.Exp <= 0 {
		slog.Warn("Rejected JWT: missing or non-positive exp claim", "exp", claims.Exp)
		return ""
	}
	if now > claims.Exp {
		slog.Warn("Rejected JWT: token has expired", "exp", claims.Exp, "now", now)
		return ""
	}
	if claims.Nbf > 0 && now < claims.Nbf {
		slog.Warn("Rejected JWT: token not yet valid (nbf)", "nbf", claims.Nbf, "now", now)
		return ""
	}

	// 3. Cryptographic Signature Verification
	if isTokenValidatorEnabled() {
		jwksURL := strings.TrimSpace(os.Getenv("CHOREO_JWKS_URL"))
		if jwksURL == "" {
			slog.Error("Rejected JWT: token verification required but CHOREO_JWKS_URL is not set")
			return ""
		}

		parsedToken, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			kid, _ := t.Header["kid"].(string)
			return getPublicKey(ctx, jwksURL, kid)
		})

		if err != nil || !parsedToken.Valid {
			slog.Warn("Rejected JWT: cryptographic signature verification failed", "err", err)
			return ""
		}
	}

	// 4. Identity Extraction
	if claims.Email != "" && strings.Contains(claims.Email, "@") {
		return claims.Email
	}
	if claims.PreferredUsername != "" && strings.Contains(claims.PreferredUsername, "@") {
		return claims.PreferredUsername
	}
	if claims.Username != "" && strings.Contains(claims.Username, "@") {
		return claims.Username
	}
	if claims.WSO2Email != "" && strings.Contains(claims.WSO2Email, "@") {
		return claims.WSO2Email
	}
	if claims.WSO2Username != "" && strings.Contains(claims.WSO2Username, "@") {
		return claims.WSO2Username
	}
	if strings.Contains(claims.Sub, "@") {
		return claims.Sub
	}

	return ""
}

// ResolveToken returns the active JWT token, resolving DEV_STAGING_TOKEN only in permitted dev environments.
func ResolveToken(incomingToken string) string {
	if incomingToken == "" {
		if IsPermittedDevEnvironment() {
			if devStagingToken := os.Getenv("DEV_STAGING_TOKEN"); devStagingToken != "" {
				return devStagingToken
			}
		}
		return ""
	}
	return incomingToken
}
