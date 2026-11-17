# Policy MCP Document RAG Embedding Model Benchmark (N = 25 Queries over 175 Real DB Chunks)

| Embedding Model | Dims | Exact Fact Hit@1 | Exact Fact Hit@3 | Exact Fact Hit@5 (`top_k=5`) | Tail-Fact Hit@3 (>256 tok) | MRR@5 | Doc Hit@1 | Query p50 | Corpus (175 chunks) |
| :--- | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: | :---: |
| `BAAI/bge-base-en-v1.5` | 768 | **88.0%** | **96.0%** | **96.0%** | **95.2%** | **0.920** | 100.0% | 44.9 ms | 117.58 s |
| `BAAI/bge-small-en-v1.5` | 384 | **80.0%** | **96.0%** | **100.0%** | **95.2%** | **0.890** | 100.0% | 18.3 ms | 25.60 s |
| `sentence-transformers/all-MiniLM-L6-v2` | 384 | **76.0%** | **84.0%** | **88.0%** | **81.0%** | **0.810** | 100.0% | 2.8 ms | 5.76 s |
| `snowflake/snowflake-arctic-embed-xs` | 384 | **68.0%** | **88.0%** | **92.0%** | **85.7%** | **0.781** | 100.0% | 3.6 ms | 16.76 s |
| `snowflake/snowflake-arctic-embed-s` | 384 | **72.0%** | **96.0%** | **100.0%** | **95.2%** | **0.835** | 96.0% | 9.8 ms | 41.61 s |
| `snowflake/snowflake-arctic-embed-m` | 768 | **72.0%** | **92.0%** | **96.0%** | **90.5%** | **0.821** | 96.0% | 18.1 ms | 78.80 s |
| `nomic-ai/nomic-embed-text-v1.5-Q` | 768 | **72.0%** | **96.0%** | **96.0%** | **95.2%** | **0.833** | 100.0% | 16.2 ms | 70.20 s |
| `jinaai/jina-embeddings-v2-small-en` | 512 | **84.0%** | **96.0%** | **96.0%** | **95.2%** | **0.893** | 100.0% | 5.3 ms | 15.52 s |
| `jinaai/jina-embeddings-v2-base-en` | 768 | **80.0%** | **100.0%** | **100.0%** | **100.0%** | **0.893** | 100.0% | 45.0 ms | 103.59 s |
| `thenlper/gte-base` | 768 | **84.0%** | **96.0%** | **96.0%** | **95.2%** | **0.893** | 100.0% | 19.9 ms | 81.57 s |