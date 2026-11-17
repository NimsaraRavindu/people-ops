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

# Recursive character chunker with contextual document headers.

import math
import re
import threading
from dataclasses import dataclass
from typing import List, Optional

from ..config import settings

MAX_HEADER_TITLE_LENGTH = 80
MAX_MODEL_TOKEN_LIMIT = 512


def _format_chunk_header(doc_name: str, continued: bool = False) -> str:
    """Formats document header with bounded length to preserve body token budget."""
    name = doc_name.strip()
    if len(name) > MAX_HEADER_TITLE_LENGTH:
        name = name[:MAX_HEADER_TITLE_LENGTH - 3] + "..."
    suffix = " (continued)" if continued else ""
    return f"=== {name}{suffix} ==="


_untruncated_tokenizer = None
_tokenizer_lock = threading.Lock()


def _get_untruncated_tokenizer():
    """Lazily creates and caches an untruncated tokenizer for accurate full-text token counts."""
    global _untruncated_tokenizer
    if _untruncated_tokenizer is not None:
        return _untruncated_tokenizer
    with _tokenizer_lock:
        if _untruncated_tokenizer is not None:
            return _untruncated_tokenizer
        try:
            from .embedder import _get_fastembed_model
            from tokenizers import Tokenizer

            model = _get_fastembed_model(settings.EMBEDDING_MODEL)
            if model is not None and hasattr(model, "model") and hasattr(model.model, "tokenizer"):
                cloned = Tokenizer.from_str(model.model.tokenizer.to_str())
                cloned.no_truncation()
                _untruncated_tokenizer = cloned
                return _untruncated_tokenizer
        except Exception as exc:
            if settings.EMBEDDING_PROVIDER != "openai":
                raise RuntimeError(
                    f"Failed to initialize FastEmbed untruncated tokenizer: {exc}"
                ) from exc
        if settings.EMBEDDING_PROVIDER != "openai":
            raise RuntimeError(
                f"FastEmbed model or tokenizer unavailable for model {settings.EMBEDDING_MODEL}"
            )
        return None


def count_tokens(text: str) -> int:
    """Accurately counts tokens without truncation using model tokenizer or conservative fallback."""
    tok = _get_untruncated_tokenizer()
    if tok is not None:
        try:
            return len(tok.encode(text).ids)
        except Exception as exc:
            if settings.EMBEDDING_PROVIDER != "openai":
                raise RuntimeError(f"FastEmbed tokenizer failed to encode text: {exc}") from exc
    elif settings.EMBEDDING_PROVIDER != "openai":
        raise RuntimeError("FastEmbed tokenizer unavailable for local embedding token counting")

    # Safe upper bound fallback for external tokenizers (e.g., OpenAI) when local tokenizer is unset
    return max(len(text) + 2, math.ceil(len(text) * 1.5) + 2)


@dataclass
class TextChunk:
    """Represents an embedded document excerpt."""
    document_id: str
    chunk_index: int
    content: str
    token_count: int


