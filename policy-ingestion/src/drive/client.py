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

# Google Drive API client using OAuth 2.0 user refresh token.

import asyncio
import logging
import time
from dataclasses import dataclass
from typing import Any, Dict, List, Optional
import httpx

from ..config import settings

logger = logging.getLogger("policy_ingestion.drive")

DRIVE_API_BASE = "https://www.googleapis.com/drive/v3"
OAUTH_TOKEN_URL = "https://oauth2.googleapis.com/token"


@dataclass
class DriveFileMetadata:
    """Metadata for a file listed from Google Drive."""
    id: str
    name: str
    mime_type: str
    modified_time: str


class GoogleDriveClient:
    """Client for Google Drive API v3 authenticated with an OAuth2 refresh token."""

    def __init__(self):
        self._access_token: Optional[str] = None
        self._token_expires_at: float = 0.0
        self._token_lock: Optional[asyncio.Lock] = None

    def _get_token_lock(self) -> asyncio.Lock:
        if self._token_lock is None:
            self._token_lock = asyncio.Lock()
        return self._token_lock

    async def get_access_token(self) -> str:
        """Obtains a valid access token, refreshing automatically if expired."""
        now = time.time()
        if self._access_token and now < (self._token_expires_at - 60):
            return self._access_token

        async with self._get_token_lock():
            now = time.time()
            if self._access_token and now < (self._token_expires_at - 60):
                return self._access_token

            client_id = settings.oauth_client_id
            client_secret = settings.oauth_client_secret
            refresh_token = settings.oauth_refresh_token

            if not all([client_id, client_secret, refresh_token]):
                raise ValueError(
                    "Missing Google Drive OAuth credentials: "
                    "client_id, client_secret, and refresh_token are required."
                )

            logger.info("Refreshing Google Drive OAuth access token...")
            async with httpx.AsyncClient(timeout=15.0) as http:
                resp = await http.post(
                    OAUTH_TOKEN_URL,
                    data={
                        "client_id": client_id,
                        "client_secret": client_secret,
                        "refresh_token": refresh_token,
                        "grant_type": "refresh_token",
                    },
                )

                if resp.status_code != 200:
                    raise RuntimeError(
                        f"Failed to refresh Google Drive token: {resp.status_code} - {resp.text}"
                    )

                data = resp.json()
                self._access_token = data["access_token"]
                expires_in = int(data.get("expires_in", 3600))
                self._token_expires_at = now + expires_in
                logger.info("Successfully refreshed Google Drive access token.")
                return self._access_token

    async def list_folder_files(self, folder_id: str) -> List[DriveFileMetadata]:
        """
        Lists all active files directly under the designated folder ID.
        Note: Does not recurse into subdirectories.
        """
        token = await self.get_access_token()
        headers = {"Authorization": f"Bearer {token}"}
        files: List[DriveFileMetadata] = []
        page_token: Optional[str] = None

        async with httpx.AsyncClient(timeout=30.0) as http:
            while True:
                params: Dict[str, Any] = {
                    "q": f"'{folder_id}' in parents and trashed = false",
                    "fields": "files(id,name,mimeType,modifiedTime),nextPageToken",
                    "pageSize": 100,
                    "supportsAllDrives": "true",
                    "includeItemsFromAllDrives": "true",
                }
                if page_token:
                    params["pageToken"] = page_token

                resp = await http.get(
                    f"{DRIVE_API_BASE}/files",
                    params=params,
                    headers=headers,
                )

                if resp.status_code != 200:
                    raise RuntimeError(
                        f"Failed to list folder files ({folder_id}): {resp.status_code} - {resp.text}"
                    )

                body = resp.json()
                for item in body.get("files", []):
                    files.append(
                        DriveFileMetadata(
                            id=item["id"],
                            name=item["name"],
                            mime_type=item["mimeType"],
                            modified_time=item["modifiedTime"],
                        )
                    )

                page_token = body.get("nextPageToken")
                if not page_token:
                    break

        logger.info(f"Retrieved {len(files)} files from Google Drive folder {folder_id}.")
        return files


drive_client = GoogleDriveClient()
