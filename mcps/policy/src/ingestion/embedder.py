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
# Includes a thread-safe LRU TTL query cache for instant repeat query resolution.

import asyncio
import hashlib
import logging
import math
import threading
import time
from collections import OrderedDict
from typing import Any, Callable, List, Optional, Tuple

from ..config import settings

logger = logging.getLogger("policy_mcp.embedder")

_fastembed_model: Optional[Any] = None
_model_lock = threading.Lock()


def normalize_cache_key(text: str) -> str:
    """Canonicalizes a query so trivially different whitespace/case strings share one cache entry."""
    return " ".join(text.lower().split())


class QueryEmbeddingCache:
    """Thread-safe, size-bounded LRU cache with per-entry TTL for query embeddings."""

    def __init__(
        self,
        capacity: int = 512,
        ttl_seconds: float = 1800.0,
        now_fn: Callable[[], float] = time.time,
    ) -> None:
        self.capacity = capacity
        self.ttl_seconds = ttl_seconds
        self.now_fn = now_fn
        self._lock = threading.Lock()
        self._items: "OrderedDict[str, Tuple[List[float], float]]" = OrderedDict()

    def get(self, key: str) -> Optional[List[float]]:
        """Returns a copy of the cached vector for the key, evicting it if expired."""
        if self.capacity <= 0:
            return None
        with self._lock:
            entry = self._items.get(key)
            if entry is None:
                return None
            vec, expires_at = entry
            if self.ttl_seconds > 0 and self.now_fn() > expires_at:
                del self._items[key]
                return None
            self._items.move_to_end(key)
            return list(vec)

    def put(self, key: str, vector: List[float]) -> None:
        """Stores a copy of the vector, evicting the least recently used entry when full."""
        if self.capacity <= 0:
            return
        stored = list(vector)
        expires_at = self.now_fn() + self.ttl_seconds
        with self._lock:
            if key in self._items:
                self._items[key] = (stored, expires_at)
                self._items.move_to_end(key)
                return
            self._items[key] = (stored, expires_at)
            if len(self._items) > self.capacity:
                self._items.popitem(last=False)

    def clear(self) -> None:
        """Clears all cached entries."""
        with self._lock:
            self._items.clear()

    def __len__(self) -> int:
        with self._lock:
            return len(self._items)


def _get_fastembed_model(model_name: str) -> Any:
    """Lazily initializes the FastEmbed TextEmbedding model on CPU (thread-safe)."""
    global _fastembed_model
    with _model_lock:
        if _fastembed_model is None:
            try:
                import os
                from fastembed import TextEmbedding

                cache_dir = os.getenv("FASTEMBED_CACHE_PATH")
                logger.info("Initializing local FastEmbed model: %s...", model_name)
                if cache_dir:
                    _fastembed_model = TextEmbedding(model_name=model_name, cache_dir=cache_dir)
                else:
                    _fastembed_model = TextEmbedding(model_name=model_name)
                logger.info("FastEmbed model %s initialized successfully.", model_name)
            except ImportError as e:
                if settings.ENVIRONMENT.lower() in ("production", "prod", "staging"):
                    raise RuntimeError(
                        "fastembed is required in production when EMBEDDING_PROVIDER is 'local'."
                    ) from e
                logger.warning(
                    "fastembed is not installed. Falling back to dummy embeddings (dev mode only)."
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

    def __init__(self) -> None:
        self.provider = settings.EMBEDDING_PROVIDER.lower()
        self.model = settings.EMBEDDING_MODEL
        self.dimension = settings.EMBEDDING_DIMENSION
        self.cache = QueryEmbeddingCache(
            capacity=settings.EMBEDDING_CACHE_CAPACITY,
            ttl_seconds=settings.EMBEDDING_CACHE_TTL_SECONDS,
        )

    async def warmup(self) -> None:
        """Pre-warms the local ONNX embedding model at service startup to avoid cold-start latency."""
        if self.provider in ("local", "fastembed"):
            await asyncio.to_thread(_get_fastembed_model, self.model)

    async def embed_query(self, query: str) -> List[float]:
        """Generates an embedding vector for a single search query with LRU caching."""
        cache_key = f"{self.provider}:{self.model}:{normalize_cache_key(query)}"
        if cache_key:
            cached = self.cache.get(cache_key)
            if cached is not None:
                return cached

        results = await self.embed_texts([query])
        vec = results[0]
        if cache_key:
            self.cache.put(cache_key, vec)
        return vec

    async def embed_texts(self, texts: List[str]) -> List[List[float]]:
        """Generates embedding vectors for a list of text strings in batch."""
        if not texts:
            return []

        if self.provider in ("local", "fastembed"):
            return await self._embed_local(texts)
        if self.provider == "dummy":
            return self._embed_dummy(texts)
        raise ValueError(
            f"Unsupported EMBEDDING_PROVIDER '{self.provider}'. "
            f"Supported providers: 'local', 'fastembed', 'dummy'."
        )

    async def _embed_local(self, texts: List[str]) -> List[List[float]]:
        """Generates embeddings locally on CPU using FastEmbed ONNX runtime."""
        model = await asyncio.to_thread(_get_fastembed_model, self.model)
        if model is None:
            if settings.EMBEDDING_MOCK_FALLBACK and settings.ENVIRONMENT.lower() in (
                "development",
                "dev",
                "test",
                "testing",
                "local",
            ):
                return self._embed_dummy(texts)
            raise RuntimeError(
                "Local FastEmbed model failed to load. Halted to prevent corrupt vector data."
            )

        def _compute() -> List[List[float]]:
            embeddings_generator = model.embed(texts)
            results = [e.tolist() for e in embeddings_generator]
            if results and len(results[0]) != self.dimension:
                raise ValueError(
                    f"Model embedding dimension mismatch: expected {self.dimension}, "
                    f"got {len(results[0])}"
                )
            return results

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
                byte_val = seed_hash[(i * 7) % len(seed_hash)]
                val = (byte_val / 127.5) - 1.0
                vec.append(val)

            norm = math.sqrt(sum(v * v for v in vec)) or 1.0
            vec = [v / norm for v in vec]
            vectors.append(vec)

        return vectors


embedder = TextEmbedder()
