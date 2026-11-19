# Copyright (c) 2026 WSO2 LLC. (https://www.wso2.com).
#
# WSO2 LLC. licenses this file to you under the Apache License,
# Version 2.0 (the "License"); you may not use this file except
# in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing,
# software distributed under the License is distributed on an
# "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
# KIND, either express or implied.  See the License for the
# specific language governing permissions and limitations
# under the License.

"""JWT identity extraction and JWKS signature verification for Policy MCP."""

import base64
import json
import logging
import os
import threading
import time
from dataclasses import dataclass, field
from typing import Any, Dict, List, Optional, Tuple

import httpx
import jwt
from jwt.algorithms import RSAAlgorithm

from ..config import settings

logger = logging.getLogger("policy_mcp.auth")

ALLOWED_ALGORITHMS = {
    "RS256", "RS384", "RS512",
    "ES256", "ES384", "ES512",
    "PS256", "PS384", "PS512",
}

JWKS_CACHE_TTL_SECONDS = 3600.0
MAX_STALE_JWKS_AGE_SECONDS = 86400.0
MIN_JWKS_REFETCH_SECONDS = 60.0


@dataclass
class _JWKSEndpointCache:
    """Stores cached RSA public keys and fetch timestamps for a single JWKS URL."""

    keys: Dict[str, Any] = field(default_factory=dict)
    keys_list: List[Any] = field(default_factory=list)
    last_attempt: float = 0.0
    last_success: float = 0.0


_jwks_lock = threading.RLock()
_jwks_fetch_lock = threading.Lock()
_jwks_caches: Dict[str, _JWKSEndpointCache] = {}


def reset_jwks_cache() -> None:
    """Clears all cached JWKS keys and fetch timestamps (primarily for unit tests)."""
    with _jwks_lock:
        _jwks_caches.clear()


def _decode_base64url(segment: str) -> bytes:
    """Decodes an unpadded Base64URL segment."""
    rem = len(segment) % 4
    if rem > 0:
        segment += "=" * (4 - rem)
    return base64.urlsafe_b64decode(segment.encode("ascii"))


def _record_fetch_attempt(jwks_url: str) -> None:
    """Records a fetch attempt timestamp to throttle rapid repeat calls on outage."""
    with _jwks_lock:
        cache = _jwks_caches.get(jwks_url)
        if cache is None:
            cache = _JWKSEndpointCache()
            _jwks_caches[jwks_url] = cache
        cache.last_attempt = time.time()


def _lookup_cached_key(jwks_url: str, kid: str) -> Tuple[Optional[Any], bool, bool]:
    """
    Checks if a valid RSA key is already cached for the given JWKS URL.
    Returns (public_key, found, can_fetch).
    """
    with _jwks_lock:
        cache = _jwks_caches.get(jwks_url)
        if cache is None or cache.last_success <= 0:
            cooldown = (
                cache is not None
                and cache.last_attempt > 0
                and (time.time() - cache.last_attempt) < MIN_JWKS_REFETCH_SECONDS
            )
            return None, False, not cooldown

        now = time.time()
        expired = (now - cache.last_success) > JWKS_CACHE_TTL_SECONDS
        cooldown = (
            cache.last_attempt > 0
            and (now - cache.last_attempt) < MIN_JWKS_REFETCH_SECONDS
        )
        within_stale_bound = (now - cache.last_success) <= MAX_STALE_JWKS_AGE_SECONDS

        if kid:
            pub = cache.keys.get(kid)
            if pub is not None and (not expired or cooldown) and within_stale_bound:
                return pub, True, False
        elif len(cache.keys_list) == 1:
            if (not expired or cooldown) and within_stale_bound:
                return cache.keys_list[0], True, False

        return None, False, not cooldown


