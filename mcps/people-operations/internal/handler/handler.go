// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com). All Rights Reserved.
//
// This software is the property of WSO2 LLC. and its suppliers, if any.
// Dissemination of any information or reproduction of any material contained
// herein in any form is strictly forbidden, unless permitted by WSO2 expressly.
// You may not alter or remove any copyright or other notice from copies of this content.

package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/wso2-open-operations/people-ops/mcps/people-operations/internal/auth"
	"github.com/wso2-open-operations/people-ops/mcps/people-operations/internal/buckets"
	"github.com/wso2-open-operations/people-ops/mcps/people-operations/internal/model"
)

var validToolNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

var colomboLocation *time.Location

func init() {
	var err error
	colomboLocation, err = time.LoadLocation("Asia/Colombo")
	if err != nil {
		colomboLocation = time.FixedZone("Asia/Colombo", 5*3600+1800)
	}
}

// Config contains runtime URLs and HTTP configuration for the MCP server.
type Config struct {
	LeaveBackendURL        string
	PeopleBackendURL       string
	LetterRequestFormURL   string
	MenuBackendURL         string
	SubscriptionBackendURL string
	HTTPClient             *http.Client
}

// Handler orchestrates MCP discovery and tool dispatching.
type Handler struct {
	cfg Config
}

// NewHandler creates a new MCP Handler with the provided configuration.
func NewHandler(cfg Config) *Handler {
	cfg.LeaveBackendURL = strings.TrimRight(cfg.LeaveBackendURL, "/")
	cfg.PeopleBackendURL = strings.TrimRight(cfg.PeopleBackendURL, "/")
	cfg.LetterRequestFormURL = strings.TrimRight(cfg.LetterRequestFormURL, "/")
	cfg.MenuBackendURL = strings.TrimRight(cfg.MenuBackendURL, "/")
	cfg.SubscriptionBackendURL = strings.TrimRight(cfg.SubscriptionBackendURL, "/")

	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{
			Timeout: 8 * time.Second,
		}
	}
	return &Handler{cfg: cfg}
}

// Health handles GET /health probes.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "healthy"})
}

// Register handles GET /register discovery requests from Agent Core.
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(buckets.GetAllBuckets())
}

// Tools handles POST /tools/{tool_name} tool invocation requests.
func (h *Handler) Tools(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	toolName := strings.TrimPrefix(r.URL.Path, "/tools/")
	if !validToolNameRegex.MatchString(toolName) {
		http.Error(w, `{"error":"Invalid tool name format"}`, http.StatusBadRequest)
		return
	}

	// Verify that the requested tool is a registered capability
	if _, found := buckets.GetTool(toolName); !found {
		slog.Warn("Rejected unknown tool invocation", "tool", toolName)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   fmt.Sprintf("Tool '%s' is not registered or supported by this MCP server.", toolName),
		})
		return
	}

	// Parse incoming arguments payload
	var args map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&args); err != nil && err != io.EOF {
		http.Error(w, `{"error":"Invalid JSON payload"}`, http.StatusBadRequest)
		return
	}
	if args == nil {
		args = make(map[string]interface{})
	}

	// Extract and resolve user authentication token
	jwtToken := r.Header.Get("X-JWT-Assertion")
	if jwtToken == "" {
		authHeader := r.Header.Get("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			jwtToken = strings.TrimPrefix(authHeader, "Bearer ")
		}
	}
	if jwtToken == "" {
		slog.Warn("Rejected request: missing authentication token", "tool", toolName)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Unauthorized: missing or invalid authentication token",
		})
		return
	}
	resolvedToken := auth.ResolveToken(jwtToken)
	if resolvedToken == "" {
		slog.Warn("Rejected request: invalid authentication token", "tool", toolName)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{
			"error": "Unauthorized: missing or invalid authentication token",
		})
		return
	}

	h.dispatchTool(w, r, toolName, args, resolvedToken)
}