class RecursiveTextChunker:
    """
    Splits text recursively by paragraphs, lines, and sentences,
    maintaining token overlap and prepending contextual document headers.
    """

    def __init__(
        self,
        chunk_size_tokens: int = 512,
        chunk_overlap_tokens: int = 50,
        chars_per_token: float = 3.2
    ):
        if chunk_size_tokens > MAX_MODEL_TOKEN_LIMIT:
            raise ValueError(
                f"Configured chunk_size_tokens ({chunk_size_tokens}) cannot exceed model limit "
                f"({MAX_MODEL_TOKEN_LIMIT})."
            )
        self.chunk_size_tokens = chunk_size_tokens
        self.chunk_overlap_tokens = chunk_overlap_tokens
        self.chunk_size_chars = int(chunk_size_tokens * chars_per_token)
        self.overlap_chars = int(chunk_overlap_tokens * chars_per_token)
        self.chars_per_token = chars_per_token
        self.separators = ["\n\n", "\n", ". ", "; ", ", ", " "]

    def chunk_document(self, doc_id: str, doc_name: str, text: str) -> List[TextChunk]:
        """Splits full document text into contextual chunks within token budget."""
        cleaned_text = re.sub(r"\r\n|\r", "\n", text).strip()
        if not cleaned_text:
            return []

        # Account for bounded header overhead so total chunk with header never exceeds budget
        continued_header = _format_chunk_header(doc_name, continued=True)
        header_tokens = count_tokens(continued_header + "\n")
        header_overhead = len(continued_header) + 1  # +1 for newline character

        if self.chunk_size_tokens <= header_tokens:
            raise ValueError(
                f"Configured chunk_size_tokens ({self.chunk_size_tokens}) is too small to fit document header "
                f"({header_tokens} tokens)."
            )

        if self.chunk_size_chars <= header_overhead:
            raise ValueError(
                f"Configured chunk_size_chars ({self.chunk_size_chars}) is too small to fit document header "
                f"({header_overhead} chars)."
            )

        body_budget = self.chunk_size_chars - header_overhead
        body_overlap = min(self.overlap_chars, max(0, body_budget // 2))

        raw_chunks = self._split_text(cleaned_text, self.separators, body_budget, body_overlap)
        chunks: List[TextChunk] = []

        for raw in raw_chunks:
            is_cont = len(chunks) > 0
            header = _format_chunk_header(doc_name, continued=is_cont)
            full_content = f"{header}\n{raw.strip()}"
            token_count = count_tokens(full_content)

            if token_count <= self.chunk_size_tokens:
                chunks.append(
                    TextChunk(
                        document_id=doc_id,
                        chunk_index=len(chunks),
                        content=full_content,
                        token_count=token_count,
                    )
                )
            else:
                sub_chunks = self._subdivide_to_token_budget(
                    doc_id, doc_name, raw.strip(), is_cont
                )
                for sc in sub_chunks:
                    sc.chunk_index = len(chunks)
                    chunks.append(sc)

        return chunks

    def _subdivide_to_token_budget(
        self,
        doc_id: str,
        doc_name: str,
        text: str,
        is_continued: bool,
    ) -> List[TextChunk]:
        """Subdivides body text iteratively until every chunk respects the token budget."""
        results: List[TextChunk] = []
        remaining = text.strip()

        while remaining:
            header = _format_chunk_header(doc_name, continued=(is_continued or len(results) > 0))
            if count_tokens(f"{header}\n{remaining}") <= self.chunk_size_tokens:
                results.append(
                    TextChunk(
                        document_id=doc_id,
                        chunk_index=0,
                        content=f"{header}\n{remaining}",
                        token_count=count_tokens(f"{header}\n{remaining}"),
                    )
                )
                break

            low = 1
            high = len(remaining)
            best_idx = 1

            while low <= high:
                mid = (low + high) // 2
                cand = f"{header}\n{remaining[:mid].strip()}"
                if count_tokens(cand) <= self.chunk_size_tokens:
                    best_idx = mid
                    low = mid + 1
                else:
                    high = mid - 1

            prefix = remaining[:best_idx]
            for sep in ["\n\n", "\n", ". ", "; ", ", ", " "]:
                last_sep = prefix.rfind(sep)
                if last_sep > max(10, best_idx // 2):
                    best_idx = last_sep + len(sep)
                    prefix = remaining[:best_idx]
                    break

            content = f"{header}\n{prefix.strip()}"
            results.append(
                TextChunk(
                    document_id=doc_id,
                    chunk_index=0,
                    content=content,
                    token_count=count_tokens(content),
                )
            )

            overlap = min(self.overlap_chars, len(prefix) // 3)
            step = max(1, len(prefix) - overlap)
            remaining = remaining[step:].strip()

        return results

    def _split_text(
        self,
        text: str,
        separators: List[str],
        target_chunk_chars: Optional[int] = None,
        target_overlap_chars: Optional[int] = None,
    ) -> List[str]:
        """Recursively breaks text using available separators with hard-slice fallback."""
        max_chars = target_chunk_chars if target_chunk_chars is not None else self.chunk_size_chars
        overlap_chars = target_overlap_chars if target_overlap_chars is not None else self.overlap_chars

        if len(text) <= max_chars:
            return [text] if text.strip() else []

        if not separators:
            # Fallback when separators exhausted: hard slice to guarantee upper bound
            chunks: List[str] = []
            step = max(1, max_chars - overlap_chars)
            for i in range(0, len(text), step):
                slice_part = text[i : i + max_chars]
                if slice_part.strip():
                    chunks.append(slice_part.strip())
            return chunks

        sep = separators[0]
        remaining_seps = separators[1:]
        splits = [s.strip() for s in text.split(sep) if s.strip()]

        chunks: List[str] = []
        current_buffer = ""
        pending_overlap = ""

        for s in splits:
            part = s + (sep if sep != " " else " ")
            if len(current_buffer) + len(part) <= max_chars:
                current_buffer += part
            else:
                prev_tail = pending_overlap
                if current_buffer.strip():
                    prev_text = current_buffer.strip()
                    chunks.append(prev_text)
                    prev_tail = prev_text[-overlap_chars:] if overlap_chars > 0 else ""
                    current_buffer = ""
                    pending_overlap = ""

                # If a single atomic section exceeds chunk size, recurse on finer separators
                if len(s) > max_chars:
                    deeper_chunks = self._split_text(
                        s, remaining_seps, max_chars, overlap_chars
                    )
                    if deeper_chunks:
                        if prev_tail and not deeper_chunks[0].startswith(prev_tail):
                            avail = max_chars - len(prev_tail) - 1
                            if len(deeper_chunks[0]) <= avail:
                                deeper_chunks[0] = f"{prev_tail}\n{deeper_chunks[0]}"
                            else:
                                chunk_0 = f"{prev_tail}\n{deeper_chunks[0][:avail]}"
                                overlap_tail = deeper_chunks[0][max(0, avail - overlap_chars) : avail]
                                chunk_1_body = overlap_tail + deeper_chunks[0][avail:]
                                deeper_chunks[0] = chunk_0
                                if len(chunk_1_body) <= max_chars:
                                    deeper_chunks.insert(1, chunk_1_body)
                                else:
                                    sub_slices = []
                                    step = max(1, max_chars - overlap_chars)
                                    for i in range(0, len(chunk_1_body), step):
                                        p = chunk_1_body[i : i + max_chars]
                                        if p:
                                            sub_slices.append(p)
                                    for idx, sl in enumerate(sub_slices):
                                        deeper_chunks.insert(1 + idx, sl)
                        chunks.extend(deeper_chunks)
                        pending_overlap = (
                            deeper_chunks[-1][-overlap_chars:]
                            if overlap_chars > 0
                            else ""
                        )
                        current_buffer = ""
                else:
                    take_len = min(overlap_chars, max(0, max_chars - len(s) - 1))
                    overlap = (
                        prev_tail[-take_len:]
                        if (prev_tail and take_len > 0)
                        else ""
                    )
                    current_buffer = (overlap + " " + s).strip() if overlap else s
                    pending_overlap = ""

        if current_buffer.strip():
            chunks.append(current_buffer.strip())

        return chunks


chunker = RecursiveTextChunker(
    chunk_size_tokens=settings.CHUNK_SIZE_TOKENS,
    chunk_overlap_tokens=settings.CHUNK_OVERLAP_TOKENS
)
