package api

import (
	"bytes"
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

type AdminTestSuite struct {
	suite.Suite
	User       *models.User
	API        *API
	Config     *conf.Configuration
	Encrypter  *crypto.AESBlockEncrypter
	token      string
	instanceID uuid.UUID
}

func TestAdmin(t *testing.T) {
	api, config, globalConf, instanceID, err := setupAPIForTestForInstance()
	require.NoError(t, err)

	ts := &AdminTestSuite{
		API:    api,
		Config: config,
		Encrypter: &crypto.AESBlockEncrypter{
			Key: globalConf.DB.EncryptionKey,
		},
		instanceID: instanceID,
	}

	suite.Run(t, ts)
}

func (ts *AdminTestSuite) SetupTest() {
	models.TruncateAll(ts.API.db)
	ts.Config.External.Email.Disabled = false
	ts.token = ts.makeSuperAdmin("test@example.com")
}

func (ts *AdminTestSuite) makeSuperAdmin(email string) string {
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
		if ts.Config.JWT.Algorithm == jwt.SigningMethodHS256.Name {
			return []byte(ts.Config.JWT.Secret), nil
		} else if ts.Config.JWT.Algorithm == jwt.SigningMethodRS256.Name {
			return tokenSigner.publicKey, nil
		}
		return nil, nil
	})
	require.NoError(ts.T(), err, "Error parsing token")

	return token
}

