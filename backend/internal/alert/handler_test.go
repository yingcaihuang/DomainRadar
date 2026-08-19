package alert

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"domainradar/internal/auth"
	"domainradar/internal/domain"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// setupHandlerTestDB creates an in-memory SQLite database with the required tables for handler tests.
func setupHandlerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	err = db.AutoMigrate(&domain.NormalizedDomain{}, &domain.Alert{})
	require.NoError(t, err)

	return db
}

// setupAlertTestRouter creates a Gin test router with the AlertHandler registered.
func setupAlertTestRouter(t *testing.T, db *gorm.DB, user *auth.UserInfo) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	logger := zap.NewNop()
	handler := NewAlertHandler(db, logger)

	router := gin.New()
	api := router.Group("/api/v1")
	// Simulate auth middleware setting user context
	api.Use(func(c *gin.Context) {
		if user != nil {
			c.Set("user", user)
		}
		c.Next()
	})
	handler.RegisterRoutes(api)

	return router
}

// seedDomain creates a test domain in the database and returns it.
func seedDomain(t *testing.T, db *gorm.DB) domain.NormalizedDomain {
	t.Helper()
	expDate := time.Now().Add(30 * 24 * time.Hour)
	d := domain.NormalizedDomain{
		DomainName:          "example.com",
		RegistrarIdentifier: "godaddy",
		ExpirationDate:      &expDate,
		DataSourceType:      "api",
		Status:              "active",
	}
	require.NoError(t, db.Create(&d).Error)
	return d
}

// seedAlerts creates test alerts in the database and returns them.
func seedAlerts(t *testing.T, db *gorm.DB, domainID uint) []domain.Alert {
	t.Helper()
	now := time.Now()

	days7 := 7
	days30 := 30
	days90 := 90

	alerts := []domain.Alert{
		{
			DomainID:       domainID,
			AlertType:      "expiration",
			Severity:       SeverityCritical,
			Message:        "Domain example.com expires in 7 days",
			DaysRemaining:  &days7,
			DeliveryStatus: "pending",
			GeneratedAt:    now,
		},
		{
			DomainID:       domainID,
			AlertType:      "expiration",
			Severity:       SeverityWarning,
			Message:        "Domain example.com expires in 30 days",
			DaysRemaining:  &days30,
			DeliveryStatus: "delivered",
			GeneratedAt:    now.Add(-24 * time.Hour),
		},
		{
			DomainID:       domainID,
			AlertType:      "certificate",
			Severity:       SeverityInformational,
			Message:        "Certificate expires in 90 days",
			DaysRemaining:  &days90,
			Acknowledged:   true,
			DeliveryStatus: "delivered",
			GeneratedAt:    now.Add(-48 * time.Hour),
			AcknowledgedAt: timePtr(now.Add(-24 * time.Hour)),
		},
	}

	for i := range alerts {
		require.NoError(t, db.Create(&alerts[i]).Error)
	}

	return alerts
}

func timePtr(t time.Time) *time.Time {
	return &t
}

// --- NewAlertHandler Tests ---

func TestNewAlertHandler(t *testing.T) {
	db := setupHandlerTestDB(t)
	logger := zap.NewNop()
	handler := NewAlertHandler(db, logger)

	require.NotNil(t, handler)
	assert.Equal(t, db, handler.db)
	assert.Equal(t, logger, handler.logger)
}

// --- HandleListAlerts Tests ---

func TestHandleListAlerts_Success(t *testing.T) {
	db := setupHandlerTestDB(t)
	d := seedDomain(t, db)
	seedAlerts(t, db, d.ID)

	user := &auth.UserInfo{ID: 1, Roles: []string{auth.RoleViewer}}
	router := setupAlertTestRouter(t, db, user)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp alertListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, int64(3), resp.Total)
	assert.Equal(t, 1, resp.Page)
	assert.Equal(t, 20, resp.PageSize)
	assert.Len(t, resp.Alerts, 3)

	// Verify sorted by generated_at DESC (most recent first)
	assert.Equal(t, SeverityCritical, resp.Alerts[0].Severity)
}

func TestHandleListAlerts_FilterBySeverity(t *testing.T) {
	db := setupHandlerTestDB(t)
	d := seedDomain(t, db)
	seedAlerts(t, db, d.ID)

	user := &auth.UserInfo{ID: 1, Roles: []string{auth.RoleViewer}}
	router := setupAlertTestRouter(t, db, user)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts?severity=critical", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp alertListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, int64(1), resp.Total)
	assert.Len(t, resp.Alerts, 1)
	assert.Equal(t, SeverityCritical, resp.Alerts[0].Severity)
}

