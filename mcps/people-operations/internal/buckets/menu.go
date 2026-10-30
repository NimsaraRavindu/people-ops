// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com). All Rights Reserved.
//
// This software is the property of WSO2 LLC. and its suppliers, if any.
// Dissemination of any information or reproduction of any material contained
// herein in any form is strictly forbidden, unless permitted by WSO2 expressly.
// You may not alter or remove any copyright or other notice from copies of this content.

package buckets

import "github.com/wso2-open-operations/people-ops/mcps/people-operations/internal/model"

// GetMenuBucket returns the PO_MENU bucket kit.
func GetMenuBucket() model.BucketKit {
	return model.BucketKit{
		Bucket:      "PO_MENU",
		Domain:      "People Operations",
		Description: "Daily cafeteria menu inquiries and lunch feedback submissions.",
		Version:     "1.0.0",
		TriggerPhrases: []string{
			"menu", "cafeteria", "lunch", "what is for lunch", "today's menu",
			"breakfast", "juice", "dessert", "food", "lunch feedback", "meal review",
			"meals", "meal", "snacks", "snack", "canteen",
		},
		PromptFragment: `You are the WSO2 Cafeteria & Menu assistant. Follow these strict guidelines:
1. When the user asks what is for lunch, breakfast, juice, dessert, snacks, or today's cafeteria menu,
   invoke 'get_menu' immediately without asking for confirmation.
2. When the user shares feedback, reviews, or comments about today's lunch, present a brief summary and invoke
   'submit_lunch_feedback' directly. Note that lunch feedback is only accepted during the lunch window (12:00 to 16:15
   Sri Lanka time).
3. If the user says "cancel" or "no", abandon the action and confirm it was cancelled.
4. If the user asks for the cafeteria portal link, or wants to view menus or place dinner orders on the web,
   provide this link: [One WSO2 Cafeteria](https://one.wso2.com/me/menu)
5. When presenting web links, format them in markdown format: [Link Text](<url>) so the UI can render
   them as interactive clickable links.
Be concise and friendly.`,
		Tools: []model.Tool{
			{
				Name: "get_menu",
				Description: "Fetch today's cafeteria menu for WSO2 offices (breakfast, lunch, juice, dessert, snack). " +
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
				Name: "submit_lunch_feedback",
				Description: "Submit feedback or review for today's lunch meal. " +
					"Submissions are accepted only during the lunch window (12:00–16:15 Sri Lanka time). " +
					"Only call after the user has explicitly confirmed their feedback message.",
				InputSchema: model.InputSchema{
					Type: "object",
					Properties: map[string]model.ToolProperty{
						"message": {
							Type:        "string",
							Description: "The feedback message or review about today's lunch",
						},
					},
					Required: []string{"message"},
				},
				ReadOnly:             false,
				RequiresConfirmation: true,
			},
		},
	}
}