def _fetch_jwks_sync(jwks_url: str) -> None:
    """Fetches and parses RSA public keys from a remote JWKS endpoint."""
    try:
        with httpx.Client(timeout=5.0) as client:
            resp = client.get(jwks_url)
    except Exception as exc:
        _record_fetch_attempt(jwks_url)
        raise RuntimeError(f"JWKS request failed: {exc}") from exc

    if resp.status_code != 200:
        _record_fetch_attempt(jwks_url)
        raise RuntimeError(f"JWKS endpoint returned HTTP {resp.status_code}")

    try:
        payload = resp.json()
    except Exception as exc:
        _record_fetch_attempt(jwks_url)
        raise RuntimeError(f"Invalid JWKS JSON response: {exc}") from exc

    new_keys: Dict[str, Any] = {}
    new_keys_list: List[Any] = []

    for key_dict in payload.get("keys", []):
        if key_dict.get("kty") != "RSA" or not key_dict.get("n") or not key_dict.get("e"):
            continue
        try:
            pub_key = RSAAlgorithm.from_jwk(json.dumps(key_dict))
        except Exception:
            continue
        kid_val = key_dict.get("kid") or ""
        if kid_val:
            new_keys[kid_val] = pub_key
        new_keys_list.append(pub_key)

    now = time.time()
    with _jwks_lock:
        cache = _jwks_caches.get(jwks_url)
        if cache is None:
            cache = _JWKSEndpointCache()
            _jwks_caches[jwks_url] = cache
        cache.keys = new_keys
        cache.keys_list = new_keys_list
        cache.last_success = now
        cache.last_attempt = now


def _get_public_key(jwks_url: str, kid: str) -> Any:
    """Retrieves a cached RSA public key by kid or fetches JWKS with stale-cache fallback."""
    pub, found, can_fetch = _lookup_cached_key(jwks_url, kid)
    if found:
        return pub
    if not can_fetch:
        raise RuntimeError(f"Key with kid '{kid}' not found in JWKS (fetch throttled)")

    with _jwks_fetch_lock:
        pub, found, can_fetch = _lookup_cached_key(jwks_url, kid)
        if found:
            return pub
        if not can_fetch:
            raise RuntimeError(f"Key with kid '{kid}' not found in JWKS (fetch throttled)")

        try:
            _fetch_jwks_sync(jwks_url)
        except Exception as fetch_err:
            with _jwks_lock:
                cache = _jwks_caches.get(jwks_url)
                if (
                    cache is not None
                    and cache.last_success > 0
                    and (time.time() - cache.last_success) <= MAX_STALE_JWKS_AGE_SECONDS
                ):
                    if kid and kid in cache.keys:
                        return cache.keys[kid]
                    if not kid and len(cache.keys_list) == 1:
                        return cache.keys_list[0]
            raise fetch_err

        pub, found, _ = _lookup_cached_key(jwks_url, kid)
        if found:
            return pub

        if not kid:
            with _jwks_lock:
                cache = _jwks_caches.get(jwks_url)
                count = len(cache.keys_list) if cache else 0
            if count > 1:
                raise RuntimeError(f"Token missing kid and JWKS contains {count} keys")

        raise RuntimeError(f"Key with kid '{kid}' not found in JWKS {jwks_url}")


def is_permitted_dev_environment() -> bool:
    """
    Returns True ONLY if a permitted dev/test environment is explicitly set
    and no production, staging, or QA environment indicator is present.
    """
    has_dev = False
    for key in ("ENVIRONMENT", "ENV", "APP_ENV"):
        val = os.getenv(key, "").strip().lower()
        if not val and key == "ENVIRONMENT":
            val = (settings.ENVIRONMENT or "").strip().lower()
        if not val and key == "ENV" and settings.ENV:
            val = settings.ENV.strip().lower()
        if not val:
            continue
        if val in ("development", "dev", "test", "testing", "local"):
            has_dev = True
        else:
            return False

    if has_dev:
        return True
    return False


def is_token_validator_enabled() -> bool:
    """
    Enforces signature verification for all non-dev environments.
    Verification can only be disabled in permitted dev/test environments.
    """
    if not is_permitted_dev_environment():
        return True
    env_flag = os.getenv("TOKEN_VALIDATOR_ENABLED")
    if env_flag is not None:
        return env_flag.strip().lower() in ("true", "1")
    return bool(settings.TOKEN_VALIDATOR_ENABLED)


