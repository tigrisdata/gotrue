package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"context"

	jwt "github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	"github.com/tigrisdata/gotrue/conf"
	"github.com/tigrisdata/gotrue/crypto"
	"github.com/tigrisdata/gotrue/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"github.com/tigrisdata/tigris-client-go/tigris"
)

type AuditTestSuite struct {
	suite.Suite
	API        *API
	Config     *conf.Configuration
	Encrypter  *crypto.AESBlockEncrypter
	token      string
	instanceID uuid.UUID
}

func TestAudit(t *testing.T) {
	api, config, globalConf, instanceID, err := setupAPIForTestForInstance()
	require.NoError(t, err)

	ts := &AuditTestSuite{
		API:        api,
		Config:     config,
		Encrypter:  &crypto.AESBlockEncrypter{Key: globalConf.DB.EncryptionKey},
		instanceID: instanceID,
	}

	suite.Run(t, ts)
}

func (ts *AuditTestSuite) SetupTest() {
	models.TruncateAll(ts.API.db)
	ts.token = ts.makeSuperAdmin("test@example.com")
}

func (ts *AuditTestSuite) makeSuperAdmin(email string) string {
	u, err := models.NewUser(ts.instanceID, email, "test", ts.Config.JWT.Aud, map[string]interface{}{"full_name": "Test User"}, ts.Encrypter)
	require.NoError(ts.T(), err, "Error making new user")

	u.IsSuperAdmin = true

	_, err = tigris.GetCollection[models.User](ts.API.db).Insert(context.TODO(), u)
	require.NoError(ts.T(), err, "Error creating user")

	tokenSigner := NewTokenSigner(ts.Config)

	token, err := generateAccessToken(u, time.Second*time.Duration(ts.Config.JWT.Exp), ts.Config, tokenSigner)
	require.NoError(ts.T(), err, "Error generating access token")

	p := jwt.Parser{ValidMethods: []string{jwt.SigningMethodHS256.Name, jwt.SigningMethodRS256.Name}}
	_, err = p.Parse(token, func(token *jwt.Token) (interface{}, error) {
		switch ts.Config.JWT.Algorithm {
		case jwt.SigningMethodRS256.Name:
			return tokenSigner.publicKey, nil
		case jwt.SigningMethodHS256.Name:
			return []byte(ts.Config.JWT.Secret), nil
		}
		return nil, nil
	})
	require.NoError(ts.T(), err, "Error parsing token")

	return token
}

func (ts *AuditTestSuite) TestAuditGet() {
	ts.prepareDeleteEvent()
	// CHECK FOR AUDIT LOG

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/audit", nil)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", ts.token))

	ts.API.handler.ServeHTTP(w, req)
	require.Equal(ts.T(), http.StatusOK, w.Code)

	//ToDo: pagination related
	//	assert.Equal(ts.T(), "</admin/audit?page=1>; rel=\"last\"", w.HeaderMap.Get("Link"))
	//	assert.Equal(ts.T(), "1", w.HeaderMap.Get("X-Total-Count"))

	logs := []models.AuditLogEntry{}
	require.NoError(ts.T(), json.NewDecoder(w.Body).Decode(&logs))

	require.Len(ts.T(), logs, 1)
	require.Contains(ts.T(), logs[0].Payload, "actor_email")
	assert.Equal(ts.T(), "test@example.com", logs[0].Payload["actor_email"])
	traits, ok := logs[0].Payload["traits"].(map[string]interface{})
	require.True(ts.T(), ok)
	require.Contains(ts.T(), traits, "user_email")
	assert.Equal(ts.T(), "test-delete@example.com", traits["user_email"])
}

func (ts *AuditTestSuite) TestAuditFilters() {
	ts.prepareDeleteEvent()

	queries := []string{
		"/admin/audit?query=action:user_deleted",
		"/admin/audit?query=type:team",
		"/admin/audit?query=author:user",
		"/admin/audit?query=author:@example.com",
	}

	for _, q := range queries {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, q, nil)
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", ts.token))

		ts.API.handler.ServeHTTP(w, req)
		require.Equal(ts.T(), http.StatusOK, w.Code)

		logs := []models.AuditLogEntry{}
		require.NoError(ts.T(), json.NewDecoder(w.Body).Decode(&logs))

		require.Len(ts.T(), logs, 1)
		require.Contains(ts.T(), logs[0].Payload, "actor_email")
		assert.Equal(ts.T(), "test@example.com", logs[0].Payload["actor_email"])
		traits, ok := logs[0].Payload["traits"].(map[string]interface{})
		require.True(ts.T(), ok)
		require.Contains(ts.T(), traits, "user_email")
		assert.Equal(ts.T(), "test-delete@example.com", traits["user_email"])
		fmt.Println("logs: ", logs)
	}
}

func (ts *AuditTestSuite) prepareDeleteEvent() {
	// DELETE USER
	u, err := models.NewUser(ts.instanceID, "test-delete@example.com", "test", ts.Config.JWT.Aud, nil, ts.Encrypter)
	require.NoError(ts.T(), err, "Error making new user")

	_, err = tigris.GetCollection[models.User](ts.API.db).Insert(context.TODO(), u)
	require.NoError(ts.T(), err, "Error creating user")

	// Setup request
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/admin/users/%s", u.Email), nil)
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", ts.token))

	ts.API.handler.ServeHTTP(w, req)
	require.Equal(ts.T(), http.StatusOK, w.Code)
}
