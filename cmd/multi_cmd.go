package cmd

import (
	"context"
	"fmt"

	"github.com/tigrisdata/gotrue/api"
	"github.com/tigrisdata/gotrue/conf"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
)

var multiCmd = cobra.Command{
	Use:  "multi",
	Long: "Start multi-tenant API server",
	Run:  multi,
}

func multi(cmd *cobra.Command, args []string) {
	globalConfig, err := conf.LoadGlobal(configFile)
	if err != nil {
		log.Fatal().Msgf("Failed to load configuration: %+v", err)
	}
	if globalConfig.OperatorToken == "" {
		log.Fatal().Msg("Operator token secret is required")
	}

	config, err := conf.LoadConfig(configFile)
	if err != nil {
		log.Fatal().Msg("couldn't load config")
	}

	globalConfig.MultiInstanceMode = true
	api := api.NewAPIWithVersion(context.Background(), globalConfig, config, bootstrapSchemas(context.TODO(), globalConfig), Version)

	l := fmt.Sprintf("%v:%v", globalConfig.API.Host, globalConfig.API.Port)
	log.Info().Msgf("GoTrue API started on: %s", l)
	api.ListenAndServe(l)
}
