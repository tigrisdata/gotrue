package conf

import (
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/pkgerrors"
)

type LoggingConfig struct {
	Level            string                 `mapstructure:"log_level" json:"log_level"  default:"debug"` // defaults to std log - developer friendly
	File             string                 `mapstructure:"log_file" json:"log_file"`
	DisableColors    bool                   `mapstructure:"disable_colors" split_words:"true" json:"disable_colors"`
	QuoteEmptyFields bool                   `mapstructure:"quote_empty_fields" split_words:"true" json:"quote_empty_fields"`
	TSFormat         string                 `mapstructure:"ts_format" json:"ts_format"`
	Fields           map[string]interface{} `mapstructure:"fields" json:"fields"`
	Format           string                 `mapstructure:"format" json:"format" default:"console"`
}

// trim full path. output in the form directory/file.go.
func consoleFormatCaller(i interface{}) string {
	var c string
	if cc, ok := i.(string); ok {
		c = cc
	}
	if len(c) > 0 {
		l := strings.Split(c, "/")
		if len(l) == 1 {
			return l[0]
		}
		return l[len(l)-2] + "/" + l[len(l)-1]
	}
	return c
}

func ConfigureZeroLogging(config *LoggingConfig) {
	zerolog.ErrorStackMarshaler = pkgerrors.MarshalStack
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	lvl, err := zerolog.ParseLevel(config.Level)
	if err != nil {
		log.Error().Err(err).Msg("error parsing log level. defaulting to info level")
		lvl = zerolog.InfoLevel
	}
	if config.Format == "console" {
		output := zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
		output.FormatCaller = consoleFormatCaller
		log.Logger = zerolog.New(output).Level(lvl).With().Timestamp().CallerWithSkipFrameCount(2).Stack().Logger()
	} else {
		log.Logger = zerolog.New(os.Stdout).Level(lvl).With().Timestamp().CallerWithSkipFrameCount(2).Stack().Logger()
	}
}
