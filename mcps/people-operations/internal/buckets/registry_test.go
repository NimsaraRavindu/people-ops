// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com). All Rights Reserved.
//
// This software is the property of WSO2 LLC. and its suppliers, if any.
// Dissemination of any information or reproduction of any material contained
// herein in any form is strictly forbidden, unless permitted by WSO2 expressly.
// You may not alter or remove any copyright or other notice from copies of this content.

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
