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

import asyncio
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Any, Dict, List, Tuple
import asyncpg
import numpy as np
import pytest

from src.config import settings

REPORT_PATH = Path(__file__).parent / "policy_rag_model_comparison_report.md"

RAG_CANDIDATE_MODELS = [
    "BAAI/bge-base-en-v1.5",
    "BAAI/bge-small-en-v1.5",
    "sentence-transformers/all-MiniLM-L6-v2",
    "snowflake/snowflake-arctic-embed-xs",
    "snowflake/snowflake-arctic-embed-s",
    "snowflake/snowflake-arctic-embed-m",
    "nomic-ai/nomic-embed-text-v1.5-Q",
    "jinaai/jina-embeddings-v2-small-en",
    "jinaai/jina-embeddings-v2-base-en",
    "thenlper/gte-base",
]

# 25 ground-truth RAG evaluation queries spanning all 8 ingested WSO2 Policy & Training documents.
# Queries marked is_tail_fact=True target facts located past character 1,100 (>256 tokens) of a chunk.
RAG_BENCHMARK_QUERIES: List[Dict[str, Any]] = [
    {
        "id": "rag-01",
        "folder": "policy",
        "query": "How many months in advance must I submit a sabbatical leave application?",
        "expected_doc": "Sabbatical Leave Policy",
        "must_contain": ["6 months"],
        "is_tail_fact": False,
    },
    {
        "id": "rag-02",
        "folder": "policy",
        "query": "What is the maximum duration of paid sabbatical leave and can it be combined with annual leave?",
        "expected_doc": "Sabbatical Leave Policy",
        "must_contain": ["6 weeks", "full pay"],
        "is_tail_fact": True,
    },
    {
        "id": "rag-03",
        "folder": "policy",
        "query": "What job band and years of continuous service are required for sabbatical eligibility?",
        "expected_doc": "Sabbatical Leave Policy",
        "must_contain": ["band 5", "3 years"],
        "is_tail_fact": False,
    },
    {
        "id": "rag-04",
        "folder": "policy",
        "query": "How much tuition fee reimbursement and interest-free loan does WSO2 provide for study programs?",
        "expected_doc": "WSO2 Study Programs - V1.3",
        "must_contain": ["300,000", "50%"],
        "is_tail_fact": True,
    },
    {
        "id": "rag-05",
        "folder": "policy",
        "query": "What happens if an employee does not complete their sponsored study program on time?",
        "expected_doc": "WSO2 Study Programs - V1.3",
        "must_contain": ["equal monthly installments", "extensions will be granted"],
        "is_tail_fact": True,
    },
    {
        "id": "rag-06",
        "folder": "policy",
        "query": "When did the WSO2 employee study program financial support take effect?",
        "expected_doc": "WSO2 Study Programs - V1.3",
        "must_contain": ["august 2016"],
        "is_tail_fact": True,
    },
    {
        "id": "rag-07",
        "folder": "policy",
        "query": "How is arrival day per diem calculated if my flight lands before 12 PM vs after 12 PM?",
        "expected_doc": "WSO2 Travel Policy - V3.3",
        "must_contain": ["arrive before 12 pm", "100%", "50%"],
        "is_tail_fact": True,
    },
    {
        "id": "rag-08",
        "folder": "policy",
        "query": "If I find cheaper hotel accommodation than what WSO2 pays, do I get a share of the savings?",
        "expected_doc": "WSO2 Travel Policy - V3.3",
        "must_contain": ["cheaper accommodation", "50% of that savings"],
        "is_tail_fact": False,
    },
    {
        "id": "rag-09",
        "folder": "policy",
        "query": "What should I do if two or more employees are traveling to and from the airport together?",
        "expected_doc": "WSO2 Travel Policy - V3.3",
        "must_contain": ["shared taxi service"],
        "is_tail_fact": True,
    },
    {
        "id": "rag-10",
        "folder": "policy",
        "query": (
            "How many months of passport validity and what endorsement is required for "
            "international business travel?"
        ),
        "expected_doc": "WSO2 Travel Policy - V3.3",
        "must_contain": ["valid for over 6 months", "profession needs to be endorsed"],
        "is_tail_fact": True,
    },
    {
        "id": "rag-11",
        "folder": "policy",
        "query": "Who pays the fare difference if I combine personal vacation travel with an official business trip?",
        "expected_doc": "WSO2 Travel Policy - V3.3",
        "must_contain": ["combining personal travel", "difference must be paid by the employee"],
        "is_tail_fact": True,
    },
    {
        "id": "rag-12",
        "folder": "policy",
        "query": "Which email address should I contact if I fall sick during travel and need to return home urgently?",
        "expected_doc": "WSO2 Travel Policy - V3.3",
        "must_contain": ["travel-escalations-group@"],
        "is_tail_fact": True,
    },
    {
        "id": "rag-13",
        "folder": "policy",
        "query": "Does the unlawful harassment policy cover cyberbullying and messages on digital channels?",
        "expected_doc": "Unlawful Harassment Policy V1.3",
        "must_contain": ["cyberbullying", "digital channels"],
        "is_tail_fact": True,
    },
    {
        "id": "rag-14",
        "folder": "policy",
        "query": (
            "If an employee reports harassment to a colleague or manager instead of People Operations, "
            "what is their duty?"
        ),
        "expected_doc": "Unlawful Harassment Policy V1.3",
        "must_contain": ["immediately inform the people operations team"],
        "is_tail_fact": True,
    },
    {
        "id": "rag-15",
        "folder": "policy",
        "query": "Who decides the final disciplinary action after a harassment committee investigation?",
        "expected_doc": "Unlawful Harassment Policy V1.3",
        "must_contain": ["committee along with the ceo"],
        "is_tail_fact": True,
    },
    {
        "id": "rag-16",
        "folder": "policy",
        "query": "What disciplinary consequences can result from violating the WSO2 Anti-Corruption Policy?",
        "expected_doc": "WSO2 LLC - Anti-Corruption Policy .docx",
        "must_contain": ["demotion", "termination"],
        "is_tail_fact": True,
    },
    {
        "id": "rag-17",
        "folder": "policy",
        "query": "Which Sri Lankan statute governs the prevention and punishment of bribery and corruption?",
        "expected_doc": "WSO2 LLC - Anti-Corruption Policy .docx",
        "must_contain": ["bribery act", "chapter 26"],
        "is_tail_fact": True,
    },
    {
        "id": "rag-18",
        "folder": "policy",
        "query": (
            "Which UK money laundering statute is referenced alongside UKBA offenses in the "
            "Anti-Corruption policy?"
        ),
        "expected_doc": "WSO2 LLC - Anti-Corruption Policy .docx",
        "must_contain": ["proceeds of crime act 2002"],
        "is_tail_fact": False,
    },
    {
        "id": "rag-19",
        "folder": "policy",
        "query": (
            "Which international data privacy regulations (EU, USA, Singapore, South Africa) are "
            "referenced in the Data Protection Policy?"
        ),
        "expected_doc": "WSO2 People Operations Data Protection Policy V1.2",
        "must_contain": ["gdpr", "ccpa", "pdpa", "popia"],
        "is_tail_fact": True,
    },
    {
        "id": "rag-20",
        "folder": "policy",
        "query": "Why might WSO2 request a copy of an employee's passport under the Data Protection Policy?",
        "expected_doc": "WSO2 People Operations Data Protection Policy V1.2",
        "must_contain": ["customer engagements", "passport may also be requested"],
        "is_tail_fact": True,
    },
    {
        "id": "rag-21",
        "folder": "training",
        "query": "Which email should I contact to enable Expert, Advanced, or Solutions Architect certification exams?",
        "expected_doc": "Copy of WSO2 Novera - Training and Certification Assistant",
        "must_contain": ["certification@wso2.com"],
        "is_tail_fact": True,
    },
    {
        "id": "rag-22",
        "folder": "training",
        "query": "How many hours per day are onsite trainings compared to online remote trainings?",
        "expected_doc": "Copy of WSO2 Novera - Training and Certification Assistant",
        "must_contain": ["8 hours", "4 hours"],
        "is_tail_fact": True,
    },
    {
        "id": "rag-23",
        "folder": "training",
        "query": "How many working days does it take to get LMS access for internal-only or sales training courses?",
        "expected_doc": "Copy of WSO2 Novera - Training and Certification Assistant",
        "must_contain": ["two working days"],
        "is_tail_fact": True,
    },
    {
        "id": "rag-24",
        "folder": "training",
        "query": (
            "How does a partner employee get linked to their partner organization before taking "
            "certification exams?"
        ),
        "expected_doc": "Copy of WSO2 Novera - Training and Certification Assistant",
        "must_contain": ["promo code", "partner portal"],
        "is_tail_fact": True,
    },
    {
        "id": "rag-25",
        "folder": "training",
        "query": "Is WSO2 product certification free for WSO2 employees and internals?",
        "expected_doc": "Copy of WSO2 Novera - Training and Certification Assistant",
        "must_contain": ["training is free for all", "free for wso2 internals"],
        "is_tail_fact": True,
    },
]


