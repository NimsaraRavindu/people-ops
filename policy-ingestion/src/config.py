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

# Configuration settings for Policy Ingestion Service.

import os
from typing import Optional
from urllib.parse import parse_qs, quote_plus, urlparse
from pydantic import PrivateAttr
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    """Configuration loaded from environment variables and .env file."""

    # Server Configuration
    ENV: Optional[str] = None
    ENVIRONMENT: str = "development"
    PORT: int = 8080
    LOG_LEVEL: str = "info"

    _explicit_env: bool = PrivateAttr(default=False)
    _explicit_environment: bool = PrivateAttr(default=False)

    def __init__(self, **values):
        super().__init__(**values)
        self._explicit_env = "ENV" in self.model_fields_set
        self._explicit_environment = "ENVIRONMENT" in self.model_fields_set
        # When ENV is explicitly configured and ENVIRONMENT is not, adopt ENV
        if self._explicit_env and not self._explicit_environment and self.ENV:
            self.ENVIRONMENT = self.ENV

    # Database Configuration (PostgreSQL with pgvector)
    DATABASE_URL: Optional[str] = None
    DB_HOST: Optional[str] = "localhost"
    DB_PORT: int = 5432
    DB_NAME: str = "defaultdb"
    DB_USER: Optional[str] = "postgres"
    DB_PASSWORD: Optional[str] = "postgres"
    DB_SSL: Optional[str] = None

    # Google Drive OAuth Credentials (supports both POLICY_* and legacy HR_POLICY_* keys)
    POLICY_GOOGLE_OAUTH_CLIENT_ID: Optional[str] = None
    POLICY_GOOGLE_OAUTH_CLIENT_SECRET: Optional[str] = None
    POLICY_GOOGLE_OAUTH_REFRESH_TOKEN: Optional[str] = None

    HR_POLICY_GOOGLE_OAUTH_CLIENT_ID: Optional[str] = None
    HR_POLICY_GOOGLE_OAUTH_CLIENT_SECRET: Optional[str] = None
    HR_POLICY_GOOGLE_OAUTH_REFRESH_TOKEN: Optional[str] = None

    # Google Drive Target Folders
    POLICY_DRIVE_FOLDER_ID: Optional[str] = None
    HR_POLICY_DRIVE_FOLDER_ID: Optional[str] = None
    TRAINING_DRIVE_FOLDER_ID: Optional[str] = None

    # Embedding Provider ("local" with FastEmbed BGE, or "dummy" for mock)
    EMBEDDING_PROVIDER: str = "local"
    EMBEDDING_MODEL: str = "BAAI/bge-base-en-v1.5"
    EMBEDDING_DIMENSION: int = 768
    GOOGLE_API_KEY: Optional[str] = None
    OPENAI_API_KEY: Optional[str] = None
    POLICY_INGESTION_SECRET: Optional[str] = None

    # Sync Cadence & Triggers
    SYNC_ENABLED: bool = True
    POLICY_SYNC_INTERVAL_HOURS: int = 24
    SYNC_ON_STARTUP: bool = True

    # Chunking Hyperparameters
    CHUNK_SIZE_TOKENS: int = 400
    CHUNK_OVERLAP_TOKENS: int = 50

    model_config = SettingsConfigDict(
        env_file=".env",
        env_file_encoding="utf-8",
        extra="ignore"
    )

    @property
    def oauth_client_id(self) -> str:
        """Returns the resolved Google OAuth Client ID."""
        return self.POLICY_GOOGLE_OAUTH_CLIENT_ID or self.HR_POLICY_GOOGLE_OAUTH_CLIENT_ID or ""

    @property
    def oauth_client_secret(self) -> str:
        """Returns the resolved Google OAuth Client Secret."""
        return self.POLICY_GOOGLE_OAUTH_CLIENT_SECRET or self.HR_POLICY_GOOGLE_OAUTH_CLIENT_SECRET or ""

    @property
    def oauth_refresh_token(self) -> str:
        """Returns the resolved Google OAuth Refresh Token."""
        return self.POLICY_GOOGLE_OAUTH_REFRESH_TOKEN or self.HR_POLICY_GOOGLE_OAUTH_REFRESH_TOKEN or ""

    @property
    def policy_folder_id(self) -> str:
        """Returns the resolved HR Policy Google Drive folder ID."""
        return self.POLICY_DRIVE_FOLDER_ID or self.HR_POLICY_DRIVE_FOLDER_ID or ""

    @property
    def training_folder_id(self) -> str:
        """Returns the resolved Training Google Drive folder ID."""
        return self.TRAINING_DRIVE_FOLDER_ID or ""

    def get_database_dsn(self) -> str:
        """Constructs an asyncpg DSN connection string with verified TLS in production."""
        raw_env = self.ENV or os.getenv("ENV") or ""
        raw_environment = self.ENVIRONMENT or os.getenv("ENVIRONMENT") or ""

        env_val = raw_env.lower().strip()
        environment_val = raw_environment.lower().strip()

        is_env_prod = env_val in ("production", "prod")
        is_env_dev = env_val in ("development", "dev", "test", "testing", "local")
        is_environment_prod = environment_val in ("production", "prod")
        is_environment_dev = environment_val in ("development", "dev", "test", "testing", "local")

        # Reject conflicting environment settings only when both are explicitly configured
        if self._explicit_env and self._explicit_environment:
            if (is_env_prod and is_environment_dev) or (is_env_dev and is_environment_prod):
                raise ValueError(
                    f"Conflicting environment settings: ENV='{raw_env}' and "
                    f"ENVIRONMENT='{raw_environment}' cannot specify opposing modes."
                )

        is_prod = is_env_prod or is_environment_prod

        if self.DATABASE_URL:
            dsn = self.DATABASE_URL
            if dsn.startswith("postgresql+asyncpg://"):
                dsn = dsn.replace("postgresql+asyncpg://", "postgresql://")
            if is_prod:
                parsed = urlparse(dsn)
                params = parse_qs(parsed.query)
                ssl_modes = params.get("sslmode", [])
                if len(ssl_modes) > 1:
                    raise ValueError(
                        f"Insecure database connection rejected: duplicate sslmode parameters "
                        f"in DATABASE_URL are not permitted ({ssl_modes})."
                    )
                ssl_mode = ssl_modes[0].lower().strip() if ssl_modes else None
                if ssl_mode != "verify-full":
                    raise ValueError(
                        f"Insecure database connection rejected: production requires verified TLS "
                        f"(sslmode=verify-full, got '{ssl_mode or 'unset'}')."
                    )
            return dsn

        raw_ssl = (self.DB_SSL or "").lower().strip() if self.DB_SSL else ""
        if not raw_ssl:
            ssl_mode = "verify-full" if is_prod else "disable"
        elif raw_ssl in ("true", "1", "require"):
            ssl_mode = "require"
        elif raw_ssl in ("disable", "allow", "prefer", "verify-ca", "verify-full"):
            ssl_mode = raw_ssl
        else:
            raise ValueError(
                f"Unsupported DB_SSL mode '{self.DB_SSL}'. Supported modes: disable, allow, "
                f"prefer, require, verify-ca, verify-full."
            )

        if is_prod and ssl_mode != "verify-full":
            raise ValueError(
                f"Insecure database connection rejected: production requires verified TLS "
                f"(sslmode=verify-full, got '{ssl_mode}')."
            )

        user_auth = ""
        if self.DB_USER:
            user = quote_plus(self.DB_USER)
            password = quote_plus(self.DB_PASSWORD or "")
            user_auth = f"{user}:{password}@" if self.DB_PASSWORD else f"{user}@"
        return (
            f"postgresql://{user_auth}{self.DB_HOST}:{self.DB_PORT}/{self.DB_NAME}?sslmode={ssl_mode}"
        )


settings = Settings()
