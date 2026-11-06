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

package buckets

import "github.com/wso2-open-operations/people-ops/mcps/people-operations/internal/model"

// GetSubscriptionsBucket returns the PO_SUBSCRIPTIONS bucket kit.
func GetSubscriptionsBucket() model.BucketKit {
	return model.BucketKit{
		Bucket:      "PO_SUBSCRIPTIONS",
		Domain:      "People Operations",
		Description: "Monthly Lunch as a Service (LaaS) and PickMe Commute subscriptions and status inquiries.",
		Version:     "1.0.0",
		TriggerPhrases: []string{
			"subscription", "subscriptions", "lunch subscription", "subscribe to lunch",
			"cancel lunch subscription", "unsubscribe from lunch", "laas", "lunch service",
			"am i subscribed to lunch", "check lunch subscription", "commute", "pickme",
			"transport subscription", "commute subscription", "subscribe to commute",
			"unsubscribe from commute", "am i subscribed to commute", "check commute subscription",
			"pickme commute", "pick me", "pick me commute", "transport",
			"opt in", "opt out", "my subscriptions",
		},
		PromptFragment: `You are the WSO2 Subscriptions assistant (managing both Lunch as a Service / LaaS
and PickMe Commute). Follow these strict guidelines:
1. When the user asks about lunch subscription status, invoke 'get_lunch_subscription' immediately
   without asking for confirmation.
2. When the user clearly asks to subscribe to lunch, invoke 'subscribe_lunch'.
   The system confirmation guardrail will prompt the user for confirmation before executing.
3. When the user clearly asks to cancel or unsubscribe from lunch, invoke 'unsubscribe_lunch'.
   The system confirmation guardrail will prompt the user for confirmation before executing.
4. When the user asks about commute subscription status, invoke 'get_commute_subscription' immediately
   without asking for confirmation.
5. When the user clearly asks to subscribe to commute, collect distance in km and mobile number
   (+947XXXXXXXX), then invoke 'subscribe_commute'.
6. When the user clearly asks to unsubscribe from commute, invoke 'unsubscribe_commute'.
7. Note: Subscribing and unsubscribing are restricted to the monthly opt-in / opt-out window.
   If the backend reports the window is closed, relay the exact message to the user.
8. If the user asks for the portal link, wants to manage subscriptions on the web, or encounters
   an error or window restriction, provide this link: [One WSO2 Subscriptions](https://one.wso2.com/me/subscriptions)
9. When presenting web links, format them in markdown format: [Link Text](<url>) so the UI can render
   them as interactive clickable links.
Be concise and friendly.`,
		Tools: []model.Tool{
			{
				Name: "get_lunch_subscription",
				Description: "Check current Lunch as a Service (LaaS) subscription status for the authenticated user. " +
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
				Name: "subscribe_lunch",
				Description: "Subscribe the authenticated employee to monthly Lunch as a Service (LaaS). " +
					"Accepted only during the monthly opt-in window. Requires user confirmation before execution.",
				InputSchema: model.InputSchema{
					Type:       "object",
					Properties: map[string]model.ToolProperty{},
					Required:   []string{},
				},
				ReadOnly:             false,
				RequiresConfirmation: true,
			},
			{
				Name: "unsubscribe_lunch",
				Description: "Unsubscribe the authenticated employee from monthly Lunch as a Service (LaaS). " +
					"Accepted only during the monthly opt-out window. Requires user confirmation before execution.",
				InputSchema: model.InputSchema{
					Type:       "object",
					Properties: map[string]model.ToolProperty{},
					Required:   []string{},
				},
				ReadOnly:             false,
				RequiresConfirmation: true,
			},
			{
				Name: "get_commute_subscription",
				Description: "Check current Commute (PickMe) as a Service subscription status for the authenticated user. " +
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
				Name: "subscribe_commute",
				Description: "Subscribe the authenticated employee to Commute (PickMe) as a Service for a given " +
					"one-way distance in km and contact number (+947XXXXXXXX). Accepted only during the monthly opt-in window.",
				InputSchema: model.InputSchema{
					Type: "object",
					Properties: map[string]model.ToolProperty{
						"distanceKm": {
							Type:        "number",
							Description: "The employee's one-way commute distance from office, in kilometers",
						},
						"contactNumber": {
							Type:        "string",
							Description: "Sri Lankan mobile number in format +947XXXXXXXX (e.g. +94771234567)",
						},
					},
					Required: []string{"distanceKm", "contactNumber"},
				},
				ReadOnly:             false,
				RequiresConfirmation: true,
			},
			{
				Name: "unsubscribe_commute",
				Description: "Unsubscribe the authenticated employee from Commute (PickMe) as a Service. " +
					"Accepted only during the monthly opt-out window.",
				InputSchema: model.InputSchema{
					Type:       "object",
					Properties: map[string]model.ToolProperty{},
					Required:   []string{},
				},
				ReadOnly:             false,
				RequiresConfirmation: true,
			},
		},
	}
}
