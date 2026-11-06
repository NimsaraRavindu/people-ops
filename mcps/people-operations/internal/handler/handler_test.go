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

package handler

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func validTestJWT() string {
	h := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	p := base64.RawURLEncoding.EncodeToString([]byte(`{"email":"user@example.com","sub":"user-123"}`))
	return h + "." + p + ".test-sig"
}

type roundTripFunc func(req *http.Request) *http.Response

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req), nil
}

func newTestHandler(rt roundTripFunc) *Handler {
	client := &http.Client{
		Transport: rt,
	}
	return NewHandler(Config{
		LeaveBackendURL:        "https://mock-leave.example.com",
		PeopleBackendURL:       "https://mock-people.example.com",
		LetterRequestFormURL:   "https://forms.example.com/po-letters",
		MenuBackendURL:         "https://mock-menu.example.com",
		SubscriptionBackendURL: "https://mock-sub.example.com",
		HTTPClient:             client,
	})
}

func TestHealthHandler(t *testing.T) {
	h := newTestHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	h.Health(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["status"] != "healthy" {
		t.Fatalf("expected status healthy, got %s", body["status"])
	}
}

func TestRegisterHandler(t *testing.T) {
	h := newTestHandler(nil)
	req := httptest.NewRequest(http.MethodGet, "/register", nil)
	w := httptest.NewRecorder()

	h.Register(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var buckets []map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&buckets)
	if len(buckets) != 6 {
		t.Fatalf("expected 6 buckets, got %d", len(buckets))
	}
}

func TestUnknownToolReturns404(t *testing.T) {
	h := newTestHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/tools/unknown_hallucinated_tool", bytes.NewBufferString("{}"))
	req.Header.Set("Authorization", "Bearer mock-token")
	w := httptest.NewRecorder()

	h.Tools(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 Not Found for unknown tool, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	if body["success"] == true {
		t.Fatalf("expected success: false for unknown tool")
	}
}

func TestToolsMissingTokenReturns401(t *testing.T) {
	h := newTestHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/tools/validate_leave", bytes.NewBufferString("{}"))
	w := httptest.NewRecorder()

	h.Tools(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized when token missing, got %d", resp.StatusCode)
	}
}

func TestLetterRequestFormTool(t *testing.T) {
	h := newTestHandler(nil)
	req := httptest.NewRequest(http.MethodPost, "/tools/get_letter_request_form", bytes.NewBufferString("{}"))
	req.Header.Set("Authorization", "Bearer valid-test-token")
	w := httptest.NewRecorder()

	h.Tools(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	if body["form_url"] != "https://forms.example.com/po-letters" {
		t.Fatalf("unexpected form URL: %v", body["form_url"])
	}
}

func TestLeaveToolTokenRelay(t *testing.T) {
	var capturedAuth string
	var capturedAssertion string
	var capturedURL string

	mockRT := roundTripFunc(func(req *http.Request) *http.Response {
		capturedAuth = req.Header.Get("Authorization")
		capturedAssertion = req.Header.Get("x-jwt-assertion")
		capturedURL = req.URL.String()

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString(`{"workingDays": 1, "hasOverlap": false}`)),
			Header:     make(http.Header),
		}
	})

	testJWT := validTestJWT()
	h := newTestHandler(mockRT)
	payload := `{"start_date":"2026-11-01","end_date":"2026-11-01","leave_type":"casual","period_type":"one"}`
	req := httptest.NewRequest(http.MethodPost, "/tools/validate_leave", bytes.NewBufferString(payload))
	req.Header.Set("Authorization", "Bearer "+testJWT)
	w := httptest.NewRecorder()

	h.Tools(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	if capturedAuth != "Bearer "+testJWT {
		t.Fatalf("expected relayed Authorization header, got %s", capturedAuth)
	}
	if capturedAssertion != testJWT {
		t.Fatalf("expected relayed x-jwt-assertion header, got %s", capturedAssertion)
	}
	if capturedURL != "https://mock-leave.example.com/leaves?isValidationOnlyMode=true" {
		t.Fatalf("unexpected target URL: %s", capturedURL)
	}
}

func TestUnverifiedTokenRejectedForSensitiveTools(t *testing.T) {
	mockRT := roundTripFunc(func(req *http.Request) *http.Response {
		t.Fatal("downstream service should NOT be called when token is unverified")
		return nil
	})
	h := newTestHandler(mockRT)

	// Insecure alg: "none" token
	noneHeader := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(`{"email":"attacker@wso2.com","groups":["peopleOpsTeam"]}`))
	forgedNoneToken := noneHeader + "." + payload + "."

	toolsToTest := []struct {
		name string
		tool string
		body string
	}{
		{
			name: "cancel_leave rejects unverified token",
			tool: "cancel_leave",
			body: `{"leave_id": 123}`,
		},
		{
			name: "submit_leave rejects unverified token",
			tool: "submit_leave",
			body: `{"start_date":"2026-12-01","end_date":"2026-12-01","leave_type":"casual","period_type":"one"}`,
		},
		{
			name: "validate_leave rejects unverified token",
			tool: "validate_leave",
			body: `{"start_date":"2026-12-01","end_date":"2026-12-01","leave_type":"casual","period_type":"one"}`,
		},
		{
			name: "submit_lunch_feedback rejects unverified token",
			tool: "submit_lunch_feedback",
			body: `{"message":"Great lunch"}`,
		},
	}

	for _, tc := range toolsToTest {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(
				http.MethodPost,
				"/tools/"+tc.tool,
				bytes.NewBufferString(tc.body),
			)
			req.Header.Set("Authorization", "Bearer "+forgedNoneToken)
			w := httptest.NewRecorder()
			h.Tools(w, req)

			if w.Result().StatusCode != http.StatusUnauthorized {
				t.Fatalf("expected 401 Unauthorized for tool %s with unverified token, got %d",
					tc.tool, w.Result().StatusCode)
			}
		})
	}
}

