// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com). All Rights Reserved.
//
// This software is the property of WSO2 LLC. and its suppliers, if any.
// Dissemination of any information or reproduction of any material contained
// herein in any form is strictly forbidden, unless permitted by WSO2 expressly.
// You may not alter or remove any copyright or other notice from copies of this content.

package buckets

import "github.com/wso2-open-operations/people-ops/mcps/people-operations/internal/model"

// GetVehicleBucket returns the PO_VEHICLES bucket kit.
func GetVehicleBucket() model.BucketKit {
	return model.BucketKit{
		Bucket:      "PO_VEHICLES",
		Domain:      "People Operations",
		Description: "Employee vehicle registration, lookup, and deletion for campus parking.",
		Version:     "1.0.0",
		TriggerPhrases: []string{
			"vehicle", "vehicles", "register vehicle", "add vehicle", "delete vehicle",
			"remove vehicle", "my car", "my bike", "motorcycle", "parking vehicle", "license plate",
			"parking", "car", "cars", "bike", "bikes", "motorbike",
			"number plate", "registered vehicles", "my vehicles",
			"register my car", "register my bike", "parking permit",
		},
		PromptFragment: `You are the WSO2 Vehicle Management assistant. Follow these strict guidelines:
1. You can only perform actions for the currently authenticated user. Reject requests for other employees.
2. Read-only queries (listing registered vehicles): call 'get_vehicles' immediately without asking for confirmation.
   By default, pass vehicleStatus='ACTIVE' for general listing. Display the registration number and type; do NOT
   display internal database vehicle IDs. Only include deleted/inactive vehicles if the user explicitly asks.
3. Adding a vehicle: collect the registration number and vehicle type (CAR or MOTORCYCLE). Map friendly names:
   "car" -> CAR, "motorcycle"/"bike"/"motorbike" -> MOTORCYCLE. Plate must follow Sri Lankan format (1-3 letters
   or digits, a single space, then 4 digits, e.g. "ABC 1234" or "WP 1234"). Present a clear summary and invoke
   'add_vehicle' directly.
4. Deleting a vehicle: first call 'get_vehicles' with vehicleStatus='ACTIVE' to show active vehicles with registration
   number and type (do NOT show numeric vehicle IDs). When the user specifies which vehicle to delete, invoke
   'delete_vehicle' directly with its numeric vehicleId.
5. If the user says "cancel" or "no", abandon the action and confirm it was cancelled.
6. If the user asks for the portal link or wants to manage vehicles on the web portal, provide this link:
   [One WSO2 Profile](https://one.wso2.com/me)
7. When presenting web links, format them in markdown format: [Link Text](<url>) so the UI can render
   them as interactive clickable links.
Be concise and friendly.`,
		Tools: []model.Tool{
			{
				Name: "get_vehicles",
				Description: "Fetch the employee's registered vehicles. Read-only — call immediately without " +
					"asking for confirmation. Default behaviour is ACTIVE-only — pass 'ACTIVE' for any normal listing query. " +
					"Omit only when the user explicitly asks for deleted/all vehicles.",
				InputSchema: model.InputSchema{
					Type: "object",
					Properties: map[string]model.ToolProperty{
						"vehicleStatus": {
							Type:        "string",
							Enum:        []string{"ACTIVE", "INACTIVE"},
							Description: "Filter by status: ACTIVE or INACTIVE",
						},
						"vehicleType": {
							Type:        "string",
							Enum:        []string{"CAR", "MOTORCYCLE"},
							Description: "Filter by type: CAR or MOTORCYCLE",
						},
					},
					Required: []string{},
				},
				ReadOnly:             true,
				RequiresConfirmation: false,
			},
			{
				Name: "add_vehicle",
				Description: "Register a new vehicle for the employee. Only call after the user has explicitly confirmed " +
					"the registration number and vehicle type. Vehicle registration plate must be valid Sri Lankan format " +
					"(e.g. 'ABC 1234' or 'WP 1234').",
				InputSchema: model.InputSchema{
					Type: "object",
					Properties: map[string]model.ToolProperty{
						"vehicleRegistrationNumber": {
							Type:        "string",
							Description: "Sri Lankan vehicle plate (e.g. 'ABC 1234' or 'WP 1234')",
						},
						"vehicleType": {
							Type:        "string",
							Enum:        []string{"CAR", "MOTORCYCLE"},
							Description: "CAR or MOTORCYCLE",
						},
					},
					Required: []string{"vehicleRegistrationNumber", "vehicleType"},
				},
				ReadOnly:             false,
				RequiresConfirmation: true,
			},
			{
				Name: "delete_vehicle",
				Description: "Delete a registered vehicle by its numeric ID. Only call after the user has explicitly " +
					"confirmed which vehicle to delete.",
				InputSchema: model.InputSchema{
					Type: "object",
					Properties: map[string]model.ToolProperty{
						"vehicleId": {
							Type:        "number",
							Description: "The numeric ID of the vehicle to delete",
						},
					},
					Required: []string{"vehicleId"},
				},
				ReadOnly:             false,
				RequiresConfirmation: true,
			},
		},
	}
}
