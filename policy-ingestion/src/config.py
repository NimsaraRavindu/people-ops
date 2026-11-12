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
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    """Configuration loaded from environment variables and .env file."""

    # Server Configuration
    ENVIRONMENT: str = "development"
    PORT: int = 8080
    LOG_LEVEL: str = "info"
    ADMIN_API_KEY: Optional[str] = None

    # Database Configuration (PostgreSQL with pgvector)
    DATABASE_URL: Optional[str] = None
    DB_HOST: Optional[str] = "localhost"
    DB_PORT: int = 5432
    DB_NAME: str = "defaultdb"
    DB_USER: Optional[str] = "postgres"
    DB_PASSWORD: Optional[str] = "postgres"
    DB_SSL: str = "disable"

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
        """Constructs an asyncpg DSN connection string."""
        if self.DATABASE_URL:
            # Normalize scheme if using standard postgresql://
            dsn = self.DATABASE_URL
            if dsn.startswith("postgresql+asyncpg://"):
                dsn = dsn.replace("postgresql+asyncpg://", "postgresql://")
            return dsn

        ssl_mode = "require" if self.DB_SSL.lower() in ("true", "require", "1") else "disable"
        user_auth = f"{self.DB_USER}:{self.DB_PASSWORD}@" if self.DB_USER else ""
        return (
            f"postgresql://{user_auth}{self.DB_HOST}:{self.DB_PORT}/{self.DB_NAME}?sslmode={ssl_mode}"
        )


settings = Settings()
