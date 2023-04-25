package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/netlify/gotrue/conf"
	"github.com/netlify/gotrue/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type InvitationTestSuite struct {
	suite.Suite
	API        *API
	Config     *conf.Configuration
	instanceID uuid.UUID
}

func TestInvitation(t *testing.T) {
	api, config, _, instanceID, err := setupAPIForTestForInstance()
	require.NoError(t, err)

	ts := &InvitationTestSuite{
		API:        api,
		Config:     config,
		instanceID: instanceID,
	}

	suite.Run(t, ts)
}

func (ts *InvitationTestSuite) SetupTest() {
	models.TruncateAll(ts.API.db)
	ts.Config.Webhook = conf.WebhookConfig{}
}

// TestCreateInvitation tests API /invitation route
func (ts *InvitationTestSuite) TestCreateInvitation() {
	data := createInvitation(ts, "a@test.com", "editor", "org_a", "org_a_display_name", "google2|123", "org_a admin username", time.Now().UnixMilli()+86400*1000)
	assert.Equal(ts.T(), "a@test.com", data.Email)
	assert.Equal(ts.T(), "org_a", data.TigrisNamespace)
	assert.Equal(ts.T(), "google2|123", data.CreatedBy)
	assert.Equal(ts.T(), "org_a admin username", data.CreatedByName)
	assert.Equal(ts.T(), "editor", data.Role)
	assert.Equal(ts.T(), 33, len(data.Code)) // 30 default + length of default prefix
}

// TestCreateInvitation tests API /invitation route
func (ts *InvitationTestSuite) TestListInvitations() {
	// create 5 invitations, 3 for org_a and 2 for org_b
	_ = createInvitation(ts, "a@test.com", "editor", "org_a", "org_a_display_name", "google2|123", "org_a admin username", time.Now().UnixMilli()+86400*1000)
	_ = createInvitation(ts, "b@test.com", "editor", "org_a", "org_a_display_name", "google2|123", "org_a admin username", time.Now().UnixMilli()+86400*1000)
	_ = createInvitation(ts, "c@test.com", "editor", "org_b", "org_b_display_name", "google2|123", "org_a admin username", time.Now().UnixMilli()+86400*1000)
	_ = createInvitation(ts, "e@test.com", "editor", "org_a", "org_a_display_name", "google2|123", "org_a admin username", time.Now().UnixMilli()+86400*1000)
	_ = createInvitation(ts, "f@test.com", "editor", "org_b", "org_b_display_name", "google2|123", "org_a admin username", time.Now().UnixMilli()+86400*1000)

	// list invitations for org_a
	invitations := listInvitations(ts, "org_a")
	require.Equal(ts.T(), 3, len(invitations))
	require.Equal(ts.T(), "e@test.com", invitations[0].Email)
	require.Equal(ts.T(), "b@test.com", invitations[1].Email)
	require.Equal(ts.T(), "a@test.com", invitations[2].Email)
}

// TestDeleteInvitation tests API /invitation route
func (ts *InvitationTestSuite) TestDeleteInvitation() {
	// create 5 invitations, 3 for org_a and 2 for org_b
	_ = createInvitation(ts, "a@test.com", "editor", "org_a", "org_a_display_name", "google2|123", "org_a admin username", time.Now().UnixMilli()+86400*1000)
	_ = createInvitation(ts, "b@test.com", "editor", "org_a", "org_a_display_name", "google2|123", "org_a admin username", time.Now().UnixMilli()+86400*1000)
	_ = createInvitation(ts, "c@test.com", "editor", "org_b", "org_b_display_name", "google2|123", "org_a admin username", time.Now().UnixMilli()+86400*1000)
	_ = createInvitation(ts, "e@test.com", "editor", "org_a", "org_a_display_name", "google2|123", "org_a admin username", time.Now().UnixMilli()+86400*1000)
	_ = createInvitation(ts, "f@test.com", "editor", "org_b", "org_b_display_name", "google2|123", "org_a admin username", time.Now().UnixMilli()+86400*1000)

	// list invitations for org_a
	invitations := listInvitations(ts, "org_a")
	require.Equal(ts.T(), 3, len(invitations))

	// delete invitation
	deleteInvitation(ts, "b@test.com", "google2|123", InvitationStatusPending, "org_a")
	// list invitations for org_a
	invitations = listInvitations(ts, "org_a")
	require.Equal(ts.T(), 2, len(invitations))
}

