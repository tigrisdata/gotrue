package mailer

import (
	"fmt"
	"net/url"
	"regexp"

	"github.com/customerio/go-customerio"
	"github.com/tigrisdata/gotrue/conf"
	"github.com/tigrisdata/gotrue/models"
	"github.com/netlify/mailme"
	"github.com/sirupsen/logrus"
)

const (
	CustomerIOMailerType = "customerio"
	TemplateMailerType   = "template"
)

// Mailer defines the interface a mailer must implement.
type Mailer interface {
	Send(user *models.User, subject, body string, data map[string]interface{}) error
	// not used by tigris
	InviteMail(user *models.User, referrerURL string) error
	// used by tigris
	TigrisInviteMail(email string, invitedByName string, code string, invitedOrgCode string, invitedOrgName string, role string, expirationTime int64) error
	ConfirmationMail(user *models.User, referrerURL string) error
	RecoveryMail(user *models.User, referrerURL string) error
	EmailChangeMail(user *models.User, referrerURL string) error
	ValidateEmail(email string) error
}

// NewMailer returns a new gotrue mailer
func NewMailer(instanceConfig *conf.Configuration) Mailer {
	if instanceConfig.SMTP.Host == "" && instanceConfig.Mailer.Type == "" {
		return &noopMailer{}
	}

	if instanceConfig.Mailer.Type == TemplateMailerType {
		return &TemplateMailer{
			SiteURL: instanceConfig.SiteURL,
			Config:  instanceConfig,
			Mailer: &mailme.Mailer{
				Host:    instanceConfig.SMTP.Host,
				Port:    instanceConfig.SMTP.Port,
				User:    instanceConfig.SMTP.User,
				Pass:    instanceConfig.SMTP.Pass,
				From:    instanceConfig.SMTP.AdminEmail,
				BaseURL: instanceConfig.SiteURL,
				Logger:  logrus.New(),
			},
		}
	} else if instanceConfig.Mailer.Type == CustomerIOMailerType {
		if instanceConfig.Mailer.CustomerIO.ApiKey == "" {
			panic("API key is empty for customerio configuration")
		}
		client := customerio.NewAPIClient(instanceConfig.Mailer.CustomerIO.ApiKey)
		return &CustomerIOMailer{
			templateId: instanceConfig.Mailer.CustomerIO.UserInvitationTemplateId,
			client:     client,
			Config:     instanceConfig,
		}
	} else {
		panic(fmt.Sprintf("Unsupported mailer type: %s", instanceConfig.Mailer.Type))
	}
}

func withDefault(value, defaultValue string) string {
	if value == "" {
		return defaultValue
	}
	return value
}

func getSiteURL(referrerURL, siteURL, filepath, fragment string) (string, error) {
	baseURL := siteURL
	if filepath == "" && referrerURL != "" {
		baseURL = referrerURL
	}

	site, err := url.Parse(baseURL)
	if err != nil {
		return "", err
	}
	if filepath != "" {
		path, err := url.Parse(filepath)
		if err != nil {
			return "", err
		}
		site = site.ResolveReference(path)
	}
	site.Fragment = fragment
	return site.String(), nil
}

var urlRegexp = regexp.MustCompile(`^https?://[^/]+`)

func enforceRelativeURL(url string) string {
	return urlRegexp.ReplaceAllString(url, "")
}
