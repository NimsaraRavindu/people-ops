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

"""Pydantic data models for the Policy MCP Server contract."""

from typing import Dict, List
from pydantic import BaseModel, Field


class ToolProperty(BaseModel):
    """Represents a JSON Schema property inside an MCP tool input_schema."""

    type: str
    description: str


class InputSchema(BaseModel):
    """Represents the JSON Schema object for tool arguments."""

    type: str = "object"
    properties: Dict[str, ToolProperty] = Field(default_factory=dict)
    required: List[str] = Field(default_factory=list)


class Tool(BaseModel):
    """Represents an individual MCP tool definition."""

    name: str
    description: str
    input_schema: InputSchema
    read_only: bool = True
    requires_confirmation: bool = False


class BucketKit(BaseModel):
    """Represents a capability bucket registered with Agent Core."""

    bucket: str
    domain: str
    description: str
    version: str
    trigger_phrases: List[str]
    prompt_fragment: str
    tools: List[Tool]


class SearchResultItem(BaseModel):
    """Represents a single policy or training excerpt returned by vector search."""

    document: str
    excerpt: str
    similarity: float


class ToolResponse(BaseModel):
    """Represents the response payload returned from /tools/{tool_name}."""

    query: str
    results: List[SearchResultItem] = Field(default_factory=list)
    source_count: int = 0