func (h *Handler) dispatchTool(
	w http.ResponseWriter,
	r *http.Request,
	toolName string,
	args map[string]interface{},
	jwtToken string,
) {
	// Guard: check user email extraction for employee-scoped tools to avoid double-slash 404s
	userScopedTools := map[string]bool{
		"get_leave_history":        true,
		"get_pending_leaves":       true,
		"get_leave_balance":        true,
		"get_vehicles":             true,
		"add_vehicle":              true,
		"delete_vehicle":           true,
		"get_my_location":          true,
		"fetch_leads":              true,
		"get_lunch_subscription":   true,
		"subscribe_lunch":          true,
		"unsubscribe_lunch":        true,
		"get_commute_subscription": true,
		"subscribe_commute":        true,
		"unsubscribe_commute":      true,
	}
	if userScopedTools[toolName] {
		userEmail := auth.ExtractEmailFromJWT(jwtToken)
		if userEmail == "" {
			slog.Warn("Rejected request: unable to extract employee email from token", "tool", toolName)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "Unable to determine authenticated user identity from token",
			})
			return
		}
	}

	var targetURL string
	var method string
	var reqBody []byte

	switch toolName {
	case "check_calendar":
		method = http.MethodGet
		startDate, _ := args["start_date"].(string)
		endDate, _ := args["end_date"].(string)
		targetURL = fmt.Sprintf("%s/user-calendar?startDate=%s&endDate=%s",
			h.cfg.LeaveBackendURL, url.QueryEscape(startDate), url.QueryEscape(endDate))

	case "validate_leave":
		method = http.MethodPost
		targetURL = fmt.Sprintf("%s/leaves?isValidationOnlyMode=true", h.cfg.LeaveBackendURL)
		reqBody = buildLeavePayload(args, true)

	case "submit_leave":
		method = http.MethodPost
		targetURL = fmt.Sprintf("%s/leaves?isValidationOnlyMode=false", h.cfg.LeaveBackendURL)
		reqBody = buildLeavePayload(args, false)

	case "get_leave_history":
		method = http.MethodGet
		userEmail := auth.ExtractEmailFromJWT(jwtToken)
		targetURL = fmt.Sprintf(
			"%s/leaves?email=%s&statuses=PENDING&statuses=APPROVED&statuses=CANCELLED&orderBy=DESC&limit=20",
			h.cfg.LeaveBackendURL, url.QueryEscape(userEmail),
		)
		if sDate, ok := args["start_date"].(string); ok && sDate != "" {
			targetURL += "&startDate=" + url.QueryEscape(sDate)
		}
		if eDate, ok := args["end_date"].(string); ok && eDate != "" {
			targetURL += "&endDate=" + url.QueryEscape(eDate)
		}

	case "get_pending_leaves":
		method = http.MethodGet
		userEmail := auth.ExtractEmailFromJWT(jwtToken)
		targetURL = fmt.Sprintf("%s/leaves?email=%s&statuses=PENDING&statuses=APPROVED",
			h.cfg.LeaveBackendURL, url.QueryEscape(userEmail))

	case "get_leave_balance":
		method = http.MethodGet
		userEmail := auth.ExtractEmailFromJWT(jwtToken)
		targetURL = fmt.Sprintf("%s/employees/%s/leave-entitlement",
			h.cfg.LeaveBackendURL, url.PathEscape(userEmail))

	case "cancel_leave":
		method = http.MethodDelete
		leaveID, ok := extractInt(args, "leave_id", "leaveId", "id")
		if !ok || leaveID <= 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "Missing or invalid leave_id parameter for cancellation.",
			})
			return
		}
		targetURL = fmt.Sprintf("%s/leaves/%d", h.cfg.LeaveBackendURL, leaveID)

	case "get_vehicles":
		method = http.MethodGet
		userEmail := auth.ExtractEmailFromJWT(jwtToken)
		targetURL = fmt.Sprintf("%s/employees/%s/vehicles", h.cfg.PeopleBackendURL, url.PathEscape(userEmail))
		params := url.Values{}
		if vs := extractString(args, "vehicleStatus", "vehicle_status", "status"); vs != "" {
			params.Add("vehicleStatus", vs)
		}
		if vrn := extractString(args, "vehicleRegistrationNumber", "vehicle_registration_number"); vrn != "" {
			params.Add("vehicleRegistrationNumber", vrn)
		}
		if vt := extractString(args, "vehicleType", "vehicle_type"); vt != "" {
			params.Add("vehicleType", vt)
		}
		if len(params) > 0 {
			targetURL += "?" + params.Encode()
		}

	case "add_vehicle":
		method = http.MethodPost
		userEmail := auth.ExtractEmailFromJWT(jwtToken)
		targetURL = fmt.Sprintf("%s/employees/%s/vehicles", h.cfg.PeopleBackendURL, url.PathEscape(userEmail))
		regNum := extractString(args, "vehicleRegistrationNumber", "vehicle_registration_number")
		vType := strings.ToUpper(extractString(args, "vehicleType", "vehicle_type"))
		if regNum == "" || vType == "" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "Missing required parameters: vehicleRegistrationNumber and vehicleType.",
			})
			return
		}
		vehiclePayload := map[string]interface{}{
			"vehicleRegistrationNumber": regNum,
			"vehicleType":               vType,
		}
		reqBody, _ = json.Marshal(vehiclePayload)

	case "delete_vehicle":
		method = http.MethodDelete
		userEmail := auth.ExtractEmailFromJWT(jwtToken)
		vehicleID, ok := extractInt(args, "vehicleId", "vehicle_id", "id")
		if !ok || vehicleID <= 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"error":   "Missing or invalid vehicleId parameter for deletion.",
			})
			return
		}
		targetURL = fmt.Sprintf(
			"%s/employees/%s/vehicles/%d",
			h.cfg.PeopleBackendURL, url.PathEscape(userEmail), vehicleID,
		)

	case "get_my_location":
		method = http.MethodGet
		userEmail := auth.ExtractEmailFromJWT(jwtToken)
		targetURL = fmt.Sprintf("%s/employees/%s", h.cfg.LeaveBackendURL, url.PathEscape(userEmail))

	case "fetch_leads":
		method = http.MethodGet
		userEmail := auth.ExtractEmailFromJWT(jwtToken)
		targetURL = fmt.Sprintf("%s/employees/%s", h.cfg.LeaveBackendURL, url.PathEscape(userEmail))

	case "get_letter_request_form":
		h.handleLetterRequestForm(w)
		return

	case "get_menu":
		method = http.MethodGet
		targetURL = fmt.Sprintf("%s/menu", h.cfg.MenuBackendURL)

	case "submit_lunch_feedback":
		method = http.MethodPost
		targetURL = fmt.Sprintf("%s/feedback", h.cfg.MenuBackendURL)
		msg, _ := args["message"].(string)
		feedbackPayload := map[string]interface{}{
			"message": msg,
			"meal":    "Lunch",
		}
		reqBody, _ = json.Marshal(feedbackPayload)

	case "get_lunch_subscription":
		method = http.MethodGet
		userEmail := auth.ExtractEmailFromJWT(jwtToken)
		targetURL = fmt.Sprintf("%s/meal/%s", h.cfg.SubscriptionBackendURL, url.PathEscape(userEmail))

	case "subscribe_lunch":
		method = http.MethodPost
		userEmail := auth.ExtractEmailFromJWT(jwtToken)
		targetURL = fmt.Sprintf("%s/meal/%s/subscribe", h.cfg.SubscriptionBackendURL, url.PathEscape(userEmail))

	case "unsubscribe_lunch":
		method = http.MethodPost
		userEmail := auth.ExtractEmailFromJWT(jwtToken)
		targetURL = fmt.Sprintf("%s/meal/%s/unsubscribe", h.cfg.SubscriptionBackendURL, url.PathEscape(userEmail))

	case "get_commute_subscription":
		method = http.MethodGet
		userEmail := auth.ExtractEmailFromJWT(jwtToken)
		targetURL = fmt.Sprintf("%s/commutes/%s", h.cfg.SubscriptionBackendURL, url.PathEscape(userEmail))

	case "subscribe_commute":
		method = http.MethodPost
		userEmail := auth.ExtractEmailFromJWT(jwtToken)
		targetURL = fmt.Sprintf("%s/commutes/%s/subscribe", h.cfg.SubscriptionBackendURL, url.PathEscape(userEmail))
		reqBody = h.buildCommutePayload(r.Context(), args, jwtToken)

	case "unsubscribe_commute":
		method = http.MethodPost
		userEmail := auth.ExtractEmailFromJWT(jwtToken)
		targetURL = fmt.Sprintf("%s/commutes/%s/unsubscribe", h.cfg.SubscriptionBackendURL, url.PathEscape(userEmail))
	}

	h.proxyBackendRequest(r.Context(), w, toolName, method, targetURL, reqBody, jwtToken, args)
}