func TestCheckCalendarDateFiltering(t *testing.T) {
	mockBackendResp := `{
		"holidays": [
			{"title": "Poya Day", "date": "2026-11-05T00:00:00Z"},
			{"title": "Christmas", "date": "2026-12-25T00:00:00Z"}
		],
		"leaves": [
			{
				"id": 101,
				"startDate": "2026-11-04T00:00:00Z",
				"endDate": "2026-11-06T00:00:00Z",
				"status": "APPROVED",
				"leaveType": "casual"
			},
			{
				"id": 102,
				"startDate": "2026-12-20T00:00:00Z",
				"endDate": "2026-12-22T00:00:00Z",
				"status": "APPROVED",
				"leaveType": "annual"
			}
		]
	}`

	mockRT := roundTripFunc(func(req *http.Request) *http.Response {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString(mockBackendResp)),
			Header:     make(http.Header),
		}
	})

	h := newTestHandler(mockRT)
	payload := `{"start_date":"2026-11-01","end_date":"2026-11-30"}`
	req := httptest.NewRequest(http.MethodPost, "/tools/check_calendar", bytes.NewBufferString(payload))
	req.Header.Set("Authorization", "Bearer my-secret-user-token")
	w := httptest.NewRecorder()

	h.Tools(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var res struct {
		Success  bool `json:"success"`
		Holidays []struct {
			Title string `json:"title"`
			Date  string `json:"date"`
		} `json:"holidays"`
		Leaves []struct {
			ID int64 `json:"id"`
		} `json:"leaves"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(res.Holidays) != 1 || res.Holidays[0].Title != "Poya Day" {
		t.Fatalf("expected 1 holiday (Poya Day) within November, got %d", len(res.Holidays))
	}
	if len(res.Leaves) != 1 || res.Leaves[0].ID != 101 {
		t.Fatalf("expected 1 leave (ID 101) within November, got %d", len(res.Leaves))
	}
}

func TestGetVehiclesQueryParams(t *testing.T) {
	var capturedURL string
	mockRT := roundTripFunc(func(req *http.Request) *http.Response {
		capturedURL = req.URL.String()
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString(`[]`)),
			Header:     make(http.Header),
		}
	})

	h := newTestHandler(mockRT)
	payload := `{"vehicleStatus":"ACTIVE","vehicleRegistrationNumber":"WP 1234","vehicleType":"CAR"}`
	req := httptest.NewRequest(http.MethodPost, "/tools/get_vehicles", bytes.NewBufferString(payload))
	req.Header.Set("Authorization", "Bearer "+validTestJWT())
	w := httptest.NewRecorder()

	h.Tools(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	expectedParts := []string{
		"vehicleStatus=ACTIVE",
		"vehicleRegistrationNumber=WP+1234",
		"vehicleType=CAR",
	}
	for _, part := range expectedParts {
		if !bytes.Contains([]byte(capturedURL), []byte(part)) {
			t.Fatalf("expected URL to contain %s, got: %s", part, capturedURL)
		}
	}
}

func TestGetLeaveBalance(t *testing.T) {
	mockBackendResp := `[
		{
			"year": 2026,
			"location": "Sri Lanka",
			"leavePolicy": {"annual": 14.0, "casual": 7.0},
			"policyAdjustedLeave": {"annual": 4.0, "casual": 2.0},
			"periodStart": "2026-01-01T00:00:00Z",
			"periodEnd": "2026-12-31T00:00:00Z"
		}
	]`
	mockRT := roundTripFunc(func(req *http.Request) *http.Response {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString(mockBackendResp)),
			Header:     make(http.Header),
		}
	})

	h := newTestHandler(mockRT)
	req := httptest.NewRequest(http.MethodPost, "/tools/get_leave_balance", bytes.NewBufferString(`{}`))
	req.Header.Set("Authorization", "Bearer "+validTestJWT())
	w := httptest.NewRecorder()

	h.Tools(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var res struct {
		Success  bool `json:"success"`
		Year     int  `json:"year"`
		Balances map[string]struct {
			Entitled float64 `json:"entitled"`
			Taken    float64 `json:"taken"`
			Balance  float64 `json:"balance"`
		} `json:"balances"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	if !res.Success || res.Year != 2026 {
		t.Fatalf("unexpected year/success: %+v", res)
	}
	annual := res.Balances["annual"]
	if annual.Entitled != 14.0 || annual.Taken != 4.0 || annual.Balance != 10.0 {
		t.Fatalf("expected annual balance 10.0 (14-4), got %+v", annual)
	}
	casual := res.Balances["casual"]
	if casual.Entitled != 7.0 || casual.Taken != 2.0 || casual.Balance != 5.0 {
		t.Fatalf("expected casual balance 5.0 (7-2), got %+v", casual)
	}
}

func TestGetPendingLeavesFiltersPastApproved(t *testing.T) {
	mockBackendResp := `{
		"leaves": [
			{
				"id": 1,
				"leaveType": "casual",
				"startDate": "2020-01-01 00:00:00.0",
				"endDate": "2020-01-01 23:59:00.0",
				"status": "APPROVED"
			},
			{
				"id": 2,
				"leaveType": "casual",
				"startDate": "2029-12-01 00:00:00.0",
				"endDate": "2029-12-02 23:59:00.0",
				"status": "APPROVED"
			},
			{
				"id": 3,
				"leaveType": "sick",
				"startDate": "2020-01-01 00:00:00.0",
				"endDate": "2020-01-01 23:59:00.0",
				"status": "PENDING"
			}
		]
	}`
	mockRT := roundTripFunc(func(req *http.Request) *http.Response {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString(mockBackendResp)),
			Header:     make(http.Header),
		}
	})

	h := newTestHandler(mockRT)
	req := httptest.NewRequest(http.MethodPost, "/tools/get_pending_leaves", bytes.NewBufferString(`{}`))
	req.Header.Set("Authorization", "Bearer "+validTestJWT())
	w := httptest.NewRecorder()

	h.Tools(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	var res struct {
		Success bool `json:"success"`
		Count   int  `json:"count"`
		Leaves  []struct {
			ID int64 `json:"id"`
		} `json:"leaves"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}

	// Must exclude ID 1 (past approved), and keep ID 2 (future approved) and ID 3 (pending)
	if res.Count != 2 {
		t.Fatalf("expected 2 leaves (future approved + pending), got %d", res.Count)
	}
	if res.Leaves[0].ID != 2 || res.Leaves[1].ID != 3 {
		t.Fatalf("expected leaves [2, 3], got %+v", res.Leaves)
	}
}

func TestGetPendingLeavesTimezoneSupport(t *testing.T) {
	mockBackendResp := `{
		"leaves": [
			{
				"id": 10,
				"leaveType": "annual",
				"startDate": "2026-06-15 00:00:00.0",
				"endDate": "2026-06-15 23:59:00.0",
				"status": "APPROVED"
			}
		]
	}`
	mockRT := roundTripFunc(func(req *http.Request) *http.Response {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString(mockBackendResp)),
			Header:     make(http.Header),
		}
	})
	h := newTestHandler(mockRT)

	// 1. With current_date before endDate -> leave is included
	body1 := `{"current_date":"2026-06-14"}`
	req1 := httptest.NewRequest(http.MethodPost, "/tools/get_pending_leaves", bytes.NewBufferString(body1))
	req1.Header.Set("Authorization", "Bearer "+validTestJWT())
	w1 := httptest.NewRecorder()
	h.Tools(w1, req1)
	if w1.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", w1.Result().StatusCode)
	}
	var res1 struct {
		Count int `json:"count"`
	}
	json.NewDecoder(w1.Body).Decode(&res1)
	if res1.Count != 1 {
		t.Fatalf("expected 1 leave when current_date is before endDate, got %d", res1.Count)
	}

	// 2. With current_date after endDate -> leave is filtered out
	body2 := `{"current_date":"2026-06-16"}`
	req2 := httptest.NewRequest(http.MethodPost, "/tools/get_pending_leaves", bytes.NewBufferString(body2))
	req2.Header.Set("Authorization", "Bearer "+validTestJWT())
	w2 := httptest.NewRecorder()
	h.Tools(w2, req2)
	var res2 struct {
		Count int `json:"count"`
	}
	json.NewDecoder(w2.Body).Decode(&res2)
	if res2.Count != 0 {
		t.Fatalf("expected 0 leaves when current_date is past endDate, got %d", res2.Count)
	}

	// 3. With X-User-Timezone header
	req3 := httptest.NewRequest(http.MethodPost, "/tools/get_pending_leaves", bytes.NewBufferString(`{}`))
	req3.Header.Set("Authorization", "Bearer "+validTestJWT())
	req3.Header.Set("X-User-Timezone", "America/New_York")
	w3 := httptest.NewRecorder()
	h.Tools(w3, req3)
	if w3.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200 with X-User-Timezone, got %d", w3.Result().StatusCode)
	}
}

func TestCancelLeaveArgumentParsing(t *testing.T) {
	var capturedURL string
	mockRT := roundTripFunc(func(req *http.Request) *http.Response {
		capturedURL = req.URL.String()
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString(`{"success":true}`)),
			Header:     make(http.Header),
		}
	})

	h := newTestHandler(mockRT)

	// Test 1: camelCase leaveId as string
	body1 := `{"leaveId":"987"}`
	req1 := httptest.NewRequest(http.MethodPost, "/tools/cancel_leave", bytes.NewBufferString(body1))
	req1.Header.Set("Authorization", "Bearer "+validTestJWT())
	w1 := httptest.NewRecorder()
	h.Tools(w1, req1)
	if w1.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for string leaveId, got %d", w1.Result().StatusCode)
	}
	if !strings.HasSuffix(capturedURL, "/leaves/987") {
		t.Fatalf("expected URL to end with /leaves/987, got %s", capturedURL)
	}

	// Test 2: missing leave_id returns 400
	req2 := httptest.NewRequest(http.MethodPost, "/tools/cancel_leave", bytes.NewBufferString(`{}`))
	req2.Header.Set("Authorization", "Bearer "+validTestJWT())
	w2 := httptest.NewRecorder()
	h.Tools(w2, req2)
	if w2.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing leave_id, got %d", w2.Result().StatusCode)
	}
}

func TestDeleteVehicleArgumentParsing(t *testing.T) {
	var capturedURL string
	mockRT := roundTripFunc(func(req *http.Request) *http.Response {
		capturedURL = req.URL.String()
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString(`{"success":true}`)),
			Header:     make(http.Header),
		}
	})

	h := newTestHandler(mockRT)

	// Test: snake_case vehicle_id as string
	body := `{"vehicle_id":"55"}`
	req := httptest.NewRequest(http.MethodPost, "/tools/delete_vehicle", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+validTestJWT())
	w := httptest.NewRecorder()
	h.Tools(w, req)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for string vehicle_id, got %d", w.Result().StatusCode)
	}
	if !strings.HasSuffix(capturedURL, "/vehicles/55") {
		t.Fatalf("expected URL to end with /vehicles/55, got %s", capturedURL)
	}
}

func TestBuildLeavePayloadNormalization(t *testing.T) {
	args := map[string]interface{}{
		"startDate":       "2026-11-01",
		"endDate":         "2026-11-01",
		"leaveType":       "CASUAL",
		"periodType":      "ONE",
		"isMorningLeave":  "true",
		"isPublicComment": false,
	}
	payloadBytes := buildLeavePayload(args, false)
	var parsed map[string]interface{}
	if err := json.Unmarshal(payloadBytes, &parsed); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if parsed["startDate"] != "2026-11-01" {
		t.Fatalf("expected startDate 2026-11-01, got %v", parsed["startDate"])
	}
	if parsed["leaveType"] != "casual" {
		t.Fatalf("expected lowercased leaveType 'casual', got %v", parsed["leaveType"])
	}
	if parsed["periodType"] != "one" {
		t.Fatalf("expected lowercased periodType 'one', got %v", parsed["periodType"])
	}
	if parsed["isMorningLeave"] != true {
		t.Fatalf("expected isMorningLeave true, got %v", parsed["isMorningLeave"])
	}
}

func TestTrailingSlashSanitization(t *testing.T) {
	var capturedURL string
	mockRT := roundTripFunc(func(req *http.Request) *http.Response {
		capturedURL = req.URL.String()
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString(`[]`)),
			Header:     make(http.Header),
		}
	})

	client := &http.Client{Transport: mockRT}
	h := NewHandler(Config{
		LeaveBackendURL:        "https://mock-leave.example.com/",
		PeopleBackendURL:       "https://mock-people.example.com/v1.0/",
		LetterRequestFormURL:   "https://forms.example.com/po-letters/",
		MenuBackendURL:         "https://mock-menu.example.com/v1.0/",
		SubscriptionBackendURL: "https://mock-sub.example.com/v1.0/",
		HTTPClient:             client,
	})

	req := httptest.NewRequest(http.MethodPost, "/tools/get_menu", bytes.NewBufferString(`{}`))
	req.Header.Set("Authorization", "Bearer user-token")
	w := httptest.NewRecorder()
	h.Tools(w, req)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Result().StatusCode)
	}
	expected := "https://mock-menu.example.com/v1.0/menu"
	if capturedURL != expected {
		t.Fatalf("expected %s, got %s (double slash detected)", expected, capturedURL)
	}
}

func TestSubscribeCommutePayloadWithHTTPClient(t *testing.T) {
	var metaCalled bool
	var subscribeCalled bool
	mockRT := roundTripFunc(func(req *http.Request) *http.Response {
		if strings.Contains(req.URL.Path, "subscriptions/meta-info") {
			metaCalled = true
			metaJSON := `{"distances":[{"id":5,"minKm":10,"maxKm":20}]}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(metaJSON)),
				Header:     make(http.Header),
			}
		}
		if strings.Contains(req.URL.Path, "/subscribe") {
			subscribeCalled = true
			var body map[string]interface{}
			json.NewDecoder(req.Body).Decode(&body)
			if int(body["distanceRangeId"].(float64)) != 5 {
				t.Errorf("expected distanceRangeId 5, got %v", body["distanceRangeId"])
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(`{"success":true}`)),
				Header:     make(http.Header),
			}
		}
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(bytes.NewBufferString(`{}`)),
			Header:     make(http.Header),
		}
	})

	h := newTestHandler(mockRT)
	body := `{"distance_km":15.0,"contact_number":"+94771234567"}`
	req := httptest.NewRequest(http.MethodPost, "/tools/subscribe_commute", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+validTestJWT())
	w := httptest.NewRecorder()
	h.Tools(w, req)

	if !metaCalled {
		t.Fatalf("expected meta-info endpoint to be called using shared HTTPClient")
	}
	if !subscribeCalled {
		t.Fatalf("expected subscribe endpoint to be called")
	}
}

