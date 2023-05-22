package storage

import (
	"context"
	"time"

	_ "github.com/GoogleCloudPlatform/cloudsql-proxy/proxy/dialers/mysql"
	_ "github.com/go-sql-driver/mysql"
	"github.com/tigrisdata/gotrue/conf"
	"github.com/rs/zerolog/log"
	tconf "github.com/tigrisdata/tigris-client-go/config"
	"github.com/tigrisdata/tigris-client-go/driver"
	"github.com/tigrisdata/tigris-client-go/tigris"
)

func Client(ctx context.Context, config *conf.GlobalConfiguration) (*tigris.Client, error) {
	log.Info().Msgf("creating tigris driver for url: %s project: %s", config.DB.URL, config.DB.Project)
	// ToDo: project creation is not needed here, this is to create the project in the local setup.

	var drv driver.Driver
	var err error
	dbConfig := &tconf.Driver{
		Branch: config.DB.Branch,
		URL:    config.DB.URL,
	}
	if config.DB.Token != "" {
		dbConfig.Token = config.DB.Token
	} else {
		dbConfig.ClientID = config.DB.ClientId
		dbConfig.ClientSecret = config.DB.ClientSecret
	}
	for i := 0; i < 3; i++ {
		drv, err = driver.NewDriver(ctx, dbConfig)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to create Tigris driver. Retrying")
			time.Sleep(5 * time.Second)
			continue
		}

		_, err := drv.Health(ctx)
		if err != nil {
			log.Warn().Err(err).Msg("Failed to health check tigris. Retrying")
			time.Sleep(5 * time.Second)
			continue
		} else {
			break
		}
	}

	if err != nil {
		log.Err(err).Msg("Failed to construct Tigris driver")
		return nil, err
	}
	log.Info().Msgf("creating tigris driver successful for url: %s project: %s", config.DB.URL, config.DB.Project)

	_, err = drv.CreateProject(ctx, config.DB.Project)
	if err != nil && err.Error() != "project already exist" {
		log.Error().Msgf("Failed to create tigris project: %+v", err)
		return nil, err
	}
	// close the driver after creating project
	defer drv.Close()

	tigrisConfig := &tigris.Config{
		URL:      config.DB.URL,
		Project:  config.DB.Project,
		Branch:   config.DB.Branch,
		Protocol: driver.HTTP,
	}
	if config.DB.Token != "" {
		tigrisConfig.Token = config.DB.Token
	} else {
		tigrisConfig.ClientID = config.DB.ClientId
		tigrisConfig.ClientSecret = config.DB.ClientSecret
	}
	return tigris.NewClient(ctx, tigrisConfig)
}
