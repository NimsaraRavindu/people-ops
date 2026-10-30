// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com). All Rights Reserved.
//
// This software is the property of WSO2 LLC. and its suppliers, if any.
// Dissemination of any information or reproduction of any material contained
// herein in any form is strictly forbidden, unless permitted by WSO2 expressly.
// You may not alter or remove any copyright or other notice from copies of this content.

package handler

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
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

	h := newTestHandler(mockRT)
	payload := `{"start_date":"2026-11-01","end_date":"2026-11-01","leave_type":"casual","period_type":"one"}`
	req := httptest.NewRequest(http.MethodPost, "/tools/validate_leave", bytes.NewBufferString(payload))
	req.Header.Set("Authorization", "Bearer my-secret-user-token")
	w := httptest.NewRecorder()

	h.Tools(w, req)
	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", resp.StatusCode)
	}

	if capturedAuth != "Bearer my-secret-user-token" {
		t.Fatalf("expected relayed Authorization header, got %s", capturedAuth)
	}
	if capturedAssertion != "my-secret-user-token" {
		t.Fatalf("expected relayed x-jwt-assertion header, got %s", capturedAssertion)
	}
	if capturedURL != "https://mock-leave.example.com/leaves?isValidationOnlyMode=true" {
		t.Fatalf("unexpected target URL: %s", capturedURL)
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
