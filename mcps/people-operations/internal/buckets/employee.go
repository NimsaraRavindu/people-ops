// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com). All Rights Reserved.
//
// This software is the property of WSO2 LLC. and its suppliers, if any.
// Dissemination of any information or reproduction of any material contained
// herein in any form is strictly forbidden, unless permitted by WSO2 expressly.
// You may not alter or remove any copyright or other notice from copies of this content.

package buckets

import "github.com/wso2-open-operations/people-ops/mcps/people-operations/internal/model"

// GetEmployeeBucket returns the PO_EMPLOYEE bucket kit.
func GetEmployeeBucket() model.BucketKit {
	return model.BucketKit{
		Bucket:      "PO_EMPLOYEE",
		Domain:      "People Operations",
		Description: "Employee workplace location, desk assignment, and reporting lead inquiries.",
		Version:     "1.0.0",
		TriggerPhrases: []string{
			"my location", "where am i located", "which office", "work location",
			"my office location", "my workplace", "what is my location",
			"office location", "workplace location", "assigned office", "home office",
			"my desk", "desk location", "my seat", "assigned seat", "seat", "desk",
			"my lead", "who is my lead", "my manager", "who is my manager", "reporting lead",
		},
		PromptFragment: `You are the WSO2 Employee Profile assistant. Follow these strict guidelines:
1. You can only perform inquiries for the currently authenticated user.
2. When the user asks where they are located, which office they belong to, or their workplace location,
   call 'get_my_location' immediately without asking for confirmation.
3. Report their work location (e.g. Sri Lanka, United States, United Kingdom) clearly and concisely.
4. When the user asks who their lead, manager, or supervisor is, call 'fetch_leads' immediately
   without asking for confirmation and report their lead's name and email.
Be concise and friendly. Use plain text only.`,
		Tools: []model.Tool{
			{
				Name: "get_my_location",
				Description: "Look up the authenticated employee's primary office / work location. " +
					"Read-only — call immediately without confirmation.",
				InputSchema: model.InputSchema{
					Type:       "object",
					Properties: map[string]model.ToolProperty{},
					Required:   []string{},
				},
				ReadOnly:             true,
				RequiresConfirmation: false,
			},
			{
				Name:        "fetch_leads",
				Description: "Fetch organizational lead and reporting hierarchy details for the employee. Read-only.",
				InputSchema: model.InputSchema{
					Type:       "object",
					Properties: map[string]model.ToolProperty{},
					Required:   []string{},
				},
				ReadOnly:             true,
				RequiresConfirmation: false,
			},
		},
	}
}