func (ts *AdminTestSuite) makeSystemUser() string {
	u := models.NewSystemUser(uuid.Nil, ts.Config.JWT.Aud)
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

// TestAdminUsersUnauthorized tests API /admin/users route without authentication
func (ts *AdminTestSuite) TestAdminUsersUnauthorized() {
	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	w := httptest.NewRecorder()

	ts.API.handler.ServeHTTP(w, req)
	assert.Equal(ts.T(), http.StatusUnauthorized, w.Code)
}

// TestAdminUsers tests API /admin/users route
func (ts *AdminTestSuite) TestAdminUsers() {
	// Setup request
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", ts.token))

	ts.API.handler.ServeHTTP(w, req)
	require.Equal(ts.T(), http.StatusOK, w.Code)

	// ToDo: this is related to pagination
	//assert.Equal(ts.T(), "</admin/users?page=1>; rel=\"last\"", w.HeaderMap.Get("Link"))
	//assert.Equal(ts.T(), "1", w.HeaderMap.Get("X-Total-Count"))

	data := struct {
		Users []*models.User `json:"users"`
		Aud   string         `json:"aud"`
	}{}
	require.NoError(ts.T(), json.NewDecoder(w.Body).Decode(&data))
	for _, user := range data.Users {
		ts.NotNil(user)
		ts.Require().NotNil(user.UserMetaData)
		ts.Equal("Test User", user.UserMetaData["full_name"])
	}
}

// TestAdminUsers tests API /admin/users route
func (ts *AdminTestSuite) TestAdminUsers_Pagination() {
	ts.T().Skip()

	u, err := models.NewUser(ts.instanceID, "test1@example.com", "test", ts.Config.JWT.Aud, nil, ts.Encrypter)
	require.NoError(ts.T(), err, "Error making new user")

	_, err = tigris.GetCollection[models.User](ts.API.db).Insert(context.TODO(), u)
	require.NoError(ts.T(), err, "Error creating user")

	u, err = models.NewUser(ts.instanceID, "test2@example.com", "test", ts.Config.JWT.Aud, nil, ts.Encrypter)
	require.NoError(ts.T(), err, "Error making new user")

	_, err = tigris.GetCollection[models.User](ts.API.db).Insert(context.TODO(), u)
	require.NoError(ts.T(), err, "Error creating user")

	// Setup request
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/users?per_page=1", nil)

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", ts.token))

	ts.API.handler.ServeHTTP(w, req)
	require.Equal(ts.T(), http.StatusOK, w.Code)

	assert.Equal(ts.T(), "</admin/users?page=2&per_page=1>; rel=\"next\", </admin/users?page=3&per_page=1>; rel=\"last\"", w.HeaderMap.Get("Link"))
	assert.Equal(ts.T(), "3", w.HeaderMap.Get("X-Total-Count"))

	data := make(map[string]interface{})
	require.NoError(ts.T(), json.NewDecoder(w.Body).Decode(&data))
	for _, user := range data["users"].([]interface{}) {
		assert.NotEmpty(ts.T(), user)
	}
}

// TestAdminUsers tests API /admin/users route
func (ts *AdminTestSuite) TestAdminUsers_SortAsc() {
	ts.T().Skip()

	u, err := models.NewUser(ts.instanceID, "test1@example.com", "test", ts.Config.JWT.Aud, nil, ts.Encrypter)
	require.NoError(ts.T(), err, "Error making new user")

	// if the created_at times are the same, then the sort order is not guaranteed
	time.Sleep(1 * time.Second)

	_, err = tigris.GetCollection[models.User](ts.API.db).Insert(context.TODO(), u)
	require.NoError(ts.T(), err, "Error creating user")

	// Setup request
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	qv := req.URL.Query()
	qv.Set("sort", "created_at asc")
	req.URL.RawQuery = qv.Encode()

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", ts.token))

	ts.API.handler.ServeHTTP(w, req)
	require.Equal(ts.T(), http.StatusOK, w.Code)

	data := struct {
		Users []*models.User `json:"users"`
		Aud   string         `json:"aud"`
	}{}
	require.NoError(ts.T(), json.NewDecoder(w.Body).Decode(&data))

	require.Len(ts.T(), data.Users, 2)
	assert.Equal(ts.T(), "test@example.com", data.Users[0].Email)
	assert.Equal(ts.T(), "test1@example.com", data.Users[1].Email)
}

// TestAdminUsers tests API /admin/users route
func (ts *AdminTestSuite) TestAdminUsers_SortDesc() {
	// enable test once sorting is implemented
	ts.T().Skip()
	u, err := models.NewUserWithAppData(ts.instanceID, "test1@example.com", "test", ts.Config.JWT.Aud, "test_role", nil, models.UserAppMetadata{
		TigrisNamespace: "test",
		TigrisProject:   "test",
		Name:            "test",
		Description:     "test",
		Provider:        "email",
	}, ts.Encrypter)
	require.NoError(ts.T(), err, "Error making new user")
	// if the created_at times are the same, then the sort order is not guaranteed
	time.Sleep(1 * time.Second)

	_, err = tigris.GetCollection[models.User](ts.API.db).Insert(context.TODO(), u)
	require.NoError(ts.T(), err, "Error creating user")

	// Setup request
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", ts.token))

	ts.API.handler.ServeHTTP(w, req)
	require.Equal(ts.T(), http.StatusOK, w.Code)

	data := struct {
		Users []*models.User `json:"users"`
		Aud   string         `json:"aud"`
	}{}
	require.NoError(ts.T(), json.NewDecoder(w.Body).Decode(&data))

	require.Len(ts.T(), data.Users, 2)
	assert.Equal(ts.T(), "test1@example.com", data.Users[0].Email)
	assert.Equal(ts.T(), "test@example.com", data.Users[1].Email)
}

// TestAdminUsers tests API /admin/users route
func (ts *AdminTestSuite) TestAdminUsers_FilterEmail() {
	u, err := models.NewUserWithAppData(ts.instanceID, "test1@example.com", "test", ts.Config.JWT.Aud, "test_role", nil, models.UserAppMetadata{
		TigrisNamespace: "test",
		TigrisProject:   "test",
		Name:            "test",
		Provider:        "email",
	}, ts.Encrypter)
	require.NoError(ts.T(), err, "Error making new user")

	_, err = tigris.GetCollection[models.User](ts.API.db).Insert(context.TODO(), u)
	require.NoError(ts.T(), err, "Error creating user")

	// Setup request
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/users?tigris_namespace=test&tigris_project=test", nil)

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", ts.token))

	ts.API.handler.ServeHTTP(w, req)
	require.Equal(ts.T(), http.StatusOK, w.Code)

	data := struct {
		Users []*models.User `json:"users"`
		Aud   string         `json:"aud"`
	}{}
	require.NoError(ts.T(), json.NewDecoder(w.Body).Decode(&data))

	require.Len(ts.T(), data.Users, 1)
	assert.Equal(ts.T(), "test1@example.com", data.Users[0].Email)
}

// TestAdminUsers tests API /admin/users route
func (ts *AdminTestSuite) TestAdminUsers_FilterName() {
	u, err := models.NewUserWithAppData(ts.instanceID, "test1@example.com", "test", ts.Config.JWT.Aud, "test_role", nil, models.UserAppMetadata{
		TigrisNamespace: "test",
		TigrisProject:   "test",
		Name:            "test",
		Provider:        "email",
	}, ts.Encrypter)
	require.NoError(ts.T(), err, "Error making new user")

	_, err = tigris.GetCollection[models.User](ts.API.db).Insert(context.TODO(), u)
	require.NoError(ts.T(), err, "Error creating user")

	// Setup request
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/users?filter=example&tigris_project=test", nil)

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", ts.token))

	ts.API.handler.ServeHTTP(w, req)
	require.Equal(ts.T(), http.StatusOK, w.Code)

	data := struct {
		Users []*models.User `json:"users"`
		Aud   string         `json:"aud"`
	}{}
	require.NoError(ts.T(), json.NewDecoder(w.Body).Decode(&data))

	require.Len(ts.T(), data.Users, 1)
	assert.Equal(ts.T(), "test1@example.com", data.Users[0].Email)
}

// TestAdminUsers_FilterTigrisProject tests API /admin/users route - creates 3 users for a test_namespace with different projects and queries them by tigris_project
func (ts *AdminTestSuite) TestAdminUsers_FilterTigrisProject() {
	// first user
	u, err := models.NewUserWithAppData(ts.instanceID, "test1@example.com", "test", ts.Config.JWT.Aud, "test_role", nil, models.UserAppMetadata{
		TigrisNamespace: "test_namespace",
		TigrisProject:   "test2",
		Name:            "test",
		Provider:        "email",
	}, ts.Encrypter)
	require.NoError(ts.T(), err, "Error making new user")

	_, err = tigris.GetCollection[models.User](ts.API.db).Insert(context.TODO(), u)
	require.NoError(ts.T(), err, "Error creating user")

	// second user
	u2, err := models.NewUserWithAppData(ts.instanceID, "test2@example.com", "test", ts.Config.JWT.Aud, "test_role", nil, models.UserAppMetadata{
		TigrisNamespace: "test_namespace",
		TigrisProject:   "test3",
		Name:            "test",
		Provider:        "email",
	}, ts.Encrypter)
	require.NoError(ts.T(), err, "Error making new user")
	_, err = tigris.GetCollection[models.User](ts.API.db).Insert(context.TODO(), u2)
	require.NoError(ts.T(), err, "Error creating user")

	// third user without project field
	u3, err := models.NewUserWithAppData(ts.instanceID, "test3@example.com", "test", ts.Config.JWT.Aud, "test_role", nil, models.UserAppMetadata{
		TigrisNamespace: "test_namespace",
		Name:            "test",
		Provider:        "email",
	}, ts.Encrypter)
	require.NoError(ts.T(), err, "Error making new user")

	_, err = tigris.GetCollection[models.User](ts.API.db).Insert(context.TODO(), u3)
	require.NoError(ts.T(), err, "Error creating user")

	// Setup request
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/users?tigris_project=test2&tigris_namespace=test_namespace", nil)

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", ts.token))

	ts.API.handler.ServeHTTP(w, req)
	require.Equal(ts.T(), http.StatusOK, w.Code)

	data := struct {
		Users []*models.User `json:"users"`
		Aud   string         `json:"aud"`
	}{}
	require.NoError(ts.T(), json.NewDecoder(w.Body).Decode(&data))

	require.Len(ts.T(), data.Users, 1)
}

// TestAdminUsers_EmptyResponse tests API /admin/users route - validates the empty response is an empty array for users
func (ts *AdminTestSuite) TestAdminUsers_EmptyResponse() {
	// Setup request
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/users?tigris_project=test2&tigris_namespace=invalid_namespace", nil)

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", ts.token))

	ts.API.handler.ServeHTTP(w, req)
	require.Equal(ts.T(), http.StatusOK, w.Code)

	data := struct {
		Users []*models.User `json:"users"`
		Aud   string         `json:"aud"`
	}{}
	require.NoError(ts.T(), json.NewDecoder(w.Body).Decode(&data))
	var emptyArray = make([]*models.User, 0)
	require.Equal(ts.T(), emptyArray, data.Users)
}

// TestAdminUserCreate tests API /admin/user route (POST)
func (ts *AdminTestSuite) TestAdminUserCreate() {
	var buffer bytes.Buffer
	require.NoError(ts.T(), json.NewEncoder(&buffer).Encode(map[string]interface{}{
		"email":    "test1@example.com",
		"password": "test1",
	}))

	// Setup request
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/users", &buffer)

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", ts.token))

	ts.API.handler.ServeHTTP(w, req)
	require.Equal(ts.T(), http.StatusOK, w.Code)

	data := models.User{}
	require.NoError(ts.T(), json.NewDecoder(w.Body).Decode(&data))
	assert.Equal(ts.T(), "test1@example.com", data.Email)
	assert.Equal(ts.T(), "email", data.AppMetaData.Provider)
}

// TestAdminUserGet tests API /admin/user route (GET)
func (ts *AdminTestSuite) TestAdminUserGet() {
	u, err := models.NewUser(ts.instanceID, "test1@example.com", "test", ts.Config.JWT.Aud, map[string]interface{}{"full_name": "Test Get User"}, ts.Encrypter)
	require.NoError(ts.T(), err, "Error making new user")

	_, err = tigris.GetCollection[models.User](ts.API.db).Insert(context.TODO(), u)
	require.NoError(ts.T(), err, "Error creating user")

	// Setup request
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/admin/users/%s", u.Email), nil)

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", ts.token))

	ts.API.handler.ServeHTTP(w, req)
	require.Equal(ts.T(), http.StatusOK, w.Code)

	data := make(map[string]interface{})
	require.NoError(ts.T(), json.NewDecoder(w.Body).Decode(&data))

	assert.Equal(ts.T(), data["email"], "test1@example.com")
	assert.Nil(ts.T(), data["app_metadata"])
	assert.NotNil(ts.T(), data["user_metadata"])
	md := data["user_metadata"].(map[string]interface{})
	assert.Len(ts.T(), md, 1)
	assert.Equal(ts.T(), "Test Get User", md["full_name"])
}

// TestAdminUserUpdate tests API /admin/user route (UPDATE)
func (ts *AdminTestSuite) TestAdminUserUpdate() {
	u, err := models.NewUser(ts.instanceID, "test1@example.com", "test", ts.Config.JWT.Aud, nil, ts.Encrypter)
	require.NoError(ts.T(), err, "Error making new user")

	_, err = tigris.GetCollection[models.User](ts.API.db).Insert(context.TODO(), u)
	require.NoError(ts.T(), err, "Error creating user")

	var buffer bytes.Buffer
	require.NoError(ts.T(), json.NewEncoder(&buffer).Encode(map[string]interface{}{
		"role": "testing",
		"app_metadata": map[string]interface{}{
			"roles": []string{"writer", "editor"},
		},
		"user_metadata": map[string]interface{}{
			"name": "David",
		},
	}))

	// Setup request
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/admin/users/%s", u.Email), &buffer)

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", ts.token))

	ts.API.handler.ServeHTTP(w, req)
	require.Equal(ts.T(), http.StatusOK, w.Code)

	data := models.User{}
	require.NoError(ts.T(), json.NewDecoder(w.Body).Decode(&data))

	assert.Equal(ts.T(), "testing", data.Role)
	assert.NotNil(ts.T(), data.UserMetaData)
	assert.Equal(ts.T(), "David", data.UserMetaData["name"])

	assert.NotNil(ts.T(), data.AppMetaData)
	assert.Len(ts.T(), data.AppMetaData.Roles, 2)
	assert.Contains(ts.T(), data.AppMetaData.Roles, "writer")
	assert.Contains(ts.T(), data.AppMetaData.Roles, "editor")
}

// TestAdminUserUpdate tests API /admin/user route (UPDATE) as system user
func (ts *AdminTestSuite) TestAdminUserUpdateAsSystemUser() {
	u, err := models.NewUser(ts.instanceID, "test1@example.com", "test", ts.Config.JWT.Aud, nil, ts.Encrypter)
	require.NoError(ts.T(), err, "Error making new user")

	_, err = tigris.GetCollection[models.User](ts.API.db).Insert(context.TODO(), u)
	require.NoError(ts.T(), err, "Error creating user")

	var buffer bytes.Buffer
	require.NoError(ts.T(), json.NewEncoder(&buffer).Encode(map[string]interface{}{
		"role": "testing",
		"app_metadata": map[string]interface{}{
			"roles": []string{"writer", "editor"},
		},
		"user_metadata": map[string]interface{}{
			"name": "David",
		},
	}))

	// Setup request
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/admin/users/%s", u.Email), &buffer)

	token := ts.makeSystemUser()

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	ts.API.handler.ServeHTTP(w, req)
	require.Equal(ts.T(), http.StatusOK, w.Code)

	data := make(map[string]interface{})
	require.NoError(ts.T(), json.NewDecoder(w.Body).Decode(&data))

	assert.Equal(ts.T(), data["role"], "testing")

	u, err = models.FindUserByEmailAndAudience(req.Context(), ts.API.db, ts.instanceID, "test1@example.com", ts.Config.JWT.Aud)
	require.NoError(ts.T(), err)
	assert.Equal(ts.T(), u.Role, "testing")
	require.NotNil(ts.T(), u.UserMetaData)
	require.Contains(ts.T(), u.UserMetaData, "name")
	assert.Equal(ts.T(), u.UserMetaData["name"], "David")
	require.NotNil(ts.T(), u.AppMetaData)
	require.NotNil(ts.T(), u.AppMetaData.Roles)
	require.NotEmpty(ts.T(), u.AppMetaData.Roles)
	assert.Len(ts.T(), u.AppMetaData.Roles, 2)
	assert.Contains(ts.T(), u.AppMetaData.Roles, "writer")
	assert.Contains(ts.T(), u.AppMetaData.Roles, "editor")
}

// TestAdminUserDelete tests API /admin/user route (DELETE)
func (ts *AdminTestSuite) TestAdminUserDelete() {
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

// TestAdminUserCreateWithManagementToken tests API /admin/user route using the management token (POST)
func (ts *AdminTestSuite) TestAdminUserCreateWithManagementToken() {
	var buffer bytes.Buffer
	require.NoError(ts.T(), json.NewEncoder(&buffer).Encode(map[string]interface{}{
		"email":    "test2@example.com",
		"password": "test2",
	}))

	// Setup request
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/users", &buffer)

	req.Header.Set("Authorization", "Bearer foobar")
	req.Header.Set("X-JWT-AUD", "op-test-aud")

	ts.API.handler.ServeHTTP(w, req)
	require.Equal(ts.T(), http.StatusOK, w.Code)

	data := models.User{}
	require.NoError(ts.T(), json.NewDecoder(w.Body).Decode(&data))

	assert.NotNil(ts.T(), data.ID)
	assert.Equal(ts.T(), "test2@example.com", data.Email)
}

func (ts *AdminTestSuite) TestAdminUserCreateWithDisabledEmailLogin() {
	var buffer bytes.Buffer
	require.NoError(ts.T(), json.NewEncoder(&buffer).Encode(map[string]interface{}{
		"email":    "test1@example.com",
		"password": "test1",
	}))

	// Setup request
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/users", &buffer)

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", ts.token))

	ts.Config.External.Email.Disabled = true

	ts.API.handler.ServeHTTP(w, req)
	require.Equal(ts.T(), http.StatusBadRequest, w.Code)
}
