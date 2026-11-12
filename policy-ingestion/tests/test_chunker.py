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
from src.ingestion.chunker import RecursiveTextChunker, TextChunk


def test_chunk_empty_document():
    chunker = RecursiveTextChunker(chunk_size_tokens=100, chunk_overlap_tokens=10)
    chunks = chunker.chunk_document("doc-1", "Empty Doc", "   \n\n   ")
    assert chunks == []


def test_chunk_small_document():
    chunker = RecursiveTextChunker(chunk_size_tokens=500, chunk_overlap_tokens=50)
    text = "WSO2 provides flexible working arrangements for all full-time employees."
    chunks = chunker.chunk_document("doc-2", "Flexible Work Policy", text)

    assert len(chunks) == 1
    assert chunks[0].document_id == "doc-2"
    assert chunks[0].chunk_index == 0
    assert "=== Flexible Work Policy ===" in chunks[0].content
    assert text in chunks[0].content
    assert chunks[0].token_count > 0


def test_chunk_large_document_contextual_headers():
    chunker = RecursiveTextChunker(
        chunk_size_tokens=20,
        chunk_overlap_tokens=5,
        chars_per_token=4.0
    )
    paragraphs = [
        "Paragraph 1 discusses standard leave requests and procedure details.",
        "Paragraph 2 discusses annual medical leave allowances and requirements.",
        "Paragraph 3 discusses study leave and professional certifications.",
        "Paragraph 4 discusses parental leave and sabbatical options.",
    ]
    full_text = "\n\n".join(paragraphs)
    chunks = chunker.chunk_document("doc-3", "Leave Policy", full_text)

    assert len(chunks) > 1
    # First chunk starts with standard header
    assert chunks[0].content.startswith("=== Leave Policy ===")
    # Subsequent chunks use continued header
    for c in chunks[1:]:
        assert c.content.startswith("=== Leave Policy (continued) ===")
        assert c.token_count > 0


def test_chunk_token_count_positive():
    chunker = RecursiveTextChunker()
    chunks = chunker.chunk_document("doc-4", "Test", "Short text.")
    assert len(chunks) == 1
    assert chunks[0].token_count >= 1
