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

"""Unit tests for JWT validation, claim extraction, and JWKS signature verification."""

import base64
import json
import time
from unittest.mock import patch
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import rsa
import jwt
from jwt.algorithms import RSAAlgorithm

from src.auth.jwt import (
    extract_email_from_jwt,
    is_permitted_dev_environment,
    reset_jwks_cache,
    resolve_token,
)


def _encode_segment(data: dict) -> str:
    raw = json.dumps(data).encode("utf-8")
    return base64.urlsafe_b64encode(raw).decode("ascii").rstrip("=")


def test_extract_email_rejects_none_alg_and_expired_tokens(monkeypatch):
    monkeypatch.setenv("ENVIRONMENT", "development")
    monkeypatch.setenv("ENV", "development")
    monkeypatch.setenv("TOKEN_VALIDATOR_ENABLED", "false")

    now = int(time.time())
    # 1. alg: none rejected
    tok_none = (
        f"{_encode_segment({'alg': 'none'})}."
        f"{_encode_segment({'email': 'a@wso2.com', 'exp': now + 300})}.sig"
    )
    assert extract_email_from_jwt(tok_none) == ""

    # 2. Expired token rejected
    tok_exp = (
        f"{_encode_segment({'alg': 'RS256'})}."
        f"{_encode_segment({'email': 'a@wso2.com', 'exp': now - 10})}.sig"
    )
    assert extract_email_from_jwt(tok_exp) == ""

    # 3. Valid dev token extracts email
    tok_ok = (
        f"{_encode_segment({'alg': 'RS256'})}."
        f"{_encode_segment({'email': 'alice@wso2.com', 'exp': now + 300})}.sig"
    )
    assert extract_email_from_jwt(f"Bearer {tok_ok}") == "alice@wso2.com"


def test_extract_email_with_jwks_rsa_verification(monkeypatch):
    reset_jwks_cache()
    monkeypatch.setenv("ENVIRONMENT", "production")
    monkeypatch.setenv("ENV", "production")
    monkeypatch.setenv("CHOREO_JWKS_URL", "https://jwks.example.test/.wellknown/jwks")

    private_key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
    public_key = private_key.public_key()
    jwk_dict = json.loads(RSAAlgorithm.to_jwk(public_key))
    jwk_dict["kid"] = "kid-2026"
    jwk_dict["use"] = "sig"
    jwk_dict["alg"] = "RS256"

    pem_private = private_key.private_bytes(
        encoding=serialization.Encoding.PEM,
        format=serialization.PrivateFormat.PKCS8,
        encryption_algorithm=serialization.NoEncryption(),
    )
    signed_token = jwt.encode(
        {"email": "verified.user@wso2.com", "exp": int(time.time()) + 600},
        pem_private,
        algorithm="RS256",
        headers={"kid": "kid-2026"},
    )

    class _FakeResp:
        status_code = 200

        def json(self):
            return {"keys": [jwk_dict]}

    with patch("src.auth.jwt.httpx.Client.get", return_value=_FakeResp()):
        assert extract_email_from_jwt(signed_token) == "verified.user@wso2.com"


def test_resolve_token_only_uses_dev_staging_in_dev(monkeypatch):
    monkeypatch.setenv("DEV_STAGING_TOKEN", "dev-fallback-jwt")
    monkeypatch.setenv("ENVIRONMENT", "development")
    monkeypatch.setenv("ENV", "development")
    assert is_permitted_dev_environment() is True
    assert resolve_token("") == "dev-fallback-jwt"

    monkeypatch.setenv("ENVIRONMENT", "production")
    monkeypatch.setenv("ENV", "production")
    assert is_permitted_dev_environment() is False
    assert resolve_token("") == ""
