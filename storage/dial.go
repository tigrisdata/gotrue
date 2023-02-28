package storage

import (
	"context"
	"time"

	_ "github.com/GoogleCloudPlatform/cloudsql-proxy/proxy/dialers/mysql"
	_ "github.com/go-sql-driver/mysql"
	"github.com/netlify/gotrue/conf"
	"github.com/sirupsen/logrus"
	tconf "github.com/tigrisdata/tigris-client-go/config"
	"github.com/tigrisdata/tigris-client-go/driver"
	"github.com/tigrisdata/tigris-client-go/tigris"
)

func Client(ctx context.Context, config *conf.GlobalConfiguration) (*tigris.Client, error) {
	logrus.Infof("creating tigris driver for url: %s project: %s", config.DB.URL, config.DB.Project)
	// ToDo: project creation is not needed here, this is to create the project in the local setup.

	var drv driver.Driver
	var err error
	for i := 0; i < 3; i++ {
		drv, err = driver.NewDriver(ctx, &tconf.Driver{
			ClientID:     config.DB.ClientId,
			ClientSecret: config.DB.ClientSecret,
			Branch:       config.DB.Branch,
			URL:          config.DB.URL,
		})
		if err != nil {
			logrus.WithError(err).Warn("Failed to create Tigris driver. Retrying")
			time.Sleep(5 * time.Second)
			continue
		}

		_, err := drv.Health(ctx)
		if err != nil {
			logrus.WithError(err).Warn("Failed to health check tigris. Retrying")
			time.Sleep(5 * time.Second)
			continue
		}
	}

	if err != nil {
		logrus.WithError(err).Error("Failed to construct Tigris driver")
		return nil, err
	}
	logrus.Infof("creating tigris driver successful for url: %s project: %s", config.DB.URL, config.DB.Project)

	_, err = drv.CreateProject(ctx, config.DB.Project)
	if err != nil && err.Error() != "project already exist" {
		logrus.Errorf("Failed to create tigris project: %+v", err)
		return nil, err
	}

	return tigris.NewClient(ctx, &tigris.Config{
		URL:          config.DB.URL,
		Project:      config.DB.Project,
		Branch:       config.DB.Branch,
		ClientID:     config.DB.ClientId,
		ClientSecret: config.DB.ClientSecret,
	})
}
