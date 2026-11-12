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

import (
	"strings"
	"testing"
)

func TestGetPolicyBucket(t *testing.T) {
	b := GetPolicyBucket()
	if b.Bucket != "PO_POLICIES" {
		t.Fatalf("expected bucket PO_POLICIES, got %s", b.Bucket)
	}
	if b.Domain != "People Operations" {
		t.Fatalf("expected domain 'People Operations', got %s", b.Domain)
	}
	if len(b.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(b.Tools))
	}
	if !strings.Contains(b.PromptFragment, "Security Boundary") {
		t.Fatalf("expected PromptFragment to contain 'Security Boundary' directive")
	}
}

func TestGetTool(t *testing.T) {
	tool, found := GetTool("query_policy")
	if !found {
		t.Fatalf("expected query_policy tool to be found")
	}
	if !tool.ReadOnly {
		t.Fatalf("expected query_policy to be read_only=true")
	}
	if tool.RequiresConfirmation {
		t.Fatalf("expected query_policy requires_confirmation=false")
	}

	_, found = GetTool("non_existent_tool")
	if found {
		t.Fatalf("did not expect non_existent_tool to be found")
	}
}
