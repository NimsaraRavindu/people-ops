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

"""Unit tests for TextEmbedder and QueryEmbeddingCache."""

import math
import pytest
from src.ingestion.embedder import QueryEmbeddingCache, TextEmbedder, normalize_cache_key


@pytest.mark.asyncio
async def test_dummy_embed_query_properties():
    embedder = TextEmbedder()
    embedder.provider = "dummy"

    vec = await embedder.embed_query("How many days of leave do I get?")
    assert len(vec) == 768

    norm = math.sqrt(sum(v * v for v in vec))
    assert abs(norm - 1.0) < 1e-4


@pytest.mark.asyncio
async def test_dummy_embed_deterministic():
    embedder = TextEmbedder()
    embedder.provider = "dummy"

    vec1 = await embedder.embed_query("Annual leave policy 2026")
    vec2 = await embedder.embed_query("Annual leave policy 2026")
    vec3 = await embedder.embed_query("Maternity leave policy 2026")

    assert vec1 == vec2
    assert vec1 != vec3


@pytest.mark.asyncio
async def test_dummy_embed_batch_texts():
    embedder = TextEmbedder()
    embedder.provider = "dummy"

    texts = [
        "First excerpt about medical leave.",
        "Second excerpt about paternity leave.",
        "Third excerpt about unpaid sabbatical.",
    ]
    vectors = await embedder.embed_texts(texts)

    assert len(vectors) == 3
    for v in vectors:
        assert len(v) == 768
        norm = math.sqrt(sum(x * x for x in v))
        assert abs(norm - 1.0) < 1e-4


@pytest.mark.asyncio
async def test_embed_empty_texts():
    embedder = TextEmbedder()
    vectors = await embedder.embed_texts([])
    assert vectors == []


def test_query_embedding_cache_lru_and_ttl():
    fake_time = [1000.0]
    cache = QueryEmbeddingCache(capacity=2, ttl_seconds=60.0, now_fn=lambda: fake_time[0])

    k1 = normalize_cache_key("  What is   Casual Leave? ")
    k2 = normalize_cache_key("what is casual leave?")
    assert k1 == k2 == "what is casual leave?"

    cache.put("q1", [1.0, 2.0])
    cache.put("q2", [3.0, 4.0])
    assert len(cache) == 2

    # Access q1 so q2 becomes least recently used
    assert cache.get("q1") == [1.0, 2.0]
    cache.put("q3", [5.0, 6.0])
    assert cache.get("q2") is None
    assert cache.get("q1") == [1.0, 2.0]
    assert cache.get("q3") == [5.0, 6.0]

    # Advance time past TTL
    fake_time[0] += 61.0
    assert cache.get("q1") is None
