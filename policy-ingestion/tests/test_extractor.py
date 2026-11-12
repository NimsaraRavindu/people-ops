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

import pytest
from src.drive.client import DriveFileMetadata
from src.drive.extractor import (
    MIME_GOOGLE_FOLDER,
    _warn_if_suspiciously_short,
    fetch_file_text,
)


@pytest.mark.asyncio
async def test_fetch_file_text_skips_folder():
    folder_meta = DriveFileMetadata(
        id="folder-1",
        name="HR Policies 2026",
        mime_type=MIME_GOOGLE_FOLDER,
        modified_time="2026-01-01T00:00:00Z",
    )
    result = await fetch_file_text(folder_meta)
    assert result is None


@pytest.mark.asyncio
async def test_fetch_file_text_skips_unsupported():
    unsupported_meta = DriveFileMetadata(
        id="image-1",
        name="company_logo.png",
        mime_type="image/png",
        modified_time="2026-01-01T00:00:00Z",
    )
    result = await fetch_file_text(unsupported_meta)
    assert result is None


def test_warn_if_suspiciously_short_does_not_raise():
    # Should log warning but not raise exception
    _warn_if_suspiciously_short("sample.pdf", "tiny", "PDF")
    _warn_if_suspiciously_short("sample.docx", "a", "DOCX")


@pytest.mark.asyncio
async def test_docx_interleaved_order_preserved(monkeypatch):
    import io
    from docx import Document
    from src.drive.extractor import MIME_DOCX

    doc = Document()
    doc.add_paragraph("Heading 1")
    table = doc.add_table(rows=1, cols=2)
    table.rows[0].cells[0].text = "Col A"
    table.rows[0].cells[1].text = "Col B"
    doc.add_paragraph("Heading 2")

    buf = io.BytesIO()
    doc.save(buf)
    raw_docx = buf.getvalue()

    async def mock_download(*args, **kwargs):
        return raw_docx

    async def mock_token():
        return "fake-token"

    monkeypatch.setattr("src.drive.extractor._download_binary", mock_download)
    monkeypatch.setattr("src.drive.extractor.drive_client.get_access_token", mock_token)

    meta = DriveFileMetadata(
        id="doc-123",
        name="policy.docx",
        mime_type=MIME_DOCX,
        modified_time="2026-01-01T00:00:00Z"
    )
    extracted = await fetch_file_text(meta)
    assert extracted == "Heading 1\n\nCol A | Col B\n\nHeading 2"
