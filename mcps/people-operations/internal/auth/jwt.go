// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com). All Rights Reserved.
//
// This software is the property of WSO2 LLC. and its suppliers, if any.
// Dissemination of any information or reproduction of any material contained
// herein in any form is strictly forbidden, unless permitted by WSO2 expressly.
// You may not alter or remove any copyright or other notice from copies of this content.

package auth

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
)

// ExtractEmailFromJWT extracts the email address or user principal from an incoming JWT.
// Returns an empty string if no valid identity claim is present in the token.
func ExtractEmailFromJWT(tokenStr string) string {
	parts := strings.Split(tokenStr, ".")
	if len(parts) >= 2 {
		payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
		if err == nil {
			var claims struct {
				Email             string `json:"email"`
				Sub               string `json:"sub"`
				Username          string `json:"username"`
				PreferredUsername string `json:"preferred_username"`
				WSO2Email         string `json:"http://wso2.org/claims/emailaddress"`
				WSO2Username      string `json:"http://wso2.org/claims/username"`
			}
			if err := json.Unmarshal(payloadBytes, &claims); err == nil {
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
			}
		}
	}

	return ""
}

// ResolveToken returns the active JWT token, resolving DEV_STAGING_TOKEN when running locally.
func ResolveToken(incomingToken string) string {
	if incomingToken == "" {
		if devStagingToken := os.Getenv("DEV_STAGING_TOKEN"); devStagingToken != "" {
			return devStagingToken
		}
		return ""
	}
	return incomingToken
}
