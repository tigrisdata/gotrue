package api

import (
	"fmt"
	"net/http"

	"github.com/tigrisdata/gotrue/conf"
)

// OpenIdConfiguration public rest endpoint
type OpenIdConfiguration struct {
	handler      http.Handler
	globalConfig *conf.GlobalConfiguration
	config       *conf.Configuration
	version      string
	response     map[string]interface{}
}

func NewOpenIdConfiguration(globalConfig *conf.GlobalConfiguration, conf *conf.Configuration, version string) OpenIdConfiguration {
	var info = make(map[string]interface{})
	info["issuer"] = conf.JWT.Issuer
	info["jwks_uri"] = fmt.Sprintf("%s/.well-known/jwks.json", conf.SiteURL)

	return OpenIdConfiguration{
		handler:      nil,
		globalConfig: globalConfig,
		config:       conf,
		version:      version,
		response:     info,
	}
}

// getConfiguration returns a public openid configuration information
func (o *OpenIdConfiguration) getConfiguration(w http.ResponseWriter, _ *http.Request) error {
	return sendJSON(w, http.StatusOK, o.response)
}