func extractInt(args map[string]interface{}, keys ...string) (int, bool) {
	for _, key := range keys {
		val, exists := args[key]
		if !exists || val == nil {
			continue
		}
		switch v := val.(type) {
		case float64:
			return int(v), true
		case int:
			return v, true
		case int64:
			return int(v), true
		case string:
			clean := strings.TrimSpace(v)
			if parsed, err := strconv.Atoi(clean); err == nil {
				return parsed, true
			}
		}
	}
	return 0, false
}

func extractString(args map[string]interface{}, keys ...string) string {
	for _, key := range keys {
		if val, exists := args[key]; exists && val != nil {
			if s, ok := val.(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func extractBool(args map[string]interface{}, defaultVal bool, keys ...string) bool {
	for _, key := range keys {
		if val, exists := args[key]; exists && val != nil {
			switch v := val.(type) {
			case bool:
				return v
			case string:
				lower := strings.ToLower(strings.TrimSpace(v))
				if lower == "true" || lower == "1" {
					return true
				} else if lower == "false" || lower == "0" {
					return false
				}
			}
		}
	}
	return defaultVal
}

func buildLeavePayload(args map[string]interface{}, isValidation bool) []byte {
	startDate := extractString(args, "start_date", "startDate")
	endDate := extractString(args, "end_date", "endDate")
	leaveType := strings.ToLower(extractString(args, "leave_type", "leaveType"))

	periodType := "multiple"
	if pt := strings.ToLower(extractString(args, "period_type", "periodType")); pt != "" {
		periodType = pt
	} else if startDate == endDate {
		periodType = "one"
	}

	payload := map[string]interface{}{
		"startDate":  startDate,
		"endDate":    endDate,
		"leaveType":  leaveType,
		"periodType": periodType,
	}
	if isValidation {
		payload["emailRecipients"] = []string{}
	}
	if _, exists := args["is_morning_leave"]; exists {
		payload["isMorningLeave"] = extractBool(args, false, "is_morning_leave", "isMorningLeave")
	} else if _, exists := args["isMorningLeave"]; exists {
		payload["isMorningLeave"] = extractBool(args, false, "is_morning_leave", "isMorningLeave")
	}
	if comment, ok := args["comment"]; ok && comment != nil {
		payload["comment"] = comment
	}
	payload["isPublicComment"] = extractBool(args, false, "is_public_comment", "isPublicComment")

	b, _ := json.Marshal(payload)
	return b
}

func (h *Handler) handleLetterRequestForm(w http.ResponseWriter) {
	letterTypes := []string{
		"Employee confirmation",
		"Visa letter (mentions leave details)",
		"Passport renewal",
		"Service letter",
		"Salary undertaking letter (for banks)",
		"Employment & salary confirmation",
		"PFCA account opening",
		"Employment and USD consultancy confirmation",
		"Internship letter",
		"Employment confirmation and name mismatch in EPF/ETF records",
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":      true,
		"form_url":     h.cfg.LetterRequestFormURL,
		"letter_types": letterTypes,
		"message":      "Submit this form to request any of the listed letter types from the People Operations team.",
	})
}

func (h *Handler) buildCommutePayload(
	ctx context.Context,
	args map[string]interface{},
	jwtToken string,
) []byte {
	distKm := 0.0
	if d, ok := args["distanceKm"].(float64); ok {
		distKm = d
	} else if d, ok := args["distance_km"].(float64); ok {
		distKm = d
	}

	contactNumber := ""
	if c, ok := args["contactNumber"].(string); ok {
		contactNumber = c
	} else if c, ok := args["contact_number"].(string); ok {
		contactNumber = c
	}

	distanceRangeID := 0
	if id, ok := args["distanceRangeId"].(float64); ok {
		distanceRangeID = int(id)
	} else if distKm > 0 {
		metaURL := fmt.Sprintf("%s/subscriptions/meta-info", h.cfg.SubscriptionBackendURL)
		metaReq, err := http.NewRequestWithContext(ctx, http.MethodGet, metaURL, nil)
		if err == nil {
			metaReq.Header.Set("Authorization", "Bearer "+jwtToken)
			metaReq.Header.Set("x-jwt-assertion", jwtToken)
			if metaResp, err := h.cfg.HTTPClient.Do(metaReq); err == nil {
				defer metaResp.Body.Close()
				var meta model.SubscriptionsMetaInfo
				if err := json.NewDecoder(metaResp.Body).Decode(&meta); err == nil {
					for _, d := range meta.Distances {
						if int(distKm) >= d.MinKm && int(distKm) <= d.MaxKm {
							distanceRangeID = d.ID
							break
						}
					}
				}
			}
		}
	}

	payload := map[string]interface{}{
		"distanceRangeId": distanceRangeID,
		"contactNumber":   contactNumber,
	}
	b, _ := json.Marshal(payload)
	return b
}

func (h *Handler) proxyBackendRequest(
	ctx context.Context,
	w http.ResponseWriter,
	toolName string,
	method string,
	targetURL string,
	reqBody []byte,
	jwtToken string,
	args map[string]interface{},
) {
	var req *http.Request
	var reqErr error
	if len(reqBody) > 0 {
		req, reqErr = http.NewRequestWithContext(ctx, method, targetURL, bytes.NewBuffer(reqBody))
	} else {
		req, reqErr = http.NewRequestWithContext(ctx, method, targetURL, nil)
	}
	if reqErr != nil {
		slog.Error("Failed to create backend request", "tool", toolName, "error", reqErr)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Failed to create backend request: " + reqErr.Error(),
		})
		return
	}

	// Forward the identity token to the downstream backend
	req.Header.Set("Authorization", "Bearer "+jwtToken)
	req.Header.Set("x-jwt-assertion", jwtToken)
	req.Header.Set("Content-Type", "application/json")

	slog.Info("Proxying tool to backend", "tool", toolName, "method", method, "url", targetURL)
	resp, err := h.cfg.HTTPClient.Do(req)
	if err != nil {
		slog.Error("Failed to reach backend", "tool", toolName, "error", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"message": "Failed to reach backend: " + err.Error(),
		})
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "application/json")

	// Custom post-processing handlers
	if toolName == "check_calendar" && resp.StatusCode == http.StatusOK {
		h.formatCalendarResponse(w, resp.Body, args)
		return
	}

	if toolName == "get_pending_leaves" && resp.StatusCode == http.StatusOK {
		h.formatPendingLeavesResponse(w, resp.Body)
		return
	}

	if toolName == "get_leave_balance" && resp.StatusCode == http.StatusOK {
		h.formatLeaveBalanceResponse(w, resp.Body)
		return
	}

	if toolName == "get_my_location" && resp.StatusCode == http.StatusOK {
		h.formatLocationResponse(w, resp.Body)
		return
	}

	if toolName == "fetch_leads" && resp.StatusCode == http.StatusOK {
		h.formatLeadsResponse(w, resp.Body)
		return
	}

	if toolName == "add_vehicle" && resp.StatusCode == http.StatusCreated {
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Vehicle registered successfully.",
		})
		return
	}

	if toolName == "delete_vehicle" && resp.StatusCode == http.StatusOK {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Vehicle deleted successfully.",
		})
		return
	}

	if toolName == "get_lunch_subscription" && resp.StatusCode == http.StatusNotFound {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":       true,
			"is_subscribed": false,
			"message":       "You do not have an active lunch subscription.",
		})
		return
	}

	if toolName == "subscribe_lunch" && resp.StatusCode == http.StatusOK {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Subscribed to lunch successfully.",
		})
		return
	}

	if toolName == "unsubscribe_lunch" && resp.StatusCode == http.StatusOK {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Unsubscribed from lunch successfully.",
		})
		return
	}

	if toolName == "get_commute_subscription" && resp.StatusCode == http.StatusNotFound {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":       true,
			"is_subscribed": false,
			"message":       "You do not have an active commute subscription.",
		})
		return
	}

	if toolName == "subscribe_commute" && resp.StatusCode == http.StatusOK {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Subscribed to commute service successfully.",
		})
		return
	}

	if toolName == "unsubscribe_commute" && resp.StatusCode == http.StatusOK {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"message": "Unsubscribed from commute service successfully.",
		})
		return
	}

	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func (h *Handler) formatCalendarResponse(w http.ResponseWriter, body io.Reader, args map[string]interface{}) {
	var rawCal struct {
		Holidays []struct {
			Title string `json:"title"`
			Date  string `json:"date"`
		} `json:"holidays"`
		Leaves []struct {
			ID        int64  `json:"id"`
			StartDate string `json:"startDate"`
			EndDate   string `json:"endDate"`
			Status    string `json:"status"`
			LeaveType string `json:"leaveType"`
		} `json:"leaves"`
	}
	bodyBytes, err := io.ReadAll(body)
	if err != nil || json.Unmarshal(bodyBytes, &rawCal) != nil {
		w.WriteHeader(http.StatusOK)
		w.Write(bodyBytes)
		return
	}

	startDate, _ := args["start_date"].(string)
	endDate, _ := args["end_date"].(string)

	type HolidayItem struct {
		Title string `json:"title"`
		Date  string `json:"date"`
	}
	type LeaveItem struct {
		ID        int64  `json:"id"`
		StartDate string `json:"startDate"`
		EndDate   string `json:"endDate"`
		Status    string `json:"status"`
		LeaveType string `json:"leaveType"`
	}

	filteredHolidays := []HolidayItem{}
	for _, h := range rawCal.Holidays {
		hDate := h.Date
		if len(hDate) >= 10 {
			hDate = hDate[:10]
		}
		if (startDate == "" || hDate >= startDate) && (endDate == "" || hDate <= endDate) {
			filteredHolidays = append(filteredHolidays, HolidayItem{
				Title: h.Title,
				Date:  hDate,
			})
		}
	}

	filteredLeaves := []LeaveItem{}
	for _, l := range rawCal.Leaves {
		lStart := l.StartDate
		if len(lStart) >= 10 {
			lStart = lStart[:10]
		}
		lEnd := l.EndDate
		if len(lEnd) >= 10 {
			lEnd = lEnd[:10]
		}
		if (endDate == "" || lStart <= endDate) && (startDate == "" || lEnd >= startDate) {
			filteredLeaves = append(filteredLeaves, LeaveItem{
				ID:        l.ID,
				StartDate: lStart,
				EndDate:   lEnd,
				Status:    l.Status,
				LeaveType: l.LeaveType,
			})
		}
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"holidays": filteredHolidays,
		"leaves":   filteredLeaves,
	})
}

