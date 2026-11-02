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

// GetLettersBucket returns the PO_LETTERS bucket kit.
func GetLettersBucket() model.BucketKit {
	return model.BucketKit{
		Bucket:      "PO_LETTERS",
		Domain:      "People Operations",
		Description: "Official People Operations letters and confirmation request documentation.",
		Version:     "1.0.0",
		TriggerPhrases: []string{
			"letter", "letters", "request letter", "visa letter", "service letter",
			"salary letter", "employment confirmation", "passport renewal letter",
			"letter request form", "embassy letter",
			"internship letter", "epf letter", "etf letter", "pfca letter",
			"bank letter", "loan letter", "proof of employment", "employment letter",
			"official letter",
		},
		PromptFragment: `You are the WSO2 People Operations Documentation assistant. Follow these strict guidelines:
1. When the user asks for any official letter (employment confirmation, visa letter, salary letter, passport renewal,
   service letter, internship letter, EPF/ETF name mismatch letter, PFCA account opening letter, etc.) or asks how
   to request a letter from People Operations, invoke the 'get_letter_request_form' tool immediately without asking
   for confirmation.
2. Present the returned form URL together with the full list of supported letter types so the user can verify their
   need is covered. Do not pre-judge or filter which letter type they require.
3. When presenting the form URL or any web link, format it in markdown format:
   [People Operations Letter Request Form](<form_url>)
   so the frontend UI can highlight it as an interactive, underlined clickable link.
Be concise and friendly.`,
		Tools: []model.Tool{
			{
				Name: "get_letter_request_form",
				Description: "Retrieve the official PO letter request form URL and list of supported letter types. " +
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
