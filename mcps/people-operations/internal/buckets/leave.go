// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com). All Rights Reserved.
//
// This software is the property of WSO2 LLC. and its suppliers, if any.
// Dissemination of any information or reproduction of any material contained
// herein in any form is strictly forbidden, unless permitted by WSO2 expressly.
// You may not alter or remove any copyright or other notice from copies of this content.

package buckets

import "github.com/wso2-open-operations/people-ops/mcps/people-operations/internal/model"

// GetLeaveBucket returns the LEAVE_STANDARD bucket kit.
func GetLeaveBucket() model.BucketKit {
	return model.BucketKit{
		Bucket:      "LEAVE_STANDARD",
		Domain:      "People Operations",
		Description: "Standard short-term leave management (casual, sick, annual).",
		Version:     "1.0.0",
		TriggerPhrases: []string{
			"leave", "leaves", "time off", "day off", "vacation", "trip", "holiday",
			"casual leave", "sick leave", "annual leave",
			"maternity leave", "paternity leave", "lieu leave",
			"book leave", "apply leave", "take leave", "request leave",
			"pending", "pending leaves", "pending leave",
			"balance", "leave balance", "my balance",
			"history", "leave history", "my leaves",
			"out of office", "ooo", "who is out",
			"cancel leave", "comment",
		},
		SuggestedPrompts: []string{
			"Check my leave balance",
			"I want to apply for annual leave tomorrow",
			"Show my pending leaves",
			"Check who is out of office this Friday",
		},
		PromptFragment: `You are the WSO2 Standard Leave assistant. Follow these strict guidelines:
1. You can only perform actions for the currently authenticated user. If the user mentions another
   person's email address in the context of any tool, reject the request immediately and explain you
   can only act on their own behalf.
2. For leave types, map friendly names: "casual" -> casual, "sick" -> sick, "annual" -> annual,
   "lieu" -> lieu, "maternity" -> maternity, "paternity" -> paternity.
3. For period type: single day -> one, multiple days -> multiple, half day -> half. If the user
   requests a half-day leave, ask whether it is for the morning (is_morning_leave: true) or
   afternoon (is_morning_leave: false).
4. When collecting leave details, ask if the user has a comment to add. If yes, ask if it should be
   public (visible to the vacation group) or private. If no comment, skip the visibility question.
5. MANDATORY: As soon as leave details (start date, end date, leave type, period type) are collected,
   you MUST invoke the 'validate_leave' tool immediately BEFORE asking for confirmation or displaying the summary.
   Do NOT invoke 'validate_leave' again if the details have already been validated in this conversation.
- If workingDays is 0, inform the user that the selected dates contain no working days (e.g. falls entirely
  on weekends or public holidays).
- If hasOverlap is true or an error occurs, inform the user of the conflict or error.
- If validation succeeds, report the server-verified working days (e.g. "Your leave was verified:
  [workingDays] working day(s) will be deducted.").
6. Once 'validate_leave' succeeds, present a clear summary of the verified leave request (dates, leave type,
   period type, and verified working days) and invoke 'submit_leave'. The system confirmation guardrail will
   prompt the user for confirmation before executing the submission.
7. To cancel a leave: call 'get_pending_leaves' to fetch cancellable leaves, show the list to the user with
   dates and leave type (do NOT show numeric database IDs). When the user identifies which leave to cancel,
   invoke 'cancel_leave'. The system confirmation guardrail will prompt the user for confirmation before executing.
8. When showing leave history: call 'get_leave_history'. Display the results in a clean point-wise list
   (NEVER use markdown tables). NEVER use asterisks (* or **) for bolding or italics. For each leave record,
   format cleanly as: "• <Leave Type> — <Start Date> to <End Date> (<Duration> day(s), <Period Type>) - <Status>".
   If a comment is present, include it indented below. Do NOT show numeric database IDs.
9. If the user says "cancel" or "no", abandon the current action and confirm it was cancelled.
10. If the user wants to book manually or encounters an error, provide this link:
    [One WSO2 Leave](https://one.wso2.com/me/leave)
11. For read-only queries (viewing leave history, balances, or pending leaves), call the relevant tool
    immediately without asking for confirmation first.
12. When the user asks for their leave balance, remaining leave, or entitlement (e.g. 'How many casual leaves
    do I have left?'), call 'get_leave_balance' immediately without asking for confirmation.
13. If the user only says "leave" or gives an open-ended request without specifying an action, respond warmly
    and present available actions point-wise with quick examples (e.g. "• Check Balance — e.g., 'How many
    annual leaves do I have left?'", "• Apply for Leave — e.g., 'Take casual leave this Friday'",
    "• View or Cancel — e.g., 'Show my pending leaves'"). Do NOT use sterile numbered menus or asterisks (* or **).
Be concise and friendly. Use plain text only (no markdown asterisks).`,
		Tools: []model.Tool{
			{
				Name: "validate_leave",
				Description: "Pre-flight dry-run to validate a leave request before submission. " +
					"Validates working days, weekends, public holidays, and existing leave overlaps without submitting. " +
					"Call immediately once dates, leave type, and period type are known, before asking for confirmation.",
				InputSchema: model.InputSchema{
					Type: "object",
					Properties: map[string]model.ToolProperty{
						"start_date": {Type: "string", Description: "YYYY-MM-DD"},
						"end_date":   {Type: "string", Description: "YYYY-MM-DD"},
						"leave_type": {
							Type: "string",
							Enum: []string{"casual", "sick", "annual", "lieu", "maternity", "paternity"},
						},
						"period_type": {
							Type:        "string",
							Enum:        []string{"one", "multiple", "half"},
							Description: "one for single full day, multiple for multiple days, half for half day",
						},
						"is_morning_leave": {
							Type:        "boolean",
							Description: "For half day only: true = morning, false = afternoon",
						},
						"comment": {
							Type:        "string",
							Description: "Optional comment to include with notification",
						},
						"is_public_comment": {
							Type:        "boolean",
							Description: "Whether comment is visible to vacation group (default false)",
						},
					},
					Required: []string{"start_date", "end_date", "leave_type", "period_type"},
				},
				ReadOnly:             true,
				RequiresConfirmation: false,
			},
			{
				Name: "check_calendar",
				Description: "Optional read-only tool to inspect company calendar public holidays " +
					"and existing leaves for a date range.",
				InputSchema: model.InputSchema{
					Type: "object",
					Properties: map[string]model.ToolProperty{
						"start_date": {Type: "string", Description: "YYYY-MM-DD"},
						"end_date":   {Type: "string", Description: "YYYY-MM-DD"},
					},
					Required: []string{"start_date", "end_date"},
				},
				ReadOnly:             true,
				RequiresConfirmation: false,
			},
			{
				Name: "submit_leave",
				Description: "Submit a leave request for the employee. " +
					"Requires user confirmation before execution.",
				InputSchema: model.InputSchema{
					Type: "object",
					Properties: map[string]model.ToolProperty{
						"start_date": {Type: "string", Description: "YYYY-MM-DD"},
						"end_date":   {Type: "string", Description: "YYYY-MM-DD"},
						"leave_type": {
							Type: "string",
							Enum: []string{"casual", "sick", "annual", "lieu", "maternity", "paternity"},
						},
						"period_type": {
							Type:        "string",
							Enum:        []string{"one", "multiple", "half"},
							Description: "one for single full day, multiple for multiple days, half for half day",
						},
						"is_morning_leave": {
							Type:        "boolean",
							Description: "For half day only: true = morning, false = afternoon",
						},
						"comment": {
							Type:        "string",
							Description: "Optional comment to include with notification",
						},
						"is_public_comment": {
							Type:        "boolean",
							Description: "Whether comment is visible to vacation group (default false)",
						},
					},
					Required: []string{"start_date", "end_date", "leave_type", "period_type"},
				},
				ReadOnly:             false,
				RequiresConfirmation: true,
			},
			{
				Name: "get_leave_history",
				Description: "Read-only. Fetch the employee's leave history (pending, approved, cancelled), " +
					"most recent first. Returns up to 20 records. Call immediately when the user asks to see their " +
					"leave history — do not ask for confirmation first.",
				InputSchema: model.InputSchema{
					Type: "object",
					Properties: map[string]model.ToolProperty{
						"start_date": {Type: "string", Description: "Optional start date filter in YYYY-MM-DD format"},
						"end_date":   {Type: "string", Description: "Optional end date filter in YYYY-MM-DD format"},
					},
					Required: []string{},
				},
				ReadOnly:             true,
				RequiresConfirmation: false,
			},
			{
				Name: "get_pending_leaves",
				Description: "Read-only. Fetch the employee's current pending and approved leaves eligible for cancellation. " +
					"Call immediately as part of the cancel flow — do not ask for confirmation before calling this.",
				InputSchema: model.InputSchema{
					Type:       "object",
					Properties: map[string]model.ToolProperty{},
					Required:   []string{},
				},
				ReadOnly:             true,
				RequiresConfirmation: false,
			},
			{
				Name: "cancel_leave",
				Description: "Cancel a leave request by its numeric ID. " +
					"Requires user confirmation before execution.",
				InputSchema: model.InputSchema{
					Type: "object",
					Properties: map[string]model.ToolProperty{
						"leave_id": {Type: "number", Description: "ID of the leave to cancel"},
					},
					Required: []string{"leave_id"},
				},
				ReadOnly:             false,
				RequiresConfirmation: true,
			},
			{
				Name: "get_leave_balance",
				Description: "Fetch the employee's current leave balance, annual entitlement, and used days. " +
					"Read-only — call immediately without confirmation.",
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