func (h *Handler) formatLocationResponse(w http.ResponseWriter, body io.Reader) {
	var uInfo struct {
		Location   string `json:"location"`
		EmployeeID string `json:"employeeId"`
		FirstName  string `json:"firstName"`
		LastName   string `json:"lastName"`
	}
	bodyBytes, err := io.ReadAll(body)
	if err == nil && json.Unmarshal(bodyBytes, &uInfo) == nil {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":     true,
			"location":    uInfo.Location,
			"employee_id": uInfo.EmployeeID,
			"name":        fmt.Sprintf("%s %s", uInfo.FirstName, uInfo.LastName),
		})
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write(bodyBytes)
}

func (h *Handler) formatLeadsResponse(w http.ResponseWriter, body io.Reader) {
	var emp struct {
		LeadEmail string `json:"leadEmail"`
		JobRole   string `json:"jobRole"`
		FirstName string `json:"firstName"`
		LastName  string `json:"lastName"`
	}
	bodyBytes, err := io.ReadAll(body)
	if err == nil && json.Unmarshal(bodyBytes, &emp) == nil {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":    true,
			"lead_email": emp.LeadEmail,
			"leadEmail":  emp.LeadEmail,
			"job_role":   emp.JobRole,
			"name":       fmt.Sprintf("%s %s", emp.FirstName, emp.LastName),
		})
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write(bodyBytes)
}