func TestSubscribeCommuteStringDistance(t *testing.T) {
	var subscribeCalled bool
	mockRT := roundTripFunc(func(req *http.Request) *http.Response {
		if strings.Contains(req.URL.Path, "subscriptions/meta-info") {
			metaJSON := `{"distances":[{"id":5,"minKm":10,"maxKm":20}]}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(metaJSON)),
				Header:     make(http.Header),
			}
		}
		if strings.Contains(req.URL.Path, "/subscribe") {
			subscribeCalled = true
			var body map[string]interface{}
			json.NewDecoder(req.Body).Decode(&body)
			if int(body["distanceRangeId"].(float64)) != 5 {
				t.Errorf("expected distanceRangeId 5, got %v", body["distanceRangeId"])
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(`{"success":true}`)),
				Header:     make(http.Header),
			}
		}
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       io.NopCloser(bytes.NewBufferString(`{}`)),
			Header:     make(http.Header),
		}
	})

	h := newTestHandler(mockRT)
	body := `{"distanceKm":"15.5","contactNumber":"+94771234567"}`
	req := httptest.NewRequest(http.MethodPost, "/tools/subscribe_commute", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+validTestJWT())
	w := httptest.NewRecorder()
	h.Tools(w, req)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Result().StatusCode)
	}
	if !subscribeCalled {
		t.Fatalf("expected subscribe endpoint to be called")
	}
}

func TestSubscribeCommuteMissingContactNumber(t *testing.T) {
	h := newTestHandler(nil)
	body := `{"distanceKm":15.0}`
	req := httptest.NewRequest(http.MethodPost, "/tools/subscribe_commute", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+validTestJWT())
	w := httptest.NewRecorder()
	h.Tools(w, req)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing contact number, got %d", w.Result().StatusCode)
	}
	var res map[string]interface{}
	json.NewDecoder(w.Result().Body).Decode(&res)
	if !strings.Contains(fmt.Sprint(res["error"]), "contactNumber") {
		t.Fatalf("expected contactNumber error message, got %v", res["error"])
	}
}

func TestSubscribeCommuteOutOfRangeDistance(t *testing.T) {
	mockRT := roundTripFunc(func(req *http.Request) *http.Response {
		if strings.Contains(req.URL.Path, "subscriptions/meta-info") {
			metaJSON := `{"distances":[{"id":1,"minKm":1,"maxKm":10},{"id":2,"minKm":11,"maxKm":20}]}`
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(bytes.NewBufferString(metaJSON)),
				Header:     make(http.Header),
			}
		}
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       io.NopCloser(bytes.NewBufferString(`{}`)),
			Header:     make(http.Header),
		}
	})

	h := newTestHandler(mockRT)
	body := `{"distanceKm":99.0,"contactNumber":"+94771234567"}`
	req := httptest.NewRequest(http.MethodPost, "/tools/subscribe_commute", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+validTestJWT())
	w := httptest.NewRecorder()
	h.Tools(w, req)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for out-of-range distance, got %d", w.Result().StatusCode)
	}
	var res map[string]interface{}
	json.NewDecoder(w.Result().Body).Decode(&res)
	if !strings.Contains(fmt.Sprint(res["error"]), "No commute plan covers") {
		t.Fatalf("expected out-of-range error message, got %v", res["error"])
	}
}

func TestSubscribeCommuteMetaInfoFailureReturns502(t *testing.T) {
	mockRT := roundTripFunc(func(req *http.Request) *http.Response {
		if strings.Contains(req.URL.Path, "subscriptions/meta-info") {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(bytes.NewBufferString(`{"error":"db failure"}`)),
				Header:     make(http.Header),
			}
		}
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       io.NopCloser(bytes.NewBufferString(`{}`)),
			Header:     make(http.Header),
		}
	})

	h := newTestHandler(mockRT)
	body := `{"distanceKm":15.0,"contactNumber":"+94771234567"}`
	req := httptest.NewRequest(http.MethodPost, "/tools/subscribe_commute", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+validTestJWT())
	w := httptest.NewRecorder()
	h.Tools(w, req)

	if w.Result().StatusCode != http.StatusBadGateway {
		t.Fatalf("expected 502 Bad Gateway when meta-info fails, got %d", w.Result().StatusCode)
	}
}

func TestSubmitLunchFeedbackValidation(t *testing.T) {
	mockRT := roundTripFunc(func(req *http.Request) *http.Response {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString(`{"success":true}`)),
			Header:     make(http.Header),
		}
	})
	h := newTestHandler(mockRT)

	tests := []struct {
		name       string
		body       string
		wantStatus int
		errSubstr  string
	}{
		{
			name:       "empty message rejected",
			body:       `{"message":""}`,
			wantStatus: http.StatusBadRequest,
			errSubstr:  "Missing or empty required parameter",
		},
		{
			name: "message exceeding 1000 characters rejected",
			body: fmt.Sprintf(
				`{"message":"%s"}`,
				strings.Repeat("A", 1001),
			),
			wantStatus: http.StatusBadRequest,
			errSubstr:  "exceeds maximum allowed length",
		},
		{
			name:       "message containing https url rejected",
			body:       `{"message":"Great food check https://evil.com/phish"}`,
			wantStatus: http.StatusBadRequest,
			errSubstr:  "must not contain hyperlinks or URLs",
		},
		{
			name:       "message containing www url rejected",
			body:       `{"message":"Great food www.malicious.com"}`,
			wantStatus: http.StatusBadRequest,
			errSubstr:  "must not contain hyperlinks or URLs",
		},
		{
			name:       "valid feedback message accepted",
			body:       `{"message":"The lunch today was fantastic, loved the curry!"}`,
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(
				http.MethodPost,
				"/tools/submit_lunch_feedback",
				bytes.NewBufferString(tt.body),
			)
			req.Header.Set("Authorization", "Bearer "+validTestJWT())
			w := httptest.NewRecorder()
			h.Tools(w, req)

			if w.Result().StatusCode != tt.wantStatus {
				t.Fatalf("expected status %d, got %d. Body: %s", tt.wantStatus, w.Result().StatusCode, w.Body.String())
			}
			if tt.errSubstr != "" && !strings.Contains(w.Body.String(), tt.errSubstr) {
				t.Fatalf("expected error body to contain %q, got %s", tt.errSubstr, w.Body.String())
			}
		})
	}
}

func TestLeaveCommentValidation(t *testing.T) {
	mockRT := roundTripFunc(func(req *http.Request) *http.Response {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewBufferString(`{"success":true}`)),
			Header:     make(http.Header),
		}
	})
	h := newTestHandler(mockRT)

	tests := []struct {
		name       string
		tool       string
		body       string
		wantStatus int
		errSubstr  string
	}{
		{
			name: "leave comment exceeding 1000 characters rejected in submit_leave",
			tool: "submit_leave",
			body: fmt.Sprintf(
				`{"start_date":"2026-12-01","end_date":"2026-12-01","leave_type":"casual","period_type":"one","comment":"%s"}`,
				strings.Repeat("B", 1001),
			),
			wantStatus: http.StatusBadRequest,
			errSubstr:  "exceeds maximum allowed length",
		},
		{
			name: "leave comment with URL rejected in submit_leave",
			tool: "submit_leave",
			body: `{"start_date":"2026-12-01","end_date":"2026-12-01","leave_type":"casual",` +
				`"period_type":"one","comment":"Taking leave see http://phishing.site"}`,
			wantStatus: http.StatusBadRequest,
			errSubstr:  "must not contain hyperlinks or URLs",
		},
		{
			name: "leave comment with URL rejected in validate_leave",
			tool: "validate_leave",
			body: `{"start_date":"2026-12-01","end_date":"2026-12-01","leave_type":"casual",` +
				`"period_type":"one","comment":"See https://phishing.site"}`,
			wantStatus: http.StatusBadRequest,
			errSubstr:  "must not contain hyperlinks or URLs",
		},
		{
			name: "valid leave comment accepted",
			tool: "submit_leave",
			body: `{"start_date":"2026-12-01","end_date":"2026-12-01","leave_type":"casual",` +
				`"period_type":"one","comment":"Family event"}`,
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(
				http.MethodPost,
				"/tools/"+tt.tool,
				bytes.NewBufferString(tt.body),
			)
			req.Header.Set("Authorization", "Bearer "+validTestJWT())
			w := httptest.NewRecorder()
			h.Tools(w, req)

			if w.Result().StatusCode != tt.wantStatus {
				t.Fatalf("expected status %d, got %d. Body: %s", tt.wantStatus, w.Result().StatusCode, w.Body.String())
			}
			if tt.errSubstr != "" && !strings.Contains(w.Body.String(), tt.errSubstr) {
				t.Fatalf("expected error body to contain %q, got %s", tt.errSubstr, w.Body.String())
			}
		})
	}
}