@dataclass
class RagModelMetrics:
    model_name: str
    dim: int
    corpus_embed_sec: float
    query_p50_ms: float
    query_p95_ms: float
    hit_at_1: float
    hit_at_3: float
    hit_at_5: float
    mrr_at_5: float
    tail_fact_hit_at_3: float
    doc_hit_at_1: float


async def fetch_corpus_from_db() -> Tuple[List[Dict[str, Any]], str, str]:
    conn = await asyncpg.connect(settings.get_database_dsn())
    try:
        rows = await conn.fetch(
            """
            SELECT pc.id, pd.name AS doc_name, pd.drive_folder_id AS folder_id,
                   pc.chunk_index, pc.content
            FROM policy_chunks pc
            JOIN policy_documents pd ON pd.id = pc.document_id
            ORDER BY pc.id;
            """
        )
        chunks = [dict(r) for r in rows]
        return chunks, settings.policy_folder_id, settings.training_folder_id
    finally:
        await conn.close()


def chunk_matches_ground_truth(chunk_text: str, must_contain: List[str]) -> bool:
    lower = chunk_text.lower()
    return all(term.lower() in lower for term in must_contain)


def evaluate_rag_model(
    model_name: str,
    chunks: List[Dict[str, Any]],
    policy_folder_id: str,
    training_folder_id: str,
) -> RagModelMetrics:
    from fastembed import TextEmbedding

    embedder = TextEmbedding(model_name=model_name, local_files_only=True)
    # Warmup
    _ = list(embedder.embed(["warmup passage text"]))

    texts = [c["content"] for c in chunks]
    t0 = time.perf_counter()
    raw_doc_vecs = list(embedder.embed(texts, batch_size=16))
    corpus_embed_sec = time.perf_counter() - t0

    doc_mat = np.vstack(raw_doc_vecs).astype(np.float32)
    doc_mat = doc_mat / (np.linalg.norm(doc_mat, axis=1, keepdims=True) + 1e-12)
    dim = doc_mat.shape[1]

    policy_indices = [
        i for i, c in enumerate(chunks) if c["folder_id"] == policy_folder_id
    ]
    training_indices = [
        i for i, c in enumerate(chunks) if c["folder_id"] == training_folder_id
    ]

    hit1 = 0
    hit3 = 0
    hit5 = 0
    doc_hit1 = 0
    rr_sum = 0.0
    tail_hit3 = 0
    tail_total = 0
    q_latencies: List[float] = []

    for q in RAG_BENCHMARK_QUERIES:
        q_text = q["query"]
        # Nomic v1.5 recommends search_query prefix when querying passages, but we test raw query
        # as Policy MCP sends raw query strings to /embed
        t_q = time.perf_counter()
        q_vec = next(embedder.embed([q_text])).astype(np.float32)
        q_vec = q_vec / (np.linalg.norm(q_vec) + 1e-12)
        q_latencies.append((time.perf_counter() - t_q) * 1000.0)

        target_indices = (
            policy_indices if q["folder"] == "policy" else training_indices
        )
        sub_mat = doc_mat[target_indices]
        sims = sub_mat @ q_vec
        ranked_local = np.argsort(sims)[::-1][:5]
        top5_chunks = [chunks[target_indices[idx]] for idx in ranked_local]

        if top5_chunks and top5_chunks[0]["doc_name"] == q["expected_doc"]:
            doc_hit1 += 1

        matched_ranks = [
            rank + 1
            for rank, c in enumerate(top5_chunks)
            if chunk_matches_ground_truth(c["content"], q["must_contain"])
        ]

        if matched_ranks:
            best_rank = matched_ranks[0]
            rr_sum += 1.0 / best_rank
            if best_rank == 1:
                hit1 += 1
            if best_rank <= 3:
                hit3 += 1
            if best_rank <= 5:
                hit5 += 1

        if q["is_tail_fact"]:
            tail_total += 1
            if matched_ranks and matched_ranks[0] <= 3:
                tail_hit3 += 1

    n_q = len(RAG_BENCHMARK_QUERIES)
    return RagModelMetrics(
        model_name=model_name,
        dim=dim,
        corpus_embed_sec=corpus_embed_sec,
        query_p50_ms=float(np.percentile(q_latencies, 50)),
        query_p95_ms=float(np.percentile(q_latencies, 95)),
        hit_at_1=(hit1 / n_q) * 100.0,
        hit_at_3=(hit3 / n_q) * 100.0,
        hit_at_5=(hit5 / n_q) * 100.0,
        mrr_at_5=rr_sum / n_q,
        tail_fact_hit_at_3=(tail_hit3 / tail_total * 100.0) if tail_total else 0.0,
        doc_hit_at_1=(doc_hit1 / n_q) * 100.0,
    )


