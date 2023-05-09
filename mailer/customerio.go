package mailer

import (
	"context"
	"fmt"

	"github.com/customerio/go-customerio"
	"github.com/netlify/gotrue/conf"
	"github.com/netlify/gotrue/models"
	"github.com/rs/zerolog/log"
)

type CustomerIOMailer struct {
	templateId string
	client     *customerio.APIClient
	Config     *conf.Configuration
}

func (m CustomerIOMailer) ValidateEmail(email string) error {
	return nil
}

func (m *CustomerIOMailer) InviteMail(user *models.User, referrerURL string) error {
	return nil
}

func (m *CustomerIOMailer) TigrisInviteMail(email string, invitedByName string, code string, invitedOrgCode string, invitedOrgName string, role string, expirationTime int64) error {
	invitationURL := fmt.Sprintf("%s/invitation?code=%s&email=%s", m.Config.TigrisConsoleURL, code, email)
	request := customerio.SendEmailRequest{
		To:                     email,
		TransactionalMessageID: m.templateId,
		MessageData: map[string]interface{}{
			"invitation_sent_by_name": invitedByName,
			"invitation_url":          invitationURL,
			"invited_email":           email,
			"invited_org_code":        invitedOrgCode,
			"invited_org_name":        invitedOrgName,
			"expiration_time":         fmt.Sprintf("%d", expirationTime),
			"role":                    role,
		},
		Identifiers: map[string]string{
			"id": email,
		},
	}

	body, err := m.client.SendEmail(context.Background(), &request)
	if err != nil {
		log.Err(err).Msg("Failed to send invitation email")
		return err
	}

	log.Debug().Str("deliveryId", body.DeliveryID).Msg("Invitation email sent")
	return nil
}

func (m *CustomerIOMailer) ConfirmationMail(user *models.User, referrerURL string) error {
	return nil
}

func (m CustomerIOMailer) RecoveryMail(user *models.User, referrerURL string) error {
	return nil
}

func (m *CustomerIOMailer) EmailChangeMail(user *models.User, referrerURL string) error {
	return nil
}

func (m CustomerIOMailer) Send(user *models.User, subject, body string, data map[string]interface{}) error {
	return nil
}