def resolve_token(incoming_token: str) -> str:
    """Returns the active JWT token, resolving DEV_STAGING_TOKEN only in dev mode."""
    if incoming_token:
        return incoming_token
    if is_permitted_dev_environment():
        dev_tok = os.getenv("DEV_STAGING_TOKEN") or settings.DEV_STAGING_TOKEN or ""
        if dev_tok:
            return dev_tok
    return ""


def extract_email_from_jwt(token_str: str) -> str:
    """
    Extracts and verifies user email identity from an incoming JWT.
    Enforces 3-part format, algorithm allowlist, expiration (exp/nbf),
    and cryptographic JWKS signature verification when enabled or in production.
    """
    if not token_str:
        return ""
    cleaned = token_str.strip()
    if cleaned.lower().startswith("bearer "):
        cleaned = cleaned[7:].strip()
    if not cleaned:
        return ""

    parts = cleaned.split(".")
    if len(parts) != 3:
        logger.warning("Rejected JWT: token does not conform to 3-part format")
        return ""

    # 1. Structural Header Validation
    try:
        header = json.loads(_decode_base64url(parts[0]).decode("utf-8"))
        if not isinstance(header, dict):
            logger.warning("Rejected JWT: header is not a JSON object")
            return ""
    except Exception:
        logger.warning("Rejected JWT: malformed header")
        return ""

    alg = str(header.get("alg") or "").strip()
    if not alg or alg.lower() == "none":
        logger.warning("Rejected JWT: insecure 'none' or missing algorithm")
        return ""
    if alg.upper() not in ALLOWED_ALGORITHMS:
        logger.warning("Rejected JWT: unsupported signing algorithm '%s'", alg)
        return ""
    if not parts[2]:
        logger.warning("Rejected JWT: missing signature segment")
        return ""

    # 2. Claims & Expiration Validation
    try:
        claims = json.loads(_decode_base64url(parts[1]).decode("utf-8"))
        if not isinstance(claims, dict):
            logger.warning("Rejected JWT: payload claims is not a JSON object")
            return ""
    except Exception:
        logger.warning("Rejected JWT: malformed payload JSON")
        return ""

    now = int(time.time())
    exp = claims.get("exp")
    if not isinstance(exp, (int, float)) or exp <= 0:
        logger.warning("Rejected JWT: missing or non-positive exp claim")
        return ""
    if now > exp:
        logger.warning("Rejected JWT: token has expired (exp=%s, now=%s)", exp, now)
        return ""

    nbf = claims.get("nbf")
    if isinstance(nbf, (int, float)) and nbf > 0 and now < nbf:
        logger.warning("Rejected JWT: token not yet valid (nbf=%s, now=%s)", nbf, now)
        return ""

    # 3. Cryptographic Signature Verification
    if is_token_validator_enabled():
        jwks_url = (
            os.getenv("CHOREO_JWKS_URL", "").strip()
            or (settings.CHOREO_JWKS_URL or "").strip()
        )
        if not jwks_url:
            logger.error("Rejected JWT: token verification required but CHOREO_JWKS_URL is unset")
            return ""
        if not alg.upper().startswith("RS"):
            logger.warning("Rejected JWT: unexpected non-RSA signing method '%s'", alg)
            return ""
        kid = str(header.get("kid") or "").strip()
        try:
            pub_key = _get_public_key(jwks_url, kid)
            jwt.decode(
                cleaned,
                key=pub_key,
                algorithms=[alg.upper()],
                options={
                    "verify_signature": True,
                    "verify_aud": False,
                    "verify_iss": False,
                    "verify_exp": True,
                },
            )
        except Exception as err:
            logger.warning("Rejected JWT: cryptographic signature verification failed: %s", err)
            return ""

    # 4. Identity Extraction
    candidate_fields = (
        "email",
        "preferred_username",
        "username",
        "http://wso2.org/claims/emailaddress",
        "http://wso2.org/claims/username",
        "sub",
    )
    for field_name in candidate_fields:
        val = claims.get(field_name)
        if isinstance(val, str) and "@" in val:
            return val.strip()

    return ""
