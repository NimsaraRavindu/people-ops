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


def test_chunk_zero_overlap():
    chunker = RecursiveTextChunker(
        chunk_size_tokens=25,
        chunk_overlap_tokens=0,
        chars_per_token=3.0
    )
    text = " ".join([f"Sentence number {i} is here with some additional words." for i in range(10)])
    chunks = chunker.chunk_document("doc-5", "No Overlap Policy", text)
    assert len(chunks) >= 2
    for c in chunks:
        assert c.token_count > 0


def test_chunk_separator_free_oversized_text():
    chunker = RecursiveTextChunker(
        chunk_size_tokens=50,
        chunk_overlap_tokens=10,
        chars_per_token=4.0
    )
    # A single continuous unbroken string of 1000 characters without any whitespace or punctuation
    unbroken_text = "a" * 1000
    chunks = chunker.chunk_document("doc-unbroken", "Unbroken Policy", unbroken_text)
    assert len(chunks) > 1
    for chunk in chunks:
        assert len(chunk.content) <= chunker.chunk_size_chars


def test_chunk_oversized_document_name():
    chunker = RecursiveTextChunker()
    long_name = "A" * 300
    text = "Important policy body content that should be chunked properly."
    chunks = chunker.chunk_document("doc-long", long_name, text)
    assert len(chunks) == 1
    header_line = chunks[0].content.split("\n")[0]
    assert len(header_line) <= 100
    assert "..." in header_line
    assert len(chunks[0].content) <= chunker.chunk_size_chars


def test_chunk_short_section_followed_by_oversized_section():
    chunker = RecursiveTextChunker(
        chunk_size_tokens=40,
        chunk_overlap_tokens=10,
        chars_per_token=3.0
    )
    text = "Intro paragraph.\n\n" + ("x" * 500)
    chunks = chunker.chunk_document("doc-boundary", "Boundary Policy", text)
    assert len(chunks) >= 2
    assert "Intro paragraph." in chunks[0].content
    # Preserves boundary overlap into subsequent chunk
    assert "Intro paragraph." in chunks[1].content
    assert all(len(c.content) <= chunker.chunk_size_chars for c in chunks)
    # Check source text coverage: all 500 'x' characters are represented
    x_total = sum(c.content.count("x") for c in chunks)
    assert x_total >= 500


def test_chunk_token_dense_text():
    chunker = RecursiveTextChunker(chunk_size_tokens=100)
    dense_text = "@#$%^&*()_+{}|:<>?[];',./ " * 40
    chunks = chunker.chunk_document("doc-dense", "Dense Symbols", dense_text)
    assert len(chunks) >= 1
    for c in chunks:
        assert c.token_count <= chunker.chunk_size_tokens


def test_chunk_header_too_large_raises():
    import pytest
    chunker = RecursiveTextChunker(
        chunk_size_tokens=5,
        chars_per_token=2.0
    )
    with pytest.raises(ValueError, match="too small to fit document header"):
        chunker.chunk_document("doc-err", "Standard Policy Name", "Some text")


def test_chunk_header_token_budget_exhausted_raises():
    import pytest
    # Header tokens for "=== Standard Long Policy Name (continued) ===" is ~12 tokens
    chunker = RecursiveTextChunker(
        chunk_size_tokens=10,
        chars_per_token=4.0
    )
    with pytest.raises(ValueError, match="too small to fit document header"):
        chunker.chunk_document("doc-token-budget", "Standard Long Policy Name", "Some text")


def test_chunk_source_text_coverage():
    chunker = RecursiveTextChunker(chunk_size_tokens=60, chunk_overlap_tokens=10)
    words = ["Alpha", "Beta", "Gamma", "Delta", "Epsilon", "Zeta", "Eta", "Theta", "Iota", "Kappa"]
    text = "\n\n".join(f"Section {w}: Content for policy {w} with details." for w in words)
    chunks = chunker.chunk_document("doc-coverage", "Coverage Policy", text)
    combined = " ".join(c.content for c in chunks)
    for w in words:
        assert f"Section {w}" in combined
        assert f"details." in combined