def format_rag_report(metrics_list: List[RagModelMetrics], num_chunks: int) -> str:
    lines: List[str] = []
    lines.append(
        f"# Policy MCP Document RAG Embedding Model Benchmark "
        f"(N = 25 Queries over {num_chunks} Real DB Chunks)"
    )
    lines.append("")
    lines.append(
        "| Embedding Model | Dims | Exact Fact Hit@1 | Exact Fact Hit@3 | "
        "Exact Fact Hit@5 (`top_k=5`) | Tail-Fact Hit@3 (>256 tok) | MRR@5 | "
        "Doc Hit@1 | Query p50 | Corpus (175 chunks) |"
    )
    lines.append(
        "| :--- | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: |"
    )
    for m in metrics_list:
        lines.append(
            f"| `{m.model_name}` | {m.dim} | **{m.hit_at_1:.1f}%** | "
            f"**{m.hit_at_3:.1f}%** | **{m.hit_at_5:.1f}%** | "
            f"**{m.tail_fact_hit_at_3:.1f}%** | **{m.mrr_at_5:.3f}** | "
            f"{m.doc_hit_at_1:.1f}% | {m.query_p50_ms:.1f} ms | "
            f"{m.corpus_embed_sec:.2f} s |"
        )
    return "\n".join(lines)


