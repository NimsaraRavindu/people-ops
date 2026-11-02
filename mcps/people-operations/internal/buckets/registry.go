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

// GetAllBuckets returns all active BucketKit definitions exposed by the People Operations MCP.
func GetAllBuckets() []model.BucketKit {
	return []model.BucketKit{
		GetLeaveBucket(),
		GetVehicleBucket(),
		GetEmployeeBucket(),
		GetLettersBucket(),
		GetMenuBucket(),
		GetSubscriptionsBucket(),
	}
}

// GetBucket looks up a bucket by its identifier.
func GetBucket(bucketName string) (model.BucketKit, bool) {
	for _, b := range GetAllBuckets() {
		if b.Bucket == bucketName {
			return b, true
		}
	}
	return model.BucketKit{}, false
}

// GetTool looks up a tool definition across all registered buckets.
func GetTool(toolName string) (model.Tool, bool) {
	for _, b := range GetAllBuckets() {
		for _, t := range b.Tools {
			if t.Name == toolName {
				return t, true
			}
		}
	}
	return model.Tool{}, false
}
