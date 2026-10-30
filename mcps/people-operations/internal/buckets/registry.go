// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com). All Rights Reserved.
//
// This software is the property of WSO2 LLC. and its suppliers, if any.
// Dissemination of any information or reproduction of any material contained
// herein in any form is strictly forbidden, unless permitted by WSO2 expressly.
// You may not alter or remove any copyright or other notice from copies of this content.

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
