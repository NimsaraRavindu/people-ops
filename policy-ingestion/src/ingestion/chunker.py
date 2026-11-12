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

import re
from dataclasses import dataclass
from typing import List

from ..config import settings


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
        chars_per_token: float = 3.8
    ):
        self.chunk_size_chars = int(chunk_size_tokens * chars_per_token)
        self.overlap_chars = int(chunk_overlap_tokens * chars_per_token)
        self.chars_per_token = chars_per_token
        self.separators = ["\n\n", "\n", ". ", "; ", ", ", " "]

    def chunk_document(self, doc_id: str, doc_name: str, text: str) -> List[TextChunk]:
        """Splits full document text into contextual chunks."""
        cleaned_text = re.sub(r"\r\n|\r", "\n", text).strip()
        if not cleaned_text:
            return []

        raw_chunks = self._split_text(cleaned_text, self.separators)
        chunks: List[TextChunk] = []

        for idx, raw in enumerate(raw_chunks):
            header = f"=== {doc_name} ===" if idx == 0 else f"=== {doc_name} (continued) ==="
            full_content = f"{header}\n{raw.strip()}"
            token_count = max(1, int(len(full_content) / self.chars_per_token))

            chunks.append(
                TextChunk(
                    document_id=doc_id,
                    chunk_index=idx,
                    content=full_content,
                    token_count=token_count
                )
            )

        return chunks

    def _split_text(self, text: str, separators: List[str]) -> List[str]:
        """Recursively breaks text using the primary available separator."""
        if len(text) <= self.chunk_size_chars or not separators:
            return [text] if text.strip() else []

        sep = separators[0]
        remaining_seps = separators[1:]
        splits = text.split(sep)

        chunks: List[str] = []
        current_buffer = ""

        for s in splits:
            part = s + sep if sep != " " else s + " "
            if len(current_buffer) + len(part) <= self.chunk_size_chars:
                current_buffer += part
            else:
                if current_buffer.strip():
                    chunks.append(current_buffer.strip())
                # If a single atomic section exceeds chunk size, recurse on finer separators
                if len(part) > self.chunk_size_chars and remaining_seps:
                    deeper_chunks = self._split_text(part, remaining_seps)
                    chunks.extend(deeper_chunks)
                    current_buffer = ""
                else:
                    # Apply overlap from previous segment if feasible
                    overlap = (
                        current_buffer[-self.overlap_chars:]
                        if len(current_buffer) > self.overlap_chars
                        else ""
                    )
                    current_buffer = overlap + part

        if current_buffer.strip():
            chunks.append(current_buffer.strip())

        return chunks


chunker = RecursiveTextChunker(
    chunk_size_tokens=settings.CHUNK_SIZE_TOKENS,
    chunk_overlap_tokens=settings.CHUNK_OVERLAP_TOKENS
)
