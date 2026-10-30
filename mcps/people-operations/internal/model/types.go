// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com). All Rights Reserved.
//
// This software is the property of WSO2 LLC. and its suppliers, if any.
// Dissemination of any information or reproduction of any material contained
// herein in any form is strictly forbidden, unless permitted by WSO2 expressly.
// You may not alter or remove any copyright or other notice from copies of this content.

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

// CommuteDistanceRange represents distance tier metadata for PickMe Commute subscriptions.
type CommuteDistanceRange struct {
	ID          int `json:"id"`
	MinKm       int `json:"minKm"`
	MaxKm       int `json:"maxKm"`
	MonthlyRate int `json:"monthlyRate"`
}

// SubscriptionsMetaInfo represents meta-info response from the subscription backend.
type SubscriptionsMetaInfo struct {
	Distances []CommuteDistanceRange `json:"distances"`
}
