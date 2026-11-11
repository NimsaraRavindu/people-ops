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

import "testing"

func TestGetAllBuckets(t *testing.T) {
	buckets := GetAllBuckets()
	if len(buckets) != 6 {
		t.Fatalf("expected 6 buckets, got %d", len(buckets))
	}

	expectedBuckets := []string{
		"LEAVE_STANDARD",
		"PO_VEHICLES",
		"PO_EMPLOYEE",
		"PO_LETTERS",
		"PO_MENU",
		"PO_SUBSCRIPTIONS",
	}

	for _, name := range expectedBuckets {
		b, found := GetBucket(name)
		if !found {
			t.Errorf("bucket %s was not found", name)
		}
		if b.Domain != "People Operations" {
			t.Errorf("expected domain 'People Operations' for %s, got %s", name, b.Domain)
		}
		if len(b.Tools) == 0 {
			t.Errorf("bucket %s has no registered tools", name)
		}
	}
}

func TestGetTool(t *testing.T) {
	expectedTools := []struct {
		name                 string
		readOnly             bool
		requiresConfirmation bool
	}{
		{"validate_leave", true, false},
		{"submit_leave", false, true},
		{"get_vehicles", true, false},
		{"add_vehicle", false, true},
		{"get_my_location", true, false},
		{"get_letter_request_form", true, false},
		{"get_menu", true, false},
		{"submit_lunch_feedback", false, true},
		{"subscribe_commute", false, true},
		{"subscribe_lunch", false, true},
		{"unsubscribe_lunch", false, true},
	}

	for _, tt := range expectedTools {
		tool, found := GetTool(tt.name)
		if !found {
			t.Errorf("tool %s was not found", tt.name)
			continue
		}
		if tool.ReadOnly != tt.readOnly {
			t.Errorf("tool %s read_only expected %v, got %v", tt.name, tt.readOnly, tool.ReadOnly)
		}
		if tool.RequiresConfirmation != tt.requiresConfirmation {
			t.Errorf("tool %s requires_confirmation expected %v, got %v",
				tt.name, tt.requiresConfirmation, tool.RequiresConfirmation)
		}
	}

	_, found := GetTool("hallucinated_unknown_tool")
	if found {
		t.Errorf("expected hallucinated_unknown_tool to not be found")
	}
}
