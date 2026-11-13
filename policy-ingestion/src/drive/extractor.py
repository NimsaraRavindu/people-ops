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

# Document text extractors for Google Docs, PDFs, DOCX, and plain text.

import io
import logging
from typing import Optional
import httpx
from pypdf import PdfReader
from docx import Document
from docx.table import Table
from docx.text.paragraph import Paragraph

from .client import DRIVE_API_BASE, DriveFileMetadata, drive_client

logger = logging.getLogger("policy_ingestion.extractor")

MIME_GOOGLE_DOC = "application/vnd.google-apps.document"
MIME_GOOGLE_FOLDER = "application/vnd.google-apps.folder"
MIME_TEXT_PLAIN = "text/plain"
MIME_PDF = "application/pdf"
MIME_DOCX = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"

SUPPORTED_MIME_TYPES = {
    MIME_GOOGLE_DOC,
    MIME_TEXT_PLAIN,
    MIME_PDF,
    MIME_DOCX,
}

SUSPICIOUS_TEXT_THRESHOLD = 50
MAX_BINARY_FILE_SIZE = 25 * 1024 * 1024  # 25 MB safety limit to protect against OOM


async def fetch_file_text(file_meta: DriveFileMetadata) -> Optional[str]:
    """
    Downloads and extracts raw text content from a Google Drive file.
    Returns None for unsupported or folder MIME types without requesting an access token.
    """
    # 1. Skip Google Drive Folder immediately
    if file_meta.mime_type == MIME_GOOGLE_FOLDER:
        logger.debug(f"Skipping directory item: {file_meta.name}")
        return None

    # 2. Skip unsupported MIME types immediately
    if file_meta.mime_type not in SUPPORTED_MIME_TYPES:
        logger.warning(
            f"Skipping unsupported file '{file_meta.name}' with MIME type '{file_meta.mime_type}'"
        )
        return None

    token = await drive_client.get_access_token()
    headers = {"Authorization": f"Bearer {token}"}

    # 3. Google Document -> Export as text/plain
    if file_meta.mime_type == MIME_GOOGLE_DOC:
        async with httpx.AsyncClient(timeout=30.0) as http:
            resp = await http.get(
                f"{DRIVE_API_BASE}/files/{file_meta.id}/export",
                params={"mimeType": "text/plain"},
                headers=headers,
            )
            if resp.status_code != 200:
                raise RuntimeError(
                    f"Failed to export Google Doc ({file_meta.name}): {resp.status_code} - {resp.text}"
                )
            return resp.text

    # 4. Plain Text File -> Direct media download
    if file_meta.mime_type == MIME_TEXT_PLAIN:
        async with httpx.AsyncClient(timeout=30.0) as http:
            resp = await http.get(
                f"{DRIVE_API_BASE}/files/{file_meta.id}",
                params={"alt": "media", "supportsAllDrives": "true"},
                headers=headers,
            )
            if resp.status_code != 200:
                raise RuntimeError(
                    f"Failed to download text file ({file_meta.name}): {resp.status_code} - {resp.text}"
                )
            return resp.text

    # 5. PDF File -> Download binary and extract text with pypdf
    if file_meta.mime_type == MIME_PDF:
        raw_bytes = await _download_binary(file_meta.id, headers)
        reader = PdfReader(io.BytesIO(raw_bytes))
        extracted_pages = []
        for i, page in enumerate(reader.pages):
            text = page.extract_text() or ""
            if text.strip():
                extracted_pages.append(text.strip())

        full_text = "\n\n".join(extracted_pages)
        _warn_if_suspiciously_short(file_meta.name, full_text, "PDF")
        return full_text

    # 6. DOCX Document -> Extract text preserving document order of paragraphs and tables
    if file_meta.mime_type == MIME_DOCX:
        raw_bytes = await _download_binary(file_meta.id, headers)
        doc = Document(io.BytesIO(raw_bytes))
        elements = []
        for block in doc.element.body:
            if block.tag.endswith("p"):
                p = Paragraph(block, doc)
                if p.text.strip():
                    elements.append(p.text.strip())
            elif block.tag.endswith("tbl"):
                table = Table(block, doc)
                for row in table.rows:
                    cells = [c.text.strip() for c in row.cells if c.text.strip()]
                    if cells:
                        elements.append(" | ".join(cells))
        full_text = "\n\n".join(elements)
        _warn_if_suspiciously_short(file_meta.name, full_text, "DOCX")
        return full_text

    return None


async def _download_binary(file_id: str, headers: dict) -> bytes:
    """Downloads raw binary media for PDF and DOCX parsing with shared drive support."""
    async with httpx.AsyncClient(timeout=60.0) as http:
        resp = await http.get(
            f"{DRIVE_API_BASE}/files/{file_id}",
            params={"alt": "media", "supportsAllDrives": "true"},
            headers=headers,
        )
        if resp.status_code != 200:
            raise RuntimeError(
                f"Failed to download binary file ({file_id}): {resp.status_code} - {resp.text}"
            )
        content_length = resp.headers.get("Content-Length")
        if content_length and int(content_length) > MAX_BINARY_FILE_SIZE:
            raise ValueError(
                f"File {file_id} exceeds maximum permitted binary download size "
                f"({content_length} bytes > {MAX_BINARY_FILE_SIZE} bytes)."
            )
        if len(resp.content) > MAX_BINARY_FILE_SIZE:
            raise ValueError(
                f"File {file_id} exceeds maximum permitted binary download size "
                f"({len(resp.content)} bytes > {MAX_BINARY_FILE_SIZE} bytes)."
            )
        return resp.content


def _warn_if_suspiciously_short(name: str, text: str, kind: str) -> None:
    """Warns if extracted text is shorter than threshold (indicates scanned or empty doc)."""
    trimmed_len = len(text.strip())
    if trimmed_len < SUSPICIOUS_TEXT_THRESHOLD:
        hint = (
            "possibly a scanned / image-only PDF without OCR"
            if kind == "PDF"
            else "possibly an empty or image-heavy document"
        )
        logger.warning(
            f"[extractor] {kind} '{name}' yielded only {trimmed_len} chars — {hint}."
        )
