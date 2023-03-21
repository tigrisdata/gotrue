package cmd

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/netlify/gotrue/api"
	"github.com/netlify/gotrue/conf"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/tigrisdata/tigris-client-go/tigris"
)

var serveCmd = cobra.Command{
	Use:  "serve",
	Long: "Start API server",
	Run: func(cmd *cobra.Command, args []string) {
		execWithConfig(cmd, serve)
	},
}

func serve(globalConfig *conf.GlobalConfiguration, config *conf.Configuration, database *tigris.Database) {
	ctx, err := api.WithInstanceConfig(context.Background(), config, uuid.Nil)
	if err != nil {
		log.Fatal().Msgf("Error loading instance config: %+v", err)
	}
	api := api.NewAPIWithVersion(ctx, globalConfig, config, database, Version)

	l := fmt.Sprintf("%v:%v", globalConfig.API.Host, globalConfig.API.Port)
	log.Info().Msgf("GoTrue API started on: %s", l)
	api.ListenAndServe(l)
}
