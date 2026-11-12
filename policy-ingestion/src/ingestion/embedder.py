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

# Local ONNX vector embedding client using FastEmbed and BAAI/bge-base-en-v1.5 (768 dimensions).
# Requires zero external API keys and runs completely on CPU.

import asyncio
import hashlib
import logging
import math
from typing import Any, List, Optional

from ..config import settings

logger = logging.getLogger("policy_ingestion.embedder")

_fastembed_model: Optional[Any] = None
_model_lock = asyncio.Lock()


def _get_fastembed_model(model_name: str) -> Any:
    """Lazily initializes the FastEmbed TextEmbedding model on CPU."""
    global _fastembed_model
    if _fastembed_model is None:
        try:
            from fastembed import TextEmbedding

            logger.info("Initializing local FastEmbed model: %s...", model_name)
            _fastembed_model = TextEmbedding(model_name=model_name)
            logger.info("FastEmbed model %s initialized successfully.", model_name)
        except ImportError as e:
            if settings.ENVIRONMENT.lower() in ("production", "prod", "staging"):
                raise RuntimeError(
                    "fastembed is required in production environments when EMBEDDING_PROVIDER is 'local'."
                ) from e
            logger.warning(
                "fastembed is not installed. Falling back to deterministic dummy embeddings (dev mode only)."
            )
            return None
        except Exception as e:
            if settings.ENVIRONMENT.lower() in ("production", "prod", "staging"):
                raise RuntimeError(
                    f"Failed to load local FastEmbed model {model_name} in production: {e}"
                ) from e
            logger.error("Failed to load local FastEmbed model %s: %s", model_name, e)
            return None
    return _fastembed_model


class TextEmbedder:
    """Generates 768-dimensional semantic embeddings for document chunks and queries."""

    def __init__(self):
        self.provider = settings.EMBEDDING_PROVIDER.lower()
        self.model = settings.EMBEDDING_MODEL
        self.dimension = settings.EMBEDDING_DIMENSION

    async def embed_query(self, query: str) -> List[float]:
        """Generates an embedding vector for a single search query."""
        results = await self.embed_texts([query])
        return results[0]

    async def embed_texts(self, texts: List[str]) -> List[List[float]]:
        """Generates embedding vectors for a list of text strings in batch."""
        if not texts:
            return []

        if self.provider in ("local", "fastembed"):
            return await self._embed_local(texts)
        elif self.provider == "dummy":
            return self._embed_dummy(texts)
        else:
            logger.warning(
                "Unknown embedding provider '%s', defaulting to local FastEmbed.",
                self.provider,
            )
            return await self._embed_local(texts)

    async def _embed_local(self, texts: List[str]) -> List[List[float]]:
        """Generates embeddings locally on CPU using FastEmbed ONNX runtime."""
        model = _get_fastembed_model(self.model)
        if model is None:
            if settings.ENVIRONMENT.lower() in ("production", "prod", "staging"):
                raise RuntimeError(
                    "Local FastEmbed model is unavailable. Ingestion halted to prevent database corruption."
                )
            return self._embed_dummy(texts)

        # Offload ONNX CPU compute to a worker thread to keep the asyncio event loop responsive
        def _compute() -> List[List[float]]:
            embeddings_generator = model.embed(texts)
            return [e.tolist() for e in embeddings_generator]

        return await asyncio.to_thread(_compute)

    def _embed_dummy(self, texts: List[str]) -> List[List[float]]:
        """
        Deterministic, unit-normalized pseudo-embedding for testing and offline environments.
        Generates repeatable vectors from text hashes without external network calls.
        """
        vectors: List[List[float]] = []
        for text in texts:
            vec: List[float] = []
            seed_hash = hashlib.sha256(text.encode("utf-8")).digest()
            for i in range(self.dimension):
                # Pseudo-random float in [-1.0, 1.0] derived from hash cycle
                byte_val = seed_hash[(i * 7) % len(seed_hash)]
                val = (byte_val / 127.5) - 1.0
                vec.append(val)

            # L2 normalize
            norm = math.sqrt(sum(v * v for v in vec)) or 1.0
            vec = [v / norm for v in vec]
            vectors.append(vec)

        return vectors


embedder = TextEmbedder()
