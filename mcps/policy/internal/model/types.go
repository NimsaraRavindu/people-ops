// Copyright (c) 2026 WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package model

// ToolProperty defines a single property in a tool's JSON input schema.
type ToolProperty struct {
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Enum        []string `json:"enum,omitempty"`
}

// InputSchema defines the JSON schema for tool invocation arguments.
type InputSchema struct {
	Type       string                  `json:"type"`
	Properties map[string]ToolProperty `json:"properties"`
	Required   []string                `json:"required"`
}

// Tool defines an executable MCP tool exposed to Agent Core.
type Tool struct {
	Name                 string      `json:"name"`
	Description          string      `json:"description,omitempty"`
	InputSchema          InputSchema `json:"input_schema"`
	ReadOnly             bool        `json:"read_only"`
	RequiresConfirmation bool        `json:"requires_confirmation"`
}

// BucketKit encapsulates a domain capability bundle registered with Agent Core.
type BucketKit struct {
	Bucket           string   `json:"bucket"`
	Domain           string   `json:"domain"`
	Description      string   `json:"description"`
	Version          string   `json:"version"`
	TriggerPhrases   []string `json:"trigger_phrases"`
	SuggestedPrompts []string `json:"suggested_prompts,omitempty"`
	PromptFragment   string   `json:"prompt_fragment"`
	Tools            []Tool   `json:"tools"`
}

// SearchResultItem represents an excerpt retrieved via vector similarity search.
type SearchResultItem struct {
	Document   string  `json:"document"`
	Excerpt    string  `json:"excerpt"`
	Similarity float64 `json:"similarity"`
}

// ToolResponse represents the standard output returned by policy search tools.
type ToolResponse struct {
	Query       string             `json:"query"`
	Results     []SearchResultItem `json:"results"`
	SourceCount int                `json:"source_count"`
}
