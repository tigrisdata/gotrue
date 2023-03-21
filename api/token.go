package api

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	jwt "github.com/golang-jwt/jwt/v4"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/netlify/gotrue/conf"
	"github.com/netlify/gotrue/metering"
	"github.com/netlify/gotrue/models"
	"github.com/rs/zerolog/log"
)

// GoTrueClaims is a struct that used for JWT claims
type GoTrueClaims struct {
	jwt.StandardClaims
	TigrisMetadata map[string]interface{} `json:"https://tigris"`
}

// AccessTokenResponse represents an OAuth2 success response
type AccessTokenResponse struct {
	Token        string `json:"access_token"`
	TokenType    string `json:"token_type"` // Bearer
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

const useCookieHeader = "x-use-cookie"
const useSessionCookie = "session"

// Token is the endpoint for OAuth access token requests
func (a *API) Token(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	grantType := r.FormValue("grant_type")

	switch grantType {
	case "password":
		return a.ResourceOwnerPasswordGrant(ctx, w, r)
	case "refresh_token":
		return a.RefreshTokenGrant(ctx, w, r)
	default:
		return oauthError("unsupported_grant_type", "")
	}
}

// ResourceOwnerPasswordGrant implements the password grant type flow
func (a *API) ResourceOwnerPasswordGrant(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	username := r.FormValue("username")
	password := r.FormValue("password")
	cookie := r.Header.Get(useCookieHeader)

	aud := a.requestAud(ctx, r)
	instanceID := getInstanceID(ctx)
	config := a.getConfig(ctx)

	user, err := models.FindUserByEmailAndAudience(r.Context(), a.db, instanceID, username, aud)
	if err != nil {
		if models.IsNotFoundError(err) {
			log.Warn().Str("email", username).Msg("No user found with that email, or password invalid.")
			return oauthError("invalid_grant", "No user found with that email, or password invalid.")
		}
		return internalServerError("Database error finding user").WithInternalError(err)
	}

	if !user.IsConfirmed() {
		return oauthError("invalid_grant", "Email not confirmed")
	}

	if !user.Authenticate(password, a.encrypter) {
		log.Warn().Str("email", username).Msg("No user found with that email, or password invalid: Auth failure")
		return oauthError("invalid_grant", "No user found with that email, or password invalid.")
	}

	if a.config.API.EnableTokenCache && a.tokenCache.Contains(user.Email) {
		cachedValue, contains := a.tokenCache.Get(user.Email)
		if contains {
			cachedAccessToken, ok := cachedValue.(*AccessTokenResponse)
			if ok {
				// parse token and check expiry
				cachedTokenPayload := strings.Split(cachedAccessToken.Token, ".")[1]
				exp := getExpiry(cachedTokenPayload)
				// if expiry is within an hour then evict the token and issue new one.
				if time.Now().Unix()+3600 >= exp {
					a.tokenCache.Remove(user.Email)
				} else {
					// update expiresIn seconds
					cachedAccessToken.ExpiresIn = int(exp - time.Now().Unix())
					metering.RecordLogin("password", user.ID, instanceID)
					return sendJSON(w, http.StatusOK, cachedAccessToken)
				}
			}
		}
	}

	var token *AccessTokenResponse
	err = a.db.Tx(ctx, func(ctx context.Context) error {
		var terr error
		if terr = models.NewAuditLogEntry(ctx, a.db, instanceID, user, models.LoginAction, nil); terr != nil {
			return terr
		}
		if terr = triggerEventHooks(ctx, a.db, LoginEvent, user, instanceID, config); terr != nil {
			return terr
		}

		token, terr = a.issueRefreshToken(ctx, user)
		if terr != nil {
			return terr
		}

		if cookie != "" && config.Cookie.Duration > 0 {
			if terr = a.setCookieToken(config, token.Token, cookie == useSessionCookie, w); terr != nil {
				return internalServerError("Failed to set JWT cookie. %s", terr)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	// process cache
	_ = a.tokenCache.Add(user.Email, token)
	metering.RecordLogin("password", user.ID, instanceID)
	return sendJSON(w, http.StatusOK, token)
}

// RefreshTokenGrant implements the refresh_token grant type flow
func (a *API) RefreshTokenGrant(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	config := a.getConfig(ctx)
	instanceID := getInstanceID(ctx)
	tokenStr := r.FormValue("refresh_token")
	cookie := r.Header.Get(useCookieHeader)

	if tokenStr == "" {
		return oauthError("invalid_request", "refresh_token required")
	}

	user, token, err := models.FindUserWithRefreshToken(r.Context(), a.db, tokenStr)
	if err != nil {
		if models.IsNotFoundError(err) {
			return oauthError("invalid_grant", "Invalid Refresh Token")
		}
		return internalServerError(err.Error())
	}

	if token.Revoked {
		a.clearCookieToken(ctx, w)
		return oauthError("invalid_grant", "Invalid Refresh Token").WithInternalMessage("Possible abuse attempt: %v", r)
	}

	var tokenString string
	var newToken *models.RefreshToken

	err = a.db.Tx(ctx, func(ctx context.Context) error {
		var terr error
		if terr = models.NewAuditLogEntry(ctx, a.db, instanceID, user, models.TokenRefreshedAction, nil); terr != nil {
			return terr
		}

		newToken, terr = models.GrantRefreshTokenSwap(ctx, a.db, user, token)
		if terr != nil {
			return internalServerError(terr.Error())
		}

		tokenString, terr = generateAccessToken(user, time.Second*time.Duration(config.JWT.Exp), a.getConfig(ctx), a.tokenSigner)
		if terr != nil {
			return internalServerError("error generating jwt token").WithInternalError(terr)
		}

		if cookie != "" && config.Cookie.Duration > 0 {
			if terr = a.setCookieToken(config, tokenString, cookie == useSessionCookie, w); terr != nil {
				return internalServerError("Failed to set JWT cookie. %s", terr)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	metering.RecordLogin("token", user.ID, instanceID)
	return sendJSON(w, http.StatusOK, &AccessTokenResponse{
		Token:        tokenString,
		TokenType:    "bearer",
		ExpiresIn:    config.JWT.Exp,
		RefreshToken: newToken.Token,
	})
}

func generateAccessToken(user *models.User, expiresIn time.Duration, config *conf.Configuration, tokenSigner *TokenSigner) (string, error) {
	var tigrisClaims = make(map[string]interface{})
	// superadmin doesn't have app metadata
	if user.AppMetaData != nil {
		tigrisClaims = map[string]interface{}{
			"nc": user.AppMetaData.TigrisNamespace,
			"p":  user.AppMetaData.TigrisProject,
		}
	}
	claims := &GoTrueClaims{
		StandardClaims: jwt.StandardClaims{
			Subject:   "gt|" + user.ID.String(), // customize sub b
			Audience:  user.Aud,
			Issuer:    fmt.Sprintf("http://%s", config.SiteURL),
			IssuedAt:  time.Now().Unix(),
			ExpiresAt: time.Now().Add(expiresIn).Unix(),
		},
		TigrisMetadata: tigrisClaims,
	}

	switch config.JWT.Algorithm {
	case jwa.RS256.String():
		token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
		return tokenSigner.signUsingRsa(token)
	default:
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		return tokenSigner.signUsingHmacWithSHA(token)
	}
}

func (a *API) issueRefreshToken(ctx context.Context, user *models.User) (*AccessTokenResponse, error) {
	config := a.getConfig(ctx)

	now := time.Now()
	user.LastSignInAt = &now

	var tokenString string
	var refreshToken *models.RefreshToken

	err := a.db.Tx(ctx, func(ctx context.Context) error {
		var terr error
		refreshToken, terr = models.GrantAuthenticatedUser(ctx, a.db, user)
		if terr != nil {
			return internalServerError("Database error granting user").WithInternalError(terr)
		}

		config := a.getConfig(ctx)
		tokenSigner := NewTokenSigner(config)

		tokenString, terr = generateAccessToken(user, time.Second*time.Duration(config.JWT.Exp), config, tokenSigner)
		if terr != nil {
			return internalServerError("error generating jwt token").WithInternalError(terr)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &AccessTokenResponse{
		Token:        tokenString,
		TokenType:    "bearer",
		ExpiresIn:    config.JWT.Exp,
		RefreshToken: refreshToken.Token,
	}, nil
}

func (a *API) setCookieToken(config *conf.Configuration, tokenString string, session bool, w http.ResponseWriter) error {
	exp := time.Second * time.Duration(config.Cookie.Duration)
	cookie := &http.Cookie{
		Name:     config.Cookie.Key,
		Value:    tokenString,
		Secure:   true,
		HttpOnly: true,
		Path:     "/",
	}
	if !session {
		cookie.Expires = time.Now().Add(exp)
		cookie.MaxAge = config.Cookie.Duration
	}

	http.SetCookie(w, cookie)
	return nil
}

func (a *API) clearCookieToken(ctx context.Context, w http.ResponseWriter) {
	config := getConfig(ctx)
	http.SetCookie(w, &http.Cookie{
		Name:     config.Cookie.Key,
		Value:    "",
		Expires:  time.Now().Add(-1 * time.Hour * 10),
		MaxAge:   -1,
		Secure:   true,
		HttpOnly: true,
		Path:     "/",
	})
}

func getExpiry(tokenPayload string) int64 {
	jsonString, _ := base64.RawStdEncoding.DecodeString(tokenPayload)
	var payload map[string]interface{}
	err := json.Unmarshal(jsonString, &payload)
	if err != nil {
		log.Warn().Err(err).Msg("Failed to parse expiry from cached token - disabling cache")
		return 0
	}
	return int64(payload["exp"].(float64))
}
