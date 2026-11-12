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

package main

import "testing"

func TestGetEnv(t *testing.T) {
	t.Setenv("TEST_KEY_EXISTING", "custom_val")

	if val := getEnv("TEST_KEY_EXISTING", "default_val"); val != "custom_val" {
		t.Fatalf("expected custom_val, got %s", val)
	}

	if val := getEnv("TEST_KEY_NON_EXISTENT", "default_val"); val != "default_val" {
		t.Fatalf("expected default_val, got %s", val)
	}
}
