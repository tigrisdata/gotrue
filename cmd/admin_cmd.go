package cmd

import (
	"context"

	"github.com/google/uuid"
	"github.com/tigrisdata/gotrue/conf"
	"github.com/tigrisdata/gotrue/crypto"
	"github.com/tigrisdata/gotrue/models"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/tigrisdata/tigris-client-go/fields"
	"github.com/tigrisdata/tigris-client-go/filter"
	"github.com/tigrisdata/tigris-client-go/tigris"
)

var autoconfirm, isSuperAdmin, isAdmin bool
var audience, instanceID string

func getAudience(c *conf.Configuration) string {
	if audience == "" {
		return c.JWT.Aud
	}

	return audience
}

func adminCmd() *cobra.Command {
	var adminCmd = &cobra.Command{
		Use: "admin",
	}

	adminCmd.AddCommand(&adminCreateUserCmd, &adminDeleteUserCmd)
	adminCmd.PersistentFlags().StringVarP(&audience, "aud", "a", "", "Set the new user's audience")
	adminCmd.PersistentFlags().StringVarP(&instanceID, "instance_id", "i", "", "Set the instance ID to interact with")

	adminCreateUserCmd.Flags().BoolVar(&autoconfirm, "confirm", false, "Automatically confirm user without sending an email")
	adminCreateUserCmd.Flags().BoolVar(&isSuperAdmin, "superadmin", false, "Create user with superadmin privileges")
	adminCreateUserCmd.Flags().BoolVar(&isAdmin, "admin", false, "Create user with admin privileges")

	return adminCmd
}

var adminCreateUserCmd = cobra.Command{
	Use: "createuser",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 2 {
			log.Fatal().Msg("Not enough arguments to createuser command. Expected at least email and password values")
			return
		}

		execWithConfigAndArgs(cmd, adminCreateUser, args)
	},
}

var adminDeleteUserCmd = cobra.Command{
	Use: "deleteuser",
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) < 1 {
			log.Fatal().Msg("Not enough arguments to deleteuser command. Expected at least ID or email")
			return
		}

		execWithConfigAndArgs(cmd, adminDeleteUser, args)
	},
}

var adminEditRoleCmd = cobra.Command{
	Use: "editrole",
	Run: func(cmd *cobra.Command, args []string) {
		execWithConfigAndArgs(cmd, adminEditRole, args)
	},
}

func adminCreateUser(globalConfig *conf.GlobalConfiguration, config *conf.Configuration, database *tigris.Database, args []string) {
	iid := uuid.Must(uuid.Parse(instanceID))

	aud := getAudience(config)
	if exists, err := models.IsDuplicatedEmail(context.TODO(), database, iid, args[0], aud); exists {
		log.Fatal().Msg("Error creating new user: user already exists")
	} else if err != nil {
		log.Fatal().Msgf("Error checking user email: %+v", err)
	}

	encrypter := &crypto.AESBlockEncrypter{
		Key: globalConfig.DB.EncryptionKey,
	}

	user, err := models.NewUser(iid, args[0], args[1], aud, nil, encrypter)
	if err != nil {
		log.Fatal().Msgf("Error creating user email: %+v", err)
	}
	user.IsSuperAdmin = isSuperAdmin

	ctx := context.TODO()
	err = database.Tx(ctx, func(ctx context.Context) error {
		var terr error

		if terr := user.BeforeCreate(); terr != nil {
			return terr
		}

		if _, terr = tigris.GetCollection[models.User](database).Insert(ctx, user); terr != nil {
			return terr
		}

		if len(args) > 2 {
			if terr = user.SetRole(context.TODO(), database, args[2]); terr != nil {
				return terr
			}
		} else if isAdmin {
			if terr = user.SetRole(context.TODO(), database, config.JWT.AdminGroupName); terr != nil {
				return terr
			}
		}

		if config.Mailer.Autoconfirm || autoconfirm {
			if terr = user.Confirm(ctx, database); terr != nil {
				return terr
			}
		}
		return nil
	})
	if err != nil {
		log.Fatal().Msgf("Unable to create user (%s): %+v", args[0], err)
	}
	log.Info().Msgf("Created user: %s", args[0])
}

func adminDeleteUser(globalConfig *conf.GlobalConfiguration, config *conf.Configuration, database *tigris.Database, args []string) {
	iid := uuid.Must(uuid.Parse(instanceID))

	user, err := models.FindUserByEmailAndAudience(context.TODO(), database, iid, args[0], getAudience(config))
	if err != nil {
		userID := uuid.Must(uuid.Parse(args[0]))
		user, err = models.FindUserByInstanceIDAndID(context.TODO(), database, iid, userID)
		if err != nil {
			log.Fatal().Msgf("Error finding user (%s): %+v", userID, err)
		}
	}

	if _, err = tigris.GetCollection[models.User](database).Delete(context.TODO(), filter.EqUUID("id", user.ID)); err != nil {
		log.Fatal().Msgf("Error removing user (%s): %+v", args[0], err)
	}

	log.Info().Msgf("Removed user: %s", args[0])
}

func adminEditRole(globalConfig *conf.GlobalConfiguration, config *conf.Configuration, database *tigris.Database, args []string) {
	iid := uuid.Must(uuid.Parse(instanceID))

	user, err := models.FindUserByEmailAndAudience(context.TODO(), database, iid, args[0], getAudience(config))
	if err != nil {
		userID := uuid.Must(uuid.Parse(args[0]))
		user, err = models.FindUserByInstanceIDAndID(context.TODO(), database, iid, userID)
		if err != nil {
			log.Fatal().Msgf("Error finding user (%s): %+v", userID, err)
		}
	}

	user.IsSuperAdmin = isSuperAdmin

	if len(args) > 0 {
		user.Role = args[0]
	} else if isAdmin {
		user.Role = config.JWT.AdminGroupName
	}

	fieldsToSet, err := fields.UpdateBuilder().Set("role", user.Role).Set("is_super_admin", user.IsSuperAdmin).Build()
	if err != nil {
		log.Fatal().Msgf("Error building fields for update (%s): %+v", args[0], err)
	}
	if _, err = tigris.GetCollection[models.User](database).Update(context.TODO(), filter.EqUUID("id", user.ID), fieldsToSet); err != nil {
		log.Fatal().Msgf("Error updating role for user (%s): %+v", args[0], err)
	}

	log.Info().Msgf("Updated user: %s", args[0])
}