func TestHandleListAlerts_FilterByAlertType(t *testing.T) {
	db := setupHandlerTestDB(t)
	d := seedDomain(t, db)
	seedAlerts(t, db, d.ID)

	user := &auth.UserInfo{ID: 1, Roles: []string{auth.RoleViewer}}
	router := setupAlertTestRouter(t, db, user)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts?alert_type=certificate", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp alertListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, int64(1), resp.Total)
	assert.Len(t, resp.Alerts, 1)
	assert.Equal(t, "certificate", resp.Alerts[0].AlertType)
}

func TestHandleListAlerts_FilterByAcknowledged(t *testing.T) {
	db := setupHandlerTestDB(t)
	d := seedDomain(t, db)
	seedAlerts(t, db, d.ID)

	user := &auth.UserInfo{ID: 1, Roles: []string{auth.RoleViewer}}
	router := setupAlertTestRouter(t, db, user)

	// Filter acknowledged=true
	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts?acknowledged=true", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp alertListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, int64(1), resp.Total)
	assert.True(t, resp.Alerts[0].Acknowledged)

	// Filter acknowledged=false
	req = httptest.NewRequest(http.MethodGet, "/api/v1/alerts?acknowledged=false", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(2), resp.Total)
}

func TestHandleListAlerts_FilterByDateRange(t *testing.T) {
	db := setupHandlerTestDB(t)
	d := seedDomain(t, db)
	seedAlerts(t, db, d.ID)

	user := &auth.UserInfo{ID: 1, Roles: []string{auth.RoleViewer}}
	router := setupAlertTestRouter(t, db, user)

	today := time.Now().Format("2006-01-02")
	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts?date_from="+today+"&date_to="+today, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp alertListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	// Only the alert generated today should match
	assert.Equal(t, int64(1), resp.Total)
}

func TestHandleListAlerts_Pagination(t *testing.T) {
	db := setupHandlerTestDB(t)
	d := seedDomain(t, db)
	seedAlerts(t, db, d.ID)

	user := &auth.UserInfo{ID: 1, Roles: []string{auth.RoleViewer}}
	router := setupAlertTestRouter(t, db, user)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts?page=1&page_size=2", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp alertListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, int64(3), resp.Total)
	assert.Equal(t, 1, resp.Page)
	assert.Equal(t, 2, resp.PageSize)
	assert.Equal(t, 2, resp.TotalPages)
	assert.Len(t, resp.Alerts, 2)

	// Page 2
	req = httptest.NewRequest(http.MethodGet, "/api/v1/alerts?page=2&page_size=2", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Len(t, resp.Alerts, 1)
}

func TestHandleListAlerts_EmptyDatabase(t *testing.T) {
	db := setupHandlerTestDB(t)

	user := &auth.UserInfo{ID: 1, Roles: []string{auth.RoleViewer}}
	router := setupAlertTestRouter(t, db, user)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp alertListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(0), resp.Total)
	assert.Len(t, resp.Alerts, 0)
}

func TestHandleListAlerts_PreloadsDomainName(t *testing.T) {
	db := setupHandlerTestDB(t)
	d := seedDomain(t, db)
	seedAlerts(t, db, d.ID)

	user := &auth.UserInfo{ID: 1, Roles: []string{auth.RoleViewer}}
	router := setupAlertTestRouter(t, db, user)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp alertListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	for _, a := range resp.Alerts {
		assert.Equal(t, "example.com", a.DomainName)
	}
}