def load_existing_rag_metrics() -> Dict[str, RagModelMetrics]:
    """Parses already-evaluated model rows from policy_rag_model_comparison_report.md."""
    cached: Dict[str, RagModelMetrics] = {}
    if not REPORT_PATH.exists():
        return cached
    for line in REPORT_PATH.read_text(encoding="utf-8").splitlines():
        if not line.startswith("| `"):
            continue
        parts = [p.strip() for p in line.strip("|").split("|")]
        if len(parts) < 10:
            continue
        m_name = parts[0].strip("`")
        cached[m_name] = RagModelMetrics(
            model_name=m_name,
            dim=int(parts[1]),
            hit_at_1=float(parts[2].replace("*", "").replace("%", "")),
            hit_at_3=float(parts[3].replace("*", "").replace("%", "")),
            hit_at_5=float(parts[4].replace("*", "").replace("%", "")),
            tail_fact_hit_at_3=float(parts[5].replace("*", "").replace("%", "")),
            mrr_at_5=float(parts[6].replace("*", "")),
            doc_hit_at_1=float(parts[7].replace("%", "")),
            query_p50_ms=float(parts[8].replace("ms", "").strip()),
            query_p95_ms=float(parts[8].replace("ms", "").strip()) * 1.3,
            corpus_embed_sec=float(parts[9].replace("s", "").strip()),
        )
    return cached


@pytest.mark.asyncio
async def test_policy_mcp_rag_embedding_models():
    """
    Benchmarks 10 embedding models on the 175 real WSO2 HR Policy & Training chunks
    from the database across 25 ground-truth questions (including tail facts >256 tokens).
    """
    chunks, policy_fid, training_fid = await fetch_corpus_from_db()
    assert len(chunks) > 100, "Expected ingested policy_chunks in database"

    existing = load_existing_rag_metrics()
    results: List[RagModelMetrics] = []
    for model_name in RAG_CANDIDATE_MODELS:
        if model_name in existing:
            print(f"[CACHE] Reusing measured RAG metrics for {model_name}", flush=True)
            results.append(existing[model_name])
        else:
            print(f"[RUN] Evaluating {model_name} on 175 DB chunks...", flush=True)
            res = evaluate_rag_model(model_name, chunks, policy_fid, training_fid)
            print(
                f" -> {model_name}: Hit@1={res.hit_at_1:.1f}%, Hit@3={res.hit_at_3:.1f}%, "
                f"MRR={res.mrr_at_5:.3f}, p50={res.query_p50_ms:.1f}ms",
                flush=True,
            )
            results.append(res)

    report = format_rag_report(results, len(chunks))
    REPORT_PATH.write_text(report, encoding="utf-8")
    print("\n\n" + report + "\n")

    best_hit5 = max(r.hit_at_5 for r in results)
    assert best_hit5 >= 85.0
