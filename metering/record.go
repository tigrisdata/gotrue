package metering

import (
	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

var logger = log.With().Bool("metering", true).Logger()

func RecordLogin(loginType string, userID, instanceID uuid.UUID) {
	recorderLogger := logger.With().
		Str("action", "login").
		Str("login_method", loginType).
		Str("instance_id", instanceID.String()).
		Str("user_id", userID.String()).Logger()
	recorderLogger.Info().Msgf("Login")
}
