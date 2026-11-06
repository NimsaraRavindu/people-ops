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
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var (
	jwksCacheLock  sync.RWMutex
	jwksFetchLock  sync.Mutex
	jwksKeyCache   = make(map[string]*rsa.PublicKey)
	lastJWKSFetch  time.Time
	jwksCacheTTL   = 1 * time.Hour
	minJWKSRefetch = 1 * time.Minute
	jwksClient     = &http.Client{Timeout: 5 * time.Second}
)

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

// fetchJWKS fetches and caches RSA public keys from a JWKS endpoint with context cancellation.
func fetchJWKS(ctx context.Context, jwksURL string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURL, nil)
	if err != nil {
		return err
	}
	resp, err := jwksClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("JWKS endpoint returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return err
	}

	var jwks jwksResponse
	if err := json.Unmarshal(body, &jwks); err != nil {
		return err
	}

	jwksCacheLock.Lock()
	defer jwksCacheLock.Unlock()

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
			jwksKeyCache[k.Kid] = pubKey
		}
		jwksKeyCache[jwksURL+":"+k.Kid] = pubKey
	}
	lastJWKSFetch = time.Now()
	return nil
}

// getPublicKey retrieves a cached RSA public key by kid or re-fetches if absent.
// Enforces minimum refetch interval (cooldown) to protect against DoS attacks from unknown kids.
func getPublicKey(jwksURL string, kid string) (*rsa.PublicKey, error) {
	jwksCacheLock.RLock()
	pubKey, exists := jwksKeyCache[kid]
	if !exists && kid != "" {
		pubKey, exists = jwksKeyCache[jwksURL+":"+kid]
	}
	cacheExpired := time.Since(lastJWKSFetch) > jwksCacheTTL
	recentlyFetched := !lastJWKSFetch.IsZero() && time.Since(lastJWKSFetch) < minJWKSRefetch
	jwksCacheLock.RUnlock()

	if exists && !cacheExpired {
		return pubKey, nil
	}

	// Throttle remote JWKS fetches to prevent DoS via random/missing kids
	if recentlyFetched && !cacheExpired {
		return nil, fmt.Errorf("key with kid %q not found in JWKS", kid)
	}

	jwksFetchLock.Lock()
	defer jwksFetchLock.Unlock()

	// Double check cache after acquiring fetch lock to collapse concurrent requests
	jwksCacheLock.RLock()
	pubKey, exists = jwksKeyCache[kid]
	if !exists && kid != "" {
		pubKey, exists = jwksKeyCache[jwksURL+":"+kid]
	}
	cacheExpired = time.Since(lastJWKSFetch) > jwksCacheTTL
	recentlyFetched = !lastJWKSFetch.IsZero() && time.Since(lastJWKSFetch) < minJWKSRefetch
	jwksCacheLock.RUnlock()

	if exists && !cacheExpired {
		return pubKey, nil
	}
	if recentlyFetched && !cacheExpired {
		return nil, fmt.Errorf("key with kid %q not found in JWKS", kid)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := fetchJWKS(ctx, jwksURL); err != nil {
		if exists {
			return pubKey, nil // Fall back to stale cache on network error
		}
		return nil, err
	}

	jwksCacheLock.RLock()
	defer jwksCacheLock.RUnlock()
	if pubKey, exists := jwksKeyCache[kid]; exists {
		return pubKey, nil
	}
	if pubKey, exists := jwksKeyCache[jwksURL+":"+kid]; exists {
		return pubKey, nil
	}
	// Fallback key only when token omits kid and JWKS contains exactly one key for this URL
	if kid == "" {
		var urlKeys []*rsa.PublicKey
		for key, pub := range jwksKeyCache {
			if strings.HasPrefix(key, jwksURL+":") {
				urlKeys = append(urlKeys, pub)
			}
		}
		if len(urlKeys) == 1 {
			return urlKeys[0], nil
		}
	}
	return nil, fmt.Errorf("key with kid %q not found in JWKS", kid)
}

// isProductionEnvironment checks ENVIRONMENT, ENV, and APP_ENV to determine if running in production/staging.
func isProductionEnvironment() bool {
	for _, key := range []string{"ENVIRONMENT", "ENV", "APP_ENV"} {
		val := strings.ToLower(strings.TrimSpace(os.Getenv(key)))
		if val == "production" || val == "prod" || val == "staging" || val == "live" {
			return true
		}
	}
	return false
}

// isTokenValidatorEnabled determines if cryptographic signature validation is required.
// Enforced in production/staging environments, or when explicitly enabled via TOKEN_VALIDATOR_ENABLED.
func isTokenValidatorEnabled() bool {
	if isProductionEnvironment() {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(os.Getenv("TOKEN_VALIDATOR_ENABLED")), "true")
}

// ExtractEmailFromJWT extracts and verifies identity from an incoming JWT.
// Enforces structural integrity, alg validation (rejects none), expiration (exp),
// and cryptographic signature verification when enabled or in production.
func ExtractEmailFromJWT(tokenStr string) string {
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
	if claims.Exp > 0 && now > claims.Exp {
		slog.Warn("Rejected JWT: token has expired", "exp", claims.Exp, "now", now)
		return ""
	}
	if claims.Nbf > 0 && now < claims.Nbf {
		slog.Warn("Rejected JWT: token not yet valid (nbf)", "nbf", claims.Nbf, "now", now)
		return ""
	}

	// 3. Cryptographic Signature Verification
	if isTokenValidatorEnabled() {
		jwksURL := strings.TrimSpace(os.Getenv("ASGARDEO_JWKS_URL"))
		if jwksURL == "" {
			jwksURL = strings.TrimSpace(os.Getenv("CHOREO_JWKS_URL"))
		}
		if jwksURL == "" {
			slog.Error("Rejected JWT: token verification required but neither ASGARDEO_JWKS_URL nor CHOREO_JWKS_URL set")
			return ""
		}

		parsedToken, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
			if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
				return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
			}
			kid, _ := t.Header["kid"].(string)
			return getPublicKey(jwksURL, kid)
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

// ResolveToken returns the active JWT token, resolving DEV_STAGING_TOKEN only in non-production environments.
func ResolveToken(incomingToken string) string {
	if incomingToken == "" {
		if !isProductionEnvironment() {
			if devStagingToken := os.Getenv("DEV_STAGING_TOKEN"); devStagingToken != "" {
				return devStagingToken
			}
		}
		return ""
	}
	return incomingToken
}
