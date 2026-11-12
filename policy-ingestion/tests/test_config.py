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

"""Unit tests for configuration and database DSN resolution."""

import pytest
from src.config import Settings


def test_default_development_dsn_has_sslmode_disable():
    cfg = Settings(ENVIRONMENT="development", DB_SSL=None, DATABASE_URL=None)
    dsn = cfg.get_database_dsn()
    assert "sslmode=disable" in dsn


def test_production_dsn_defaults_to_verify_full():
    cfg = Settings(ENVIRONMENT="production", DB_SSL=None, DATABASE_URL=None)
    dsn = cfg.get_database_dsn()
    assert "sslmode=verify-full" in dsn


@pytest.mark.parametrize("insecure_mode", ["disable", "allow", "prefer", "require", "verify-ca"])
def test_production_rejects_insecure_db_ssl_modes(insecure_mode: str):
    cfg = Settings(ENVIRONMENT="production", DB_SSL=insecure_mode, DATABASE_URL=None)
    with pytest.raises(ValueError, match="production requires verified TLS"):
        cfg.get_database_dsn()


@pytest.mark.parametrize("insecure_url", [
    "postgresql://user:pass@localhost:5432/db?sslmode=disable",
    "postgresql://user:pass@localhost:5432/db?sslmode=require",
    "postgresql://user:pass@localhost:5432/db?sslmode=verify-ca",
    "postgresql://user:pass@localhost:5432/db",
])
def test_production_rejects_insecure_database_url(insecure_url: str):
    cfg = Settings(ENVIRONMENT="production", DATABASE_URL=insecure_url)
    with pytest.raises(ValueError, match="production requires verified TLS"):
        cfg.get_database_dsn()


def test_production_accepts_verify_full_database_url():
    url = "postgresql://user:pass@localhost:5432/db?sslmode=verify-full"
    cfg = Settings(ENVIRONMENT="production", DATABASE_URL=url)
    assert cfg.get_database_dsn() == url


def test_nonproduction_allows_custom_ssl_modes():
    cfg_ca = Settings(ENVIRONMENT="development", DB_SSL="verify-ca", DATABASE_URL=None)
    assert "sslmode=verify-ca" in cfg_ca.get_database_dsn()

    cfg_req = Settings(ENVIRONMENT="development", DB_SSL="require", DATABASE_URL=None)
    assert "sslmode=require" in cfg_req.get_database_dsn()

    insecure_url = "postgresql://user:pass@localhost:5432/db?sslmode=disable"
    cfg_url = Settings(ENVIRONMENT="development", DATABASE_URL=insecure_url)
    assert cfg_url.get_database_dsn() == insecure_url


def test_unsupported_ssl_mode_raises():
    cfg = Settings(ENVIRONMENT="development", DB_SSL="bogus_ssl", DATABASE_URL=None)
    with pytest.raises(ValueError, match="Unsupported DB_SSL mode"):
        cfg.get_database_dsn()
