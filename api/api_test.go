package api

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/tigrisdata/gotrue/conf"
	"github.com/tigrisdata/gotrue/models"
	"github.com/tigrisdata/gotrue/storage/test"
	"github.com/stretchr/testify/require"
	"github.com/tigrisdata/tigris-client-go/tigris"
)

const (
	apiTestVersion = "1"
	apiTestConfig  = "../hack/test.env"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// setupAPIForTest creates a new API to run tests with.
// Using this function allows us to keep track of the database connection
// and cleaning up data between tests.
func setupAPIForTest() (*API, *conf.Configuration, *conf.GlobalConfiguration, error) {
	return setupAPIForTestWithCallback(nil)
}

func setupAPIForMultiinstanceTest() (*API, *conf.Configuration, *conf.GlobalConfiguration, error) {
	cb := func(gc *conf.GlobalConfiguration, c *conf.Configuration, database *tigris.Database) (uuid.UUID, error) {
		gc.MultiInstanceMode = true
		return uuid.Nil, nil
	}

	return setupAPIForTestWithCallback(cb)
}

func setupAPIForTestForInstance() (*API, *conf.Configuration, *conf.GlobalConfiguration, uuid.UUID, error) {
	instanceID := uuid.Must(uuid.NewRandom())
	cb := func(gc *conf.GlobalConfiguration, c *conf.Configuration, database *tigris.Database) (uuid.UUID, error) {
		_, err := tigris.GetCollection[models.Instance](database).Insert(context.TODO(), &models.Instance{
			ID:         instanceID,
			UUID:       testUUID,
			BaseConfig: c,
		})
		return instanceID, err
	}

	api, conf, globalConf, err := setupAPIForTestWithCallback(cb)
	if err != nil {
		return nil, nil, nil, uuid.Nil, err
	}
	return api, conf, globalConf, instanceID, nil
}

func setupAPIForTestWithCallback(cb func(*conf.GlobalConfiguration, *conf.Configuration, *tigris.Database) (uuid.UUID, error)) (*API, *conf.Configuration, *conf.GlobalConfiguration, error) {
	globalConfig, err := conf.LoadGlobal(apiTestConfig)
	if err != nil {
		return nil, nil, nil, err
	}

	tigrisClient, err := test.SetupDBConnection(globalConfig)
	if err != nil {
		return nil, nil, nil, err
	}

	config, err := conf.LoadConfig(apiTestConfig)
	if err != nil {
		tigrisClient.Close()
		return nil, nil, nil, err
	}

	database, err := tigrisClient.OpenDatabase(context.TODO(), &models.AuditLogEntry{}, &models.User{}, &models.RefreshToken{}, &models.Instance{}, &models.Invitation{})
	if err != nil {
		tigrisClient.Close()
		return nil, nil, nil, err
	}

	instanceID := uuid.Nil
	if cb != nil {
		instanceID, err = cb(globalConfig, config, database)
		if err != nil {
			tigrisClient.Close()
			return nil, nil, nil, err
		}
	}

	ctx, err := WithInstanceConfig(context.Background(), config, instanceID)
	if err != nil {
		tigrisClient.Close()
		return nil, nil, nil, err
	}

	return NewAPIWithVersion(ctx, globalConfig, config, database, apiTestVersion), config, globalConfig, nil
}

func TestEmailEnabledByDefault(t *testing.T) {
	api, _, _, err := setupAPIForTest()
	require.NoError(t, err)

	require.False(t, api.config.External.Email.Disabled)
}

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func randStringBytes(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = letterBytes[rand.Int63()%int64(len(letterBytes))]
	}
	return string(b)
}