def test_chunk_short_section_followed_by_oversized_distinct_chars():
    chunker = RecursiveTextChunker(
        chunk_size_tokens=30,
        chunk_overlap_tokens=5,
        chars_per_token=3.0
    )
    short_part = "ABCDEFGHIJKLM"
    distinct_oversized = "".join(f"[{i:03d}-{chr(65 + (i % 26))}]" for i in range(40))
    text = f"{short_part}\n\n{distinct_oversized}"
    chunks = chunker.chunk_document("doc-distinct", "Distinct Policy", text)
    assert len(chunks) >= 2
    # Verify boundary overlap retention
    for i in range(1, len(chunks)):
        prev_body = "\n".join(chunks[i - 1].content.split("\n")[1:])
        curr_body = "\n".join(chunks[i].content.split("\n")[1:])
        tail = prev_body[-chunker.overlap_chars:]
        assert tail in curr_body or any(tail[-k:] in curr_body for k in range(3, len(tail) + 1))
    # Verify ordered source text coverage
    combined = "".join("\n".join(c.content.split("\n")[1:]) for c in chunks)
    assert short_part in combined
    last_pos = -1
    for i in range(40):
        token_pattern = f"[{i:03d}-{chr(65 + (i % 26))}]"
        pos = combined.find(token_pattern)
        assert pos != -1, f"Missing marker {token_pattern}"
        assert pos >= last_pos, f"Out-of-order marker {token_pattern}: pos={pos} < last_pos={last_pos}"
        last_pos = pos


def test_chunk_section_exact_length_of_body_budget():
    from src.ingestion.chunker import _format_chunk_header
    chunker = RecursiveTextChunker(
        chunk_size_tokens=30,
        chunk_overlap_tokens=5,
        chars_per_token=3.0
    )
    header = _format_chunk_header("Policy", continued=True)
    body_budget = chunker.chunk_size_chars - (len(header) + 1)
    sec1 = "Short section."
    sec2 = "Y" * body_budget
    text = f"{sec1}\n\n{sec2}"
    chunks = chunker.chunk_document("doc-exact", "Policy", text)
    assert len(chunks) >= 2
    for c in chunks:
        assert len(c.content) <= chunker.chunk_size_chars
        assert c.token_count <= chunker.chunk_size_tokens


def test_count_tokens_without_truncation_on_long_text():
    from src.ingestion.chunker import count_tokens
    # Text with more than 512 tokens
    long_text = "word " * 700
    tokens = count_tokens(long_text)
    # FastEmbed bge model limit is 512, counter must not truncate
    assert tokens > 512


def test_chunker_exceeding_max_model_token_limit_raises():
    import pytest
    with pytest.raises(ValueError, match="cannot exceed model limit"):
        RecursiveTextChunker(chunk_size_tokens=513)


def test_chunk_token_dense_text_independent_tokenizer():
    from tokenizers import Tokenizer
    from src.ingestion.embedder import _get_fastembed_model
    from src.config import settings

    chunker = RecursiveTextChunker(chunk_size_tokens=512)
    dense_text = "@#$%^&*()_+{}|:<>?[];',./ " * 200
    chunks = chunker.chunk_document("doc-dense-indep", "Dense Symbols", dense_text)
    assert len(chunks) >= 1

    model = _get_fastembed_model(settings.EMBEDDING_MODEL)
    assert model is not None, "FastEmbed model must be available for independent tokenizer verification"
    assert hasattr(model, "model") and hasattr(model.model, "tokenizer"), (
        "FastEmbed model must expose tokenizer"
    )
    independent_tok = Tokenizer.from_str(model.model.tokenizer.to_str())
    independent_tok.no_truncation()
    for c in chunks:
        true_count = len(independent_tok.encode(c.content).ids)
        assert true_count <= 512, f"Chunk exceeded 512 token limit: true_count={true_count}"


def test_count_tokens_fail_closed_on_local_tokenizer_error():
    import pytest
    from unittest.mock import patch
    from src.ingestion.chunker import count_tokens

    with patch("src.ingestion.chunker._get_untruncated_tokenizer", return_value=None), \
         patch("src.config.settings.EMBEDDING_PROVIDER", "fastembed"):
        with pytest.raises(RuntimeError, match="FastEmbed tokenizer unavailable"):
            count_tokens("Test text")