// TestInvitationVerification tests API /invitation/verify route
func (ts *InvitationTestSuite) TestInvitationVerification() {
	email := "abc@test.com"
	_ = createInvitation(ts, "abc@test.com", "editor", "org_a", "org_a_display_name", "google2|123", "org_a admin username", time.Now().UnixMilli()+86400*1000)

	// list invitations for org_a
	invitations := listInvitations(ts, "org_a")
	require.Equal(ts.T(), 1, len(invitations))
	code := invitations[0].Code
	req := invitationVerificationRequest(ts, email, code)

	w1 := httptest.NewRecorder()

	ts.API.handler.ServeHTTP(w1, req)
	require.Equal(ts.T(), http.StatusOK, w1.Code)
	data := VerifyInvitationResponse{}
	require.NoError(ts.T(), json.NewDecoder(w1.Body).Decode(&data))
	require.Equal(ts.T(), "org_a", data.TigrisNamespace)
	require.Equal(ts.T(), "org_a_display_name", data.TigrisNamespaceName)

	// with invalid code
	w2 := httptest.NewRecorder()

	reqWithInvalidCode := invitationVerificationRequest(ts, email, "invalid")
	ts.API.handler.ServeHTTP(w2, reqWithInvalidCode)
	require.Equal(ts.T(), http.StatusUnauthorized, w2.Code)
}

func invitationVerificationRequest(ts *InvitationTestSuite, email string, code string) *http.Request {
	// Request body
	var buffer bytes.Buffer
	require.NoError(ts.T(), json.NewEncoder(&buffer).Encode(map[string]interface{}{
		"email": email,
		"code":  code,
	}))
	req := httptest.NewRequest(http.MethodPost, "/invitations/verify", &buffer)
	req.Header.Set("Content-Type", "application/json")
	return req
}
func listInvitations(ts *InvitationTestSuite, org string) []*models.Invitation {
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/invitations?tigris_namespace=%s", org), nil)
	req.Header.Set("Content-Type", "application/json")

	// Setup response recorder
	w := httptest.NewRecorder()

	ts.API.handler.ServeHTTP(w, req)

	require.Equal(ts.T(), http.StatusOK, w.Code)

	var invitations []*models.Invitation
	require.NoError(ts.T(), json.NewDecoder(w.Body).Decode(&invitations))
	return invitations
}

func deleteInvitation(ts *InvitationTestSuite, email string, createdBy string, status string, org string) {
	// Request body
	var buffer bytes.Buffer
	require.NoError(ts.T(), json.NewEncoder(&buffer).Encode(map[string]interface{}{
		"email":            email,
		"created_by":       createdBy,
		"tigris_namespace": org,
		"status":           status,
	}))
	req := httptest.NewRequest(http.MethodDelete, "/invitations", &buffer)
	req.Header.Set("Content-Type", "application/json")

	// Setup response recorder
	w := httptest.NewRecorder()

	ts.API.handler.ServeHTTP(w, req)
	require.Equal(ts.T(), http.StatusOK, w.Code)
}

func createInvitation(ts *InvitationTestSuite, email string, role string, tigrisNamespace string, tigrisNamespaceName string, createdBy string, createdByName string, expirationTime int64) models.Invitation {
	// Request body
	var buffer bytes.Buffer
	require.NoError(ts.T(), json.NewEncoder(&buffer).Encode(map[string]interface{}{
		"email":                 email,
		"role":                  role,
		"tigris_namespace":      tigrisNamespace,
		"tigris_namespace_name": tigrisNamespaceName,
		"created_by":            createdBy,
		"created_by_name":       createdByName,
		"expiration_time":       expirationTime,
	}))

	// Setup request
	req := httptest.NewRequest(http.MethodPost, "/invitations", &buffer)
	req.Header.Set("Content-Type", "application/json")

	// Setup response recorder
	w := httptest.NewRecorder()

	ts.API.handler.ServeHTTP(w, req)

	require.Equal(ts.T(), http.StatusOK, w.Code)

	data := models.Invitation{}
	require.NoError(ts.T(), json.NewDecoder(w.Body).Decode(&data))
	return data
}