func (h *Handler) formatLeaveBalanceResponse(w http.ResponseWriter, body io.Reader) {
	var rawEntitlements []struct {
		Year                int                 `json:"year"`
		Location            string              `json:"location"`
		LeavePolicy         map[string]*float64 `json:"leavePolicy"`
		PolicyAdjustedLeave map[string]*float64 `json:"policyAdjustedLeave"`
		PeriodStart         string              `json:"periodStart"`
		PeriodEnd           string              `json:"periodEnd"`
	}
	bodyBytes, err := io.ReadAll(body)
	if err == nil && json.Unmarshal(bodyBytes, &rawEntitlements) == nil && len(rawEntitlements) > 0 {
		ent := rawEntitlements[0]
		type CategoryBalance struct {
			Entitled float64 `json:"entitled"`
			Taken    float64 `json:"taken"`
			Balance  float64 `json:"balance"`
		}
		balances := make(map[string]CategoryBalance)
		for category, entitledPtr := range ent.LeavePolicy {
			entitled := 0.0
			if entitledPtr != nil {
				entitled = *entitledPtr
			}
			taken := 0.0
			if takenPtr, exists := ent.PolicyAdjustedLeave[category]; exists && takenPtr != nil {
				taken = *takenPtr
			}
			balances[category] = CategoryBalance{
				Entitled: entitled,
				Taken:    taken,
				Balance:  entitled - taken,
			}
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success":      true,
			"year":         ent.Year,
			"location":     ent.Location,
			"period_start": ent.PeriodStart,
			"period_end":   ent.PeriodEnd,
			"balances":     balances,
		})
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write(bodyBytes)
}