func TestHandleListAlerts_Unauthenticated(t *testing.T) {
	db := setupHandlerTestDB(t)
	router := setupAlertTestRouter(t, db, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandleListAlerts_PageSizeCap(t *testing.T) {
	db := setupHandlerTestDB(t)

	user := &auth.UserInfo{ID: 1, Roles: []string{auth.RoleViewer}}
	router := setupAlertTestRouter(t, db, user)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts?page_size=500", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp alertListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 100, resp.PageSize)
}

// --- HandleGetAlert Tests ---

func TestHandleGetAlert_Success(t *testing.T) {
	db := setupHandlerTestDB(t)
	d := seedDomain(t, db)
	alerts := seedAlerts(t, db, d.ID)

	user := &auth.UserInfo{ID: 1, Roles: []string{auth.RoleViewer}}
	router := setupAlertTestRouter(t, db, user)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/"+uitoa(alerts[0].ID), nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp alertResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, alerts[0].ID, resp.ID)
	assert.Equal(t, "expiration", resp.AlertType)
	assert.Equal(t, SeverityCritical, resp.Severity)
	assert.Equal(t, "example.com", resp.DomainName)
	assert.Equal(t, d.ID, resp.DomainID)
}

func TestHandleGetAlert_NotFound(t *testing.T) {
	db := setupHandlerTestDB(t)

	user := &auth.UserInfo{ID: 1, Roles: []string{auth.RoleViewer}}
	router := setupAlertTestRouter(t, db, user)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/99999", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleGetAlert_InvalidID(t *testing.T) {
	db := setupHandlerTestDB(t)

	user := &auth.UserInfo{ID: 1, Roles: []string{auth.RoleViewer}}
	router := setupAlertTestRouter(t, db, user)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/abc", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleGetAlert_Unauthenticated(t *testing.T) {
	db := setupHandlerTestDB(t)
	router := setupAlertTestRouter(t, db, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// --- HandleAcknowledgeAlert Tests ---

func TestHandleAcknowledgeAlert_Success(t *testing.T) {
	db := setupHandlerTestDB(t)
	d := seedDomain(t, db)
	alerts := seedAlerts(t, db, d.ID)

	// Use alert[0] which is not acknowledged
	user := &auth.UserInfo{ID: 1, Roles: []string{auth.RoleOperator}}
	router := setupAlertTestRouter(t, db, user)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/alerts/"+uitoa(alerts[0].ID)+"/acknowledge", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "alert acknowledged successfully", resp["message"])

	// Verify alert field in response
	alertResp := resp["alert"].(map[string]interface{})
	assert.Equal(t, true, alertResp["acknowledged"])
	assert.NotNil(t, alertResp["acknowledged_at"])

	// Verify in database
	var dbAlert domain.Alert
	require.NoError(t, db.First(&dbAlert, alerts[0].ID).Error)
	assert.True(t, dbAlert.Acknowledged)
	assert.NotNil(t, dbAlert.AcknowledgedAt)
}

func TestHandleAcknowledgeAlert_AlreadyAcknowledged(t *testing.T) {
	db := setupHandlerTestDB(t)
	d := seedDomain(t, db)
	alerts := seedAlerts(t, db, d.ID)

	// alert[2] is already acknowledged
	user := &auth.UserInfo{ID: 1, Roles: []string{auth.RoleOperator}}
	router := setupAlertTestRouter(t, db, user)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/alerts/"+uitoa(alerts[2].ID)+"/acknowledge", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAcknowledgeAlert_NotFound(t *testing.T) {
	db := setupHandlerTestDB(t)

	user := &auth.UserInfo{ID: 1, Roles: []string{auth.RoleOperator}}
	router := setupAlertTestRouter(t, db, user)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/alerts/99999/acknowledge", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleAcknowledgeAlert_InvalidID(t *testing.T) {
	db := setupHandlerTestDB(t)

	user := &auth.UserInfo{ID: 1, Roles: []string{auth.RoleOperator}}
	router := setupAlertTestRouter(t, db, user)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/alerts/abc/acknowledge", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleAcknowledgeAlert_Forbidden_Viewer(t *testing.T) {
	db := setupHandlerTestDB(t)
	d := seedDomain(t, db)
	alerts := seedAlerts(t, db, d.ID)

	// Viewer cannot manage alerts
	user := &auth.UserInfo{ID: 1, Roles: []string{auth.RoleViewer}}
	router := setupAlertTestRouter(t, db, user)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/alerts/"+uitoa(alerts[0].ID)+"/acknowledge", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestHandleAcknowledgeAlert_Unauthenticated(t *testing.T) {
	db := setupHandlerTestDB(t)
	d := seedDomain(t, db)
	alerts := seedAlerts(t, db, d.ID)

	router := setupAlertTestRouter(t, db, nil)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/alerts/"+uitoa(alerts[0].ID)+"/acknowledge", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandleAcknowledgeAlert_AdminCanAcknowledge(t *testing.T) {
	db := setupHandlerTestDB(t)
	d := seedDomain(t, db)
	alerts := seedAlerts(t, db, d.ID)

	// Admin should also be able to acknowledge
	user := &auth.UserInfo{ID: 1, Roles: []string{auth.RoleAdmin}}
	router := setupAlertTestRouter(t, db, user)

	req := httptest.NewRequest(http.MethodPut, "/api/v1/alerts/"+uitoa(alerts[0].ID)+"/acknowledge", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// --- Combination Filters ---

func TestHandleListAlerts_MultipleFilters(t *testing.T) {
	db := setupHandlerTestDB(t)
	d := seedDomain(t, db)
	seedAlerts(t, db, d.ID)

	user := &auth.UserInfo{ID: 1, Roles: []string{auth.RoleViewer}}
	router := setupAlertTestRouter(t, db, user)

	// Filter by severity=warning AND alert_type=expiration
	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts?severity=warning&alert_type=expiration", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp alertListResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	assert.Equal(t, int64(1), resp.Total)
	assert.Len(t, resp.Alerts, 1)
	assert.Equal(t, SeverityWarning, resp.Alerts[0].Severity)
	assert.Equal(t, "expiration", resp.Alerts[0].AlertType)
}

// uitoa is a helper to convert uint to string.
func uitoa(id uint) string {
	return strconv.FormatUint(uint64(id), 10)
}
