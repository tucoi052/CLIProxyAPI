package management

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
)

const (
	antigravityClientID     = "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com"
	antigravityClientSecret = "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"
)

// Token refresh response
type tokenRefreshResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int64  `json:"expires_in"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
}

// AntigravityQuotaResponse is the main response structure for the quota endpoint
type AntigravityQuotaResponse struct {
	TotalAccounts    int            `json:"total_accounts"`
	ActiveAccounts   int            `json:"active_accounts"`
	InactiveAccounts int            `json:"inactive_accounts"`
	ErrorAccounts    int            `json:"error_accounts"`
	Accounts         []AccountQuota `json:"accounts"`
	LastUpdated      time.Time      `json:"last_updated"`
}

// AccountQuota represents quota information for a single Antigravity account
type AccountQuota struct {
	Email       string        `json:"email"`
	ProjectID   string        `json:"project_id,omitempty"`
	Status      string        `json:"status"` // "active", "inactive", "error"
	ModelQuotas []ModelQuota  `json:"model_quotas,omitempty"`
	Error       string        `json:"error,omitempty"`
	LastUpdated time.Time     `json:"last_updated"`
}

// ModelQuota represents quota information for a specific model
type ModelQuota struct {
	Model            string  `json:"model"`
	DisplayName      string  `json:"display_name"`
	RemainingPercent float64 `json:"remaining_percent"`
	ResetTime        string  `json:"reset_time,omitempty"`
}

const (
	defaultAntigravityUserAgent = "antigravity/1.104.0 darwin/arm64"
)

// Google API response structures - support both formats
// Format 1: Array format (current implementation)
type fetchAvailableModelsResponseArray struct {
	Models []struct {
		Model     string `json:"model"`
		RateLimit struct {
			RpmLimit       int    `json:"rpmLimit"`
			TpmLimit       int    `json:"tpmLimit"`
			RemainingRpm   int    `json:"remainingRpm"`
			RemainingTpm   int    `json:"remainingTpm"`
			ResetTimeStamp string `json:"resetTimeStamp,omitempty"`
		} `json:"rateLimit"`
	} `json:"models"`
}

// Format 2: Map format (documentation format)
type fetchAvailableModelsResponseMap struct {
	Models map[string]struct {
		QuotaInfo *struct {
			RemainingFraction *float64 `json:"remainingFraction"`
			ResetTime         *string  `json:"resetTime"`
		} `json:"quotaInfo,omitempty"`
	} `json:"models"`
}

// GetAntigravityQuota fetches quota information for all Antigravity accounts
func (h *Handler) GetAntigravityQuota(c *gin.Context) {
	// Check if auth manager is available
	if h.authManager == nil {
		c.JSON(503, gin.H{"error": "auth manager unavailable"})
		return
	}

	// Get all auth files
	allAuths := h.authManager.List()

	// Filter for antigravity accounts
	var antigravityAuths []struct {
		email        string
		projectID    string
		accessToken  string
		refreshToken string
		authID       string
		expired      bool
	}

	for _, auth := range allAuths {
		// Check both Provider and metadata type
		isAntigravity := auth.Provider == "antigravity"
		if !isAntigravity && auth.Metadata != nil {
			if t, ok := auth.Metadata["type"].(string); ok && t == "antigravity" {
				isAntigravity = true
			}
		}
		if !isAntigravity {
			continue
		}

		// Extract metadata
		email := ""
		projectID := ""
		accessToken := ""
		refreshToken := ""
		expiredStr := ""

		if auth.Metadata != nil {
			if e, ok := auth.Metadata["email"].(string); ok {
				email = e
			}
			if p, ok := auth.Metadata["project_id"].(string); ok {
				projectID = p
			}
			if t, ok := auth.Metadata["access_token"].(string); ok {
				accessToken = t
			}
			if r, ok := auth.Metadata["refresh_token"].(string); ok {
				refreshToken = r
			}
			if exp, ok := auth.Metadata["expired"].(string); ok {
				expiredStr = exp
			}
		}

		// Check if token is expired
		isExpired := false
		if expiredStr != "" {
			expiredTime, err := time.Parse(time.RFC3339, expiredStr)
			now := time.Now()
			if err != nil {
				log.Printf("[Antigravity Quota] Failed to parse expiry for %s: %v (value: %s)", email, err, expiredStr)
			} else {
				log.Printf("[Antigravity Quota] Expiry check for %s: expired=%s (UTC), now=%s (UTC), isExpired=%v",
					email, expiredTime.UTC().Format(time.RFC3339), now.UTC().Format(time.RFC3339), now.After(expiredTime))
				if now.After(expiredTime) {
					isExpired = true
				}
			}
		} else {
			log.Printf("[Antigravity Quota] No expiry field for %s", email)
		}

		// Skip if no access token
		if accessToken == "" {
			continue
		}

		antigravityAuths = append(antigravityAuths, struct {
			email        string
			projectID    string
			accessToken  string
			refreshToken string
			authID       string
			expired      bool
		}{
			email:        email,
			projectID:    projectID,
			accessToken:  accessToken,
			refreshToken: refreshToken,
			authID:       auth.ID,
			expired:      isExpired,
		})
	}

	// Fetch quota for each account concurrently
	var wg sync.WaitGroup
	results := make(chan *AccountQuota, len(antigravityAuths))

	for _, auth := range antigravityAuths {
		wg.Add(1)
		go func(email, projectID, accessToken, refreshToken, authID string, expired bool) {
			defer wg.Done()

			log.Printf("[Antigravity Quota] Processing %s: expired=%v, hasRefreshToken=%v", email, expired, refreshToken != "")

			var actualToken string
			// If token is expired, try to refresh
			if expired && refreshToken != "" {
				log.Printf("[Antigravity Quota] Token expired for %s, attempting refresh...", email)
				newToken, expiresIn, err := h.refreshAccessToken(refreshToken)
				if err != nil {
					log.Printf("[Antigravity Quota] Token refresh FAILED for %s: %v", email, err)
					results <- &AccountQuota{
						Email:       email,
						ProjectID:   projectID,
						Status:      "inactive",
						Error:       fmt.Sprintf("Token refresh failed: %v", err),
						LastUpdated: time.Now(),
					}
					return
				}
				log.Printf("[Antigravity Quota] Token refresh SUCCESS for %s (expires in %d seconds)", email, expiresIn)
				actualToken = newToken
				// Update auth file with new token
				h.updateAuthToken(authID, newToken, expiresIn)
			} else if expired {
				log.Printf("[Antigravity Quota] Token expired but no refresh token for %s", email)
				results <- &AccountQuota{
					Email:       email,
					ProjectID:   projectID,
					Status:      "inactive",
					Error:       "Access token expired and no refresh token available",
					LastUpdated: time.Now(),
				}
				return
			} else {
				log.Printf("[Antigravity Quota] Using existing valid token for %s", email)
				actualToken = accessToken
			}

			quota := h.fetchQuotaForAccount(email, projectID, actualToken)
			results <- quota
		}(auth.email, auth.projectID, auth.accessToken, auth.refreshToken, auth.authID, auth.expired)
	}

	// Wait for all goroutines to complete
	go func() {
		wg.Wait()
		close(results)
	}()

	// Collect results
	accounts := make([]AccountQuota, 0, len(antigravityAuths))
	for quota := range results {
		accounts = append(accounts, *quota)
	}

	// Calculate statistics
	totalAccounts := len(accounts)
	activeAccounts := 0
	inactiveAccounts := 0
	errorAccounts := 0

	for _, account := range accounts {
		switch account.Status {
		case "active":
			activeAccounts++
		case "inactive":
			inactiveAccounts++
		case "error":
			errorAccounts++
		}
	}

	// Build response
	response := AntigravityQuotaResponse{
		TotalAccounts:    totalAccounts,
		ActiveAccounts:   activeAccounts,
		InactiveAccounts: inactiveAccounts,
		ErrorAccounts:    errorAccounts,
		Accounts:         accounts,
		LastUpdated:      time.Now(),
	}

	c.JSON(200, response)
}

// fetchQuotaForAccount fetches quota information for a single account
func (h *Handler) fetchQuotaForAccount(email, projectID, accessToken string) *AccountQuota {
	// Create HTTP client with proxy settings and timeout
	httpClient := util.SetProxy(&h.cfg.SDKConfig, &http.Client{
		Timeout: 10 * time.Second,
	})

	// Try endpoints in order with fallback
	endpoints := []string{
		"https://daily-cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels",
		"https://daily-cloudcode-pa.sandbox.googleapis.com/v1internal:fetchAvailableModels",
		"https://cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels",
	}

	var lastError error
	for _, endpoint := range endpoints {
		quota, err := h.callQuotaEndpoint(httpClient, endpoint, email, projectID, accessToken)
		if err == nil {
			return quota
		}

		lastError = err

		// If we got a 401 (token expired), don't try other endpoints
		if strings.Contains(err.Error(), "unauthorized") || strings.Contains(err.Error(), "access token expired") {
			return &AccountQuota{
				Email:       email,
				ProjectID:   projectID,
				Status:      "inactive",
				Error:       "Access token expired. Please re-login: CLIProxyAPI -antigravity-login",
				LastUpdated: time.Now(),
			}
		}

		// If we got a 403, try fallback method (extract quota from generateContent headers)
		if strings.Contains(err.Error(), "forbidden") || strings.Contains(err.Error(), "banned") {
			log.Printf("[Antigravity Quota] HTTP 403 for %s - trying fallback method (headers)", email)
			quotas, fallbackErr := h.fetchQuotaFromHeaders(httpClient, email, projectID, accessToken)
			if fallbackErr == nil {
				log.Printf("[Antigravity Quota] Fallback SUCCESS for %s - got %d model quotas", email, len(quotas))
				return &AccountQuota{
					Email:       email,
					ProjectID:   projectID,
					Status:      "active",
					ModelQuotas: quotas,
					LastUpdated: time.Now(),
				}
			}
			log.Printf("[Antigravity Quota] Fallback FAILED for %s: %v", email, fallbackErr)
			return &AccountQuota{
				Email:       email,
				ProjectID:   projectID,
				Status:      "inactive",
				Error:       fmt.Sprintf("Access forbidden and fallback failed: %v", fallbackErr),
				LastUpdated: time.Now(),
			}
		}

		// If not a 404, return the error
		if !strings.Contains(err.Error(), "404") {
			break
		}

		// Continue to next endpoint on 404
	}

	// All endpoints failed
	errorMsg := "Failed to fetch quota"
	if lastError != nil {
		errorMsg = lastError.Error()
	}

	return &AccountQuota{
		Email:       email,
		ProjectID:   projectID,
		Status:      "error",
		Error:       errorMsg,
		LastUpdated: time.Now(),
	}
}

// callQuotaEndpoint makes the actual API call to Google's quota endpoint
func (h *Handler) callQuotaEndpoint(httpClient *http.Client, endpoint, email, projectID, accessToken string) (*AccountQuota, error) {
	log.Printf("[Antigravity Quota] Calling endpoint for %s: %s", email, endpoint)

	// Create request with empty body
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint, strings.NewReader("{}"))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	// Set headers (matching antigravity executor pattern)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", defaultAntigravityUserAgent)
	req.Header.Set("X-Goog-Api-Client", "google-cloud-sdk vscode_cloudeshelleditor/0.1")
	req.Header.Set("Client-Metadata", `{"ideType":"IDE_UNSPECIFIED","platform":"PLATFORM_UNSPECIFIED","pluginType":"GEMINI"}`)

	// Execute request
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// Handle non-200 responses
	if resp.StatusCode == 401 {
		log.Printf("[Antigravity Quota] HTTP 401 for %s: %s", email, string(respBody))
		return nil, fmt.Errorf("unauthorized: access token expired")
	}
	if resp.StatusCode == 403 {
		log.Printf("[Antigravity Quota] HTTP 403 for %s: %s", email, string(respBody))
		return nil, fmt.Errorf("forbidden: %s", string(respBody))
	}
	if resp.StatusCode == 404 {
		log.Printf("[Antigravity Quota] HTTP 404 for %s (endpoint not found, trying next)", email)
		return nil, fmt.Errorf("404: endpoint not found")
	}
	if resp.StatusCode != 200 {
		log.Printf("[Antigravity Quota] HTTP %d for %s: %s", resp.StatusCode, email, string(respBody))
		return nil, fmt.Errorf("API returned %d: %s", resp.StatusCode, string(respBody))
	}

	log.Printf("[Antigravity Quota] HTTP 200 SUCCESS for %s", email)

	// Log response for debugging (first 500 chars)
	responsePreview := string(respBody)
	if len(responsePreview) > 500 {
		responsePreview = responsePreview[:500] + "..."
	}
	log.Printf("[Antigravity Quota] Response preview for %s: %s", email, responsePreview)

	// Try to parse as array format first
	var apiRespArray fetchAvailableModelsResponseArray
	if err := json.Unmarshal(respBody, &apiRespArray); err == nil && len(apiRespArray.Models) > 0 {
		log.Printf("[Antigravity Quota] Parsed as array format with %d models", len(apiRespArray.Models))
		return h.buildQuotasFromArray(email, projectID, apiRespArray)
	}

	// Try to parse as map format (documentation format)
	var apiRespMap fetchAvailableModelsResponseMap
	if err := json.Unmarshal(respBody, &apiRespMap); err == nil && len(apiRespMap.Models) > 0 {
		log.Printf("[Antigravity Quota] Parsed as map format with %d models", len(apiRespMap.Models))
		return h.buildQuotasFromMap(email, projectID, apiRespMap)
	}

	// If both fail, return error with response preview
	return nil, fmt.Errorf("parse response: failed to parse as array or map format. Response: %s", responsePreview)
}

// buildQuotasFromArray builds model quotas from array format response
func (h *Handler) buildQuotasFromArray(email, projectID string, apiResp fetchAvailableModelsResponseArray) (*AccountQuota, error) {
	modelQuotas := make([]ModelQuota, 0)
	for _, model := range apiResp.Models {
		if model.RateLimit.RpmLimit == 0 {
			continue // Skip models with no rate limit
		}

		// Calculate remaining percentage
		remainingPercent := float64(model.RateLimit.RemainingRpm) / float64(model.RateLimit.RpmLimit) * 100

		displayName := h.getDisplayName(model.Model)

		modelQuotas = append(modelQuotas, ModelQuota{
			Model:            model.Model,
			DisplayName:      displayName,
			RemainingPercent: remainingPercent,
			ResetTime:        model.RateLimit.ResetTimeStamp,
		})
	}

	return &AccountQuota{
		Email:       email,
		ProjectID:   projectID,
		Status:      "active",
		ModelQuotas: modelQuotas,
		LastUpdated: time.Now(),
	}, nil
}

// buildQuotasFromMap builds model quotas from map format response (documentation format)
func (h *Handler) buildQuotasFromMap(email, projectID string, apiResp fetchAvailableModelsResponseMap) (*AccountQuota, error) {
	modelQuotas := make([]ModelQuota, 0)
	for modelName, modelInfo := range apiResp.Models {
		if modelInfo.QuotaInfo == nil || modelInfo.QuotaInfo.RemainingFraction == nil {
			continue // Skip models with no quota info
		}

		remainingFraction := *modelInfo.QuotaInfo.RemainingFraction
		remainingPercent := remainingFraction * 100.0

		displayName := h.getDisplayName(modelName)

		resetTime := ""
		if modelInfo.QuotaInfo.ResetTime != nil {
			resetTime = *modelInfo.QuotaInfo.ResetTime
		}

		modelQuotas = append(modelQuotas, ModelQuota{
			Model:            modelName,
			DisplayName:      displayName,
			RemainingPercent: remainingPercent,
			ResetTime:        resetTime,
		})
	}

	return &AccountQuota{
		Email:       email,
		ProjectID:   projectID,
		Status:      "active",
		ModelQuotas: modelQuotas,
		LastUpdated: time.Now(),
	}, nil
}

// getDisplayName maps model names to display names
func (h *Handler) getDisplayName(modelName string) string {
	switch modelName {
	case "gemini-2.5-pro":
		return "Gemini 2.5 Pro"
	case "gemini-2.5-flash":
		return "Gemini 2.5 Flash"
	case "gemini-2.0-flash":
		return "Gemini 2.0 Flash"
	case "gemini-2.0-flash-lite":
		return "Gemini 2.0 Flash Lite"
	case "gemini-2.0-flash-exp":
		return "Gemini 2.0 Flash Exp"
	case "gemini-exp-1206":
		return "Gemini Exp"
	case "gemini-claude-sonnet-4-5", "gemini-claude-sonnet-4-5-thinking":
		return "Claude Sonnet 4.5"
	case "gemini-claude-opus-4-5", "gemini-claude-opus-4-5-thinking":
		return "Claude Opus 4.5"
	case "imagen-3.0-generate-002":
		return "Imagen 3"
	default:
		return modelName
	}
}

// refreshAccessToken refreshes an expired access token using refresh token
func (h *Handler) refreshAccessToken(refreshToken string) (string, int64, error) {
	httpClient := util.SetProxy(&h.cfg.SDKConfig, &http.Client{
		Timeout: 10 * time.Second,
	})

	// Build form data for token refresh
	data := url.Values{}
	data.Set("client_id", antigravityClientID)
	data.Set("client_secret", antigravityClientSecret)
	data.Set("refresh_token", refreshToken)
	data.Set("grant_type", "refresh_token")

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(data.Encode()))
	if err != nil {
		return "", 0, fmt.Errorf("create refresh request: %w", err)
	}
	req.Header.Set("Host", "oauth2.googleapis.com")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("execute refresh request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", 0, fmt.Errorf("refresh token failed: status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp tokenRefreshResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", 0, fmt.Errorf("parse refresh response: %w", err)
	}

	return tokenResp.AccessToken, tokenResp.ExpiresIn, nil
}

// updateAuthToken updates the access token in the auth file
func (h *Handler) updateAuthToken(authID, newAccessToken string, expiresIn int64) {
	if h.authManager == nil {
		return
	}

	ctx := context.Background()
	auth, ok := h.authManager.GetByID(authID)
	if !ok {
		return
	}

	// Ensure metadata exists
	if auth.Metadata == nil {
		auth.Metadata = make(map[string]any)
	}

	// Update metadata with new access token and new expiry
	auth.Metadata["access_token"] = newAccessToken
	auth.Metadata["expires_in"] = expiresIn
	auth.Metadata["timestamp"] = time.Now().UnixMilli()
	newExpiry := time.Now().Add(time.Duration(expiresIn) * time.Second).Format(time.RFC3339)
	auth.Metadata["expired"] = newExpiry

	// Save updated auth
	h.authManager.Update(ctx, auth)
}

// fetchQuotaFromHeaders extracts quota information from generateContent API response headers
// This is a fallback method for Google One accounts that don't have permission to fetchAvailableModels
func (h *Handler) fetchQuotaFromHeaders(httpClient *http.Client, email, projectID, accessToken string) ([]ModelQuota, error) {
	log.Printf("[Antigravity Quota] Using header fallback for %s", email)

	// List of models to check
	modelsToCheck := []string{
		"gemini-2.5-pro",
		"gemini-2.5-flash",
		"gemini-2.0-flash",
		"gemini-2.0-flash-lite",
		"gemini-exp-1206",
		"gemini-claude-sonnet-4-5",
		"gemini-claude-opus-4-5",
		"imagen-3.0-generate-002",
	}

	modelQuotas := make([]ModelQuota, 0)

	// Try each model to get quota from headers
	for _, modelName := range modelsToCheck {
		log.Printf("[Antigravity Quota] Trying to extract quota for model: %s", modelName)
		quota, err := h.extractQuotaForModel(httpClient, email, projectID, accessToken, modelName)
		if err != nil {
			log.Printf("[Antigravity Quota] Failed to get quota for %s: %v", modelName, err)
			continue
		}
		if quota != nil {
			modelQuotas = append(modelQuotas, *quota)
			log.Printf("[Antigravity Quota] Successfully extracted quota for %s", modelName)
		} else {
			log.Printf("[Antigravity Quota] No quota data for model: %s (headers missing)", modelName)
		}
	}

	if len(modelQuotas) == 0 {
		return nil, fmt.Errorf("no quota information extracted from headers")
	}

	log.Printf("[Antigravity Quota] Extracted %d model quotas from headers for %s", len(modelQuotas), email)
	return modelQuotas, nil
}

// extractQuotaForModel makes a minimal generateContent call and extracts quota from response headers
func (h *Handler) extractQuotaForModel(httpClient *http.Client, email, projectID, accessToken, modelName string) (*ModelQuota, error) {
	// Build minimal request in Antigravity format
	reqBody := map[string]interface{}{
		"model":     modelName,
		"project":   projectID,
		"requestId": fmt.Sprintf("quota-check-%d", time.Now().UnixNano()),
		"userAgent": "antigravity",
		"request": map[string]interface{}{
			"contents": []map[string]interface{}{
				{
					"parts": []map[string]string{{"text": "hi"}},
					"role":  "user",
				},
			},
			"generationConfig": map[string]interface{}{
				"temperature":     0.1,
				"maxOutputTokens": 1, // Minimal to save quota
			},
			"sessionId": fmt.Sprintf("quota-check-%d", time.Now().UnixNano()),
		},
	}

	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	// Determine base URL based on project ID pattern
	baseURL := "https://cloudcode-pa.googleapis.com"
	if strings.Contains(projectID, "daily") || strings.Contains(email, "daily") {
		baseURL = "https://daily-cloudcode-pa.googleapis.com"
	}

	endpoint := fmt.Sprintf("%s/v1internal:generateContent", baseURL)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, endpoint, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	// Set headers (matching antigravity pattern)
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", defaultAntigravityUserAgent)
	req.Header.Set("X-Goog-Api-Client", "google-cloud-sdk vscode_cloudeshelleditor/0.1")
	req.Header.Set("Client-Metadata", `{"ideType":"IDE_UNSPECIFIED","platform":"PLATFORM_UNSPECIFIED","pluginType":"GEMINI"}`)

	resp, err := httpClient.Do(req)
	if err != nil {
		log.Printf("[Antigravity Quota] Request failed for %s: %v", modelName, err)
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	// Log all headers for debugging
	headersList := []string{}
	for k, v := range resp.Header {
		headersList = append(headersList, fmt.Sprintf("%s=%s", k, strings.Join(v, ",")))
	}
	log.Printf("[Antigravity Quota] HTTP %d for model %s, All headers: %s",
		resp.StatusCode, modelName, strings.Join(headersList, "; "))

	// Read and log response body for debugging
	bodyBytes, readErr := io.ReadAll(resp.Body)
	log.Printf("[Antigravity Quota] Read body for %s: err=%v, status=%d, bodyLen=%d",
		modelName, readErr, resp.StatusCode, len(bodyBytes))

	if readErr == nil && resp.StatusCode == 200 {
		// Pretty print JSON for easier reading
		var jsonObj map[string]interface{}
		if err := json.Unmarshal(bodyBytes, &jsonObj); err == nil {
			prettyJSON, _ := json.MarshalIndent(jsonObj, "", "  ")
			log.Printf("[Antigravity Quota] Response for %s:\n%s", modelName, string(prettyJSON))
		} else {
			log.Printf("[Antigravity Quota] Raw response for %s: %s", modelName, string(bodyBytes))
		}
	}

	// Extract rate limit headers
	limitHeader := resp.Header.Get("X-RateLimit-Limit")
	remainingHeader := resp.Header.Get("X-RateLimit-Remaining")
	resetHeader := resp.Header.Get("X-RateLimit-Reset")

	if limitHeader == "" || remainingHeader == "" {
		// No rate limit headers, skip this model
		return nil, nil
	}

	// Parse quota values
	var limit, remaining int
	if _, err := fmt.Sscanf(limitHeader, "%d", &limit); err != nil || limit == 0 {
		return nil, nil
	}
	if _, err := fmt.Sscanf(remainingHeader, "%d", &remaining); err != nil {
		return nil, nil
	}

	// Calculate remaining percentage
	remainingPercent := float64(remaining) / float64(limit) * 100

	// Map model name to display name
	displayName := modelName
	switch modelName {
	case "gemini-2.5-pro":
		displayName = "Gemini 2.5 Pro"
	case "gemini-2.5-flash":
		displayName = "Gemini 2.5 Flash"
	case "gemini-2.0-flash":
		displayName = "Gemini 2.0 Flash"
	case "gemini-2.0-flash-lite":
		displayName = "Gemini 2.0 Flash Lite"
	case "gemini-2.0-flash-exp":
		displayName = "Gemini 2.0 Flash Exp"
	case "gemini-exp-1206":
		displayName = "Gemini Exp"
	case "gemini-claude-sonnet-4-5", "gemini-claude-sonnet-4-5-thinking":
		displayName = "Claude Sonnet 4.5"
	case "gemini-claude-opus-4-5", "gemini-claude-opus-4-5-thinking":
		displayName = "Claude Opus 4.5"
	case "imagen-3.0-generate-002":
		displayName = "Imagen 3"
	}

	log.Printf("[Antigravity Quota] %s - %s: %d/%d (%.1f%%)", email, displayName, remaining, limit, remainingPercent)

	return &ModelQuota{
		Model:            modelName,
		DisplayName:      displayName,
		RemainingPercent: remainingPercent,
		ResetTime:        resetHeader,
	}, nil
}
