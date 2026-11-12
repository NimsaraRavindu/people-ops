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

import math
import pytest
from src.ingestion.embedder import TextEmbedder


@pytest.mark.asyncio
async def test_dummy_embed_query_properties():
    embedder = TextEmbedder()
    embedder.provider = "dummy"

    vec = await embedder.embed_query("How many days of leave do I get?")
    assert len(vec) == 768

    # Check unit normalization (L2 norm should be ~1.0)
    norm = math.sqrt(sum(v * v for v in vec))
    assert abs(norm - 1.0) < 1e-4


@pytest.mark.asyncio
async def test_dummy_embed_deterministic():
    embedder = TextEmbedder()
    embedder.provider = "dummy"

    vec1 = await embedder.embed_query("Annual leave policy 2026")
    vec2 = await embedder.embed_query("Annual leave policy 2026")
    vec3 = await embedder.embed_query("Maternity leave policy 2026")

    # Same input produces identical vectors
    assert vec1 == vec2
    # Different input produces different vectors
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