func (h *Handler) formatPendingLeavesResponse(w http.ResponseWriter, body io.Reader) {
	var rawResp struct {
		Leaves []struct {
			ID            int64   `json:"id"`
			LeaveType     string  `json:"leaveType"`
			PeriodType    string  `json:"periodType"`
			StartDate     string  `json:"startDate"`
			EndDate       string  `json:"endDate"`
			NumberOfDays  float64 `json:"numberOfDays"`
			Status        string  `json:"status"`
			SubmitComment string  `json:"submitComment"`
		} `json:"leaves"`
	}
	bodyBytes, err := io.ReadAll(body)
	if err == nil && json.Unmarshal(bodyBytes, &rawResp) == nil {
		now := time.Now()
		if colomboLocation != nil {
			now = now.In(colomboLocation)
		}
		todayStr := now.Format("2006-01-02")
		cancellable := []interface{}{}

		for _, l := range rawResp.Leaves {
			sDate := l.StartDate
			if len(sDate) >= 10 {
				sDate = sDate[:10]
			}
			eDate := l.EndDate
			if len(eDate) >= 10 {
				eDate = eDate[:10]
			}

			// Include if status is PENDING, or APPROVED and not past (endDate >= today)
			isPending := strings.EqualFold(l.Status, "PENDING")
			isUpcomingApproved := strings.EqualFold(l.Status, "APPROVED") && eDate >= todayStr

			if isPending || isUpcomingApproved {
				cancellable = append(cancellable, map[string]interface{}{
					"id":             l.ID,
					"leave_type":     l.LeaveType,
					"period_type":    l.PeriodType,
					"start_date":     sDate,
					"end_date":       eDate,
					"number_of_days": l.NumberOfDays,
					"status":         l.Status,
					"comment":        l.SubmitComment,
				})
			}
		}

		w.WriteHeader(http.StatusOK)
		respPayload := map[string]interface{}{
			"success": true,
			"leaves":  cancellable,
			"count":   len(cancellable),
		}
		if len(cancellable) == 0 {
			respPayload["message"] = "You have no pending or upcoming leaves to cancel."
		}
		json.NewEncoder(w).Encode(respPayload)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write(bodyBytes)
}
