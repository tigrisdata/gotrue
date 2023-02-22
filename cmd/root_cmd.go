package cmd

import (
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	tconf "github.com/tigrisdata/tigris-client-go/config"
	"github.com/tigrisdata/tigris-client-go/driver"

	"context"

	"github.com/netlify/gotrue/conf"
	"github.com/netlify/gotrue/models"
	"github.com/netlify/gotrue/storage"
	"github.com/tigrisdata/tigris-client-go/tigris"
)

var configFile = ""

var rootCmd = cobra.Command{
	Use: "gotrue",
	Run: func(cmd *cobra.Command, args []string) {
		execWithConfig(cmd, serve)
	},
}

// RootCommand will setup and return the root command
func RootCommand() *cobra.Command {
	rootCmd.AddCommand(&serveCmd, &multiCmd, &versionCmd, adminCmd())
	rootCmd.PersistentFlags().StringVarP(&configFile, "config", "c", "", "the config file to use")

	return &rootCmd
}

func execWithConfig(cmd *cobra.Command, fn func(globalConfig *conf.GlobalConfiguration, config *conf.Configuration, database *tigris.Database)) {
	globalConfig, err := conf.LoadGlobal(configFile)
	if err != nil {
		logrus.Fatalf("Failed to load configuration: %+v", err)
	}
	config, err := conf.LoadConfig(configFile)
	if err != nil {
		logrus.Fatalf("Failed to load configuration: %+v", err)
	}

	db := bootstrapSchemas(context.TODO(), globalConfig)

	fn(globalConfig, config, db)
}

func execWithConfigAndArgs(cmd *cobra.Command, fn func(globalConfig *conf.GlobalConfiguration, config *conf.Configuration, database *tigris.Database, args []string), args []string) {
	globalConfig, err := conf.LoadGlobal(configFile)
	if err != nil {
		logrus.Fatalf("Failed to load configuration: %+v", err)
	}
	config, err := conf.LoadConfig(configFile)
	if err != nil {
		logrus.Fatalf("Failed to load configuration: %+v", err)
	}

	db := bootstrapSchemas(context.TODO(), globalConfig)

	fn(globalConfig, config, db, args)
}

func bootstrapSchemas(ctx context.Context, globalConfig *conf.GlobalConfiguration) *tigris.Database {
	tigrisClient, err := storage.Client(ctx, globalConfig)
	if err != nil {
		logrus.Fatalf("Failed to create tigris client: %+v", err)
	}

	drv, err := driver.NewDriver(context.TODO(), &tconf.Driver{
		ClientID:     globalConfig.DB.ClientId,
		ClientSecret: globalConfig.DB.ClientSecret,
		Branch:       globalConfig.DB.Branch,
		URL:          globalConfig.DB.URL,
	})
	if err != nil {
		logrus.Errorf("Failed to create tigris client: %+v", err)
	}
	_, err = drv.CreateProject(context.TODO(), globalConfig.DB.Project)
	if err != nil {
		logrus.Errorf("Failed to create tigris project: %+v", err)
	}
	db, err := tigrisClient.OpenDatabase(ctx, &models.AuditLogEntry{}, &models.User{}, &models.RefreshToken{}, &models.Instance{})
	if err != nil {
		logrus.Fatalf("Error opening database: %+v", err)
	}

	return db
}
