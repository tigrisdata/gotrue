package api

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/pprof"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"regexp"
	"syscall"
	"time"

	"github.com/didip/tollbooth/v5"
	"github.com/didip/tollbooth/v5/limiter"
	"github.com/go-chi/chi"
	"github.com/golang-jwt/jwt/v4"
	"github.com/google/uuid"
	lru "github.com/hashicorp/golang-lru"
	"github.com/imdario/mergo"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/tigrisdata/gotrue/conf"
	"github.com/tigrisdata/gotrue/crypto"
	"github.com/tigrisdata/gotrue/mailer"
	"github.com/tigrisdata/gotrue/models"
	"github.com/rs/cors"
	"github.com/rs/zerolog/log"
	"github.com/sebest/xff"
	"github.com/tigrisdata/tigris-client-go/tigris"
)

const (
	audHeaderName  = "X-JWT-AUD"
	defaultVersion = "unknown version"
)

var bearerRegexp = regexp.MustCompile(`^(?:B|b)earer (\S+$)`)

// API is the main REST API
type API struct {
	handler     http.Handler
	db          *tigris.Database
	encrypter   *crypto.AESBlockEncrypter
	config      *conf.GlobalConfiguration
	tokenSigner *TokenSigner
	version     string
	tokenCache  *lru.Cache
}

// TokenSigner is responsible to sign token, it supports HS256, RS256 algo
type TokenSigner struct {
	jwtConfig  *conf.JWTConfiguration
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
	kid        string
}

// NewTokenSigner - Returns new instance of TokenSinger
func NewTokenSigner(config *conf.Configuration) *TokenSigner {
	t := &TokenSigner{
		jwtConfig: &config.JWT,
	}
	t.init()
	return t
}

func (t *TokenSigner) init() {
	if t.jwtConfig.RSAPrivateKey == "" && t.jwtConfig.Algorithm == jwa.RS256.String() {
		log.Fatal().Msg("No RSA private key configured")
	}
	privateKeyData, err := os.ReadFile(t.jwtConfig.RSAPrivateKey)
	if err != nil {
		log.Fatal().Err(err)
	}

	block, _ := pem.Decode(privateKeyData)
	if block == nil {
		log.Fatal().Msg("block is null for configured rsa private key\"")
		return
	}

	privateKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		log.Fatal().Err(err)
		return
	}
	t.privateKey = privateKey
	// public key
	publicKeyData, e := os.ReadFile(t.jwtConfig.RSAPublicKeys[0])
	if e != nil {
		panic(e.Error())
	}
	publicKey, e := jwt.ParseRSAPublicKeyFromPEM(publicKeyData)
	if e != nil {
		panic(e.Error())
	}
	t.publicKey = publicKey
	t.kid, err = getKeyID(publicKey)
	if err != nil {
		panic(e.Error())
	}
}

// Signs the token with RSA algorithm
func (t *TokenSigner) signUsingRsa(token *jwt.Token) (string, error) {
	claims := token.Claims.(*GoTrueClaims)
	token.Header["kid"] = t.kid
	claims.Issuer = t.jwtConfig.Issuer
	return token.SignedString(t.privateKey)
}

// Signs the token with HMAC+SHA
func (t *TokenSigner) signUsingHmacWithSHA(token *jwt.Token) (string, error) {
	claims := token.Claims.(*GoTrueClaims)
	claims.Issuer = t.jwtConfig.Issuer
	return token.SignedString([]byte(t.jwtConfig.Secret))
}

// ListenAndServe starts the REST API
func (a *API) ListenAndServe(hostAndPort string) {
	server := &http.Server{
		Addr:    hostAndPort,
		Handler: a.handler,
	}

	done := make(chan struct{})
	defer close(done)
	go func() {
		waitForTermination(done)
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		server.Shutdown(ctx)
	}()

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal().Err(err).Msg("http server listen failed")
	}
}

// WaitForShutdown blocks until the system signals termination or done has a value
func waitForTermination(done <-chan struct{}) {
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	select {
	case sig := <-signals:
		log.Info().Msgf("Triggering shutdown from signal %s", sig)
	case <-done:
		log.Info().Msg("Shutting down...")
	}
}

// NewAPI instantiates a new REST API
func NewAPI(globalConfig *conf.GlobalConfiguration, config *conf.Configuration, db *tigris.Database) *API {
	return NewAPIWithVersion(context.Background(), globalConfig, config, db, defaultVersion)
}

// NewAPIWithVersion creates a new REST API using the specified version
func NewAPIWithVersion(ctx context.Context, globalConfig *conf.GlobalConfiguration, config *conf.Configuration, db *tigris.Database, version string) *API {
	cache, err := lru.New(globalConfig.API.TokenCacheSize)
	if err != nil {
		log.Fatal().Msgf("Couldn't construct token cache %v", err)
		return nil
	}
	api := &API{config: globalConfig, db: db, version: version, tokenSigner: NewTokenSigner(config), encrypter: &crypto.AESBlockEncrypter{Key: globalConfig.DB.EncryptionKey}, tokenCache: cache}
	jwks, err := NewJKWS(globalConfig, config, version)
	if err != nil {
		log.Fatal().Msgf("Couldn't construct JWKS %v", err)
		return nil
	}

	openidConf := NewOpenIdConfiguration(globalConfig, config, version)
	xffmw, _ := xff.Default()
	logger := newStructuredLogger(log.Logger)

	r := newRouter()
	r.UseBypass(xffmw.Handler)
	r.Use(addRequestID(globalConfig))
	r.Use(recoverer)
	r.UseBypass(tracer)
	if globalConfig.API.EnableDebugEndpoint {
		r.HandleFunc("/debug", pprof.Index)
		r.HandleFunc("/debug/pprof/", pprof.Index)
		r.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		r.HandleFunc("/debug/pprof/profile", pprof.Profile)
		r.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		r.Handle("/debug/pprof/goroutine", pprof.Handler("goroutine"))
		r.Handle("/debug/pprof/heap", pprof.Handler("heap"))
	}
	r.Get("/health", api.HealthCheck)

	r.Route("/callback", func(r *router) {
		r.UseBypass(logger)
		r.Use(api.loadOAuthState)

		if globalConfig.MultiInstanceMode {
			r.Use(api.loadInstanceConfig)
		}
		r.Get("/", api.ExternalProviderCallback)
	})

	r.Route("/", func(r *router) {
		r.UseBypass(logger)

		if globalConfig.MultiInstanceMode {
			r.Use(api.loadJWSSignatureHeader)
			r.Use(api.loadInstanceConfig)
		}

		r.Get("/settings", api.Settings)

		r.Get("/authorize", api.ExternalProviderRedirect)

		r.With(api.requireAdminCredentials).Post("/invite", api.Invite)
		r.Route("/invitations", func(r *router) {
			r.Get("/", api.ListInvitations)
			r.Delete("/", api.DeleteInvitation)
			r.Post("/", api.CreateInvitation)
			r.Post("/verify", api.VerifyInvitation)
		})

		r.With(api.requireEmailProvider).Post("/signup", api.Signup)
		r.With(api.requireEmailProvider).Post("/recover", api.Recover)
		r.With(api.requireEmailProvider).With(api.limitHandler(
			// Allow requests at a rate of 30 per 5 minutes.
			tollbooth.NewLimiter(30.0/(60*5), &limiter.ExpirableOptions{
				DefaultExpirationTTL: time.Hour,
			}).SetBurst(30),
		)).Post("/token", api.Token)
		r.Post("/verify", api.Verify)

		r.With(api.requireAuthentication).Post("/logout", api.Logout)

		r.Route("/user", func(r *router) {
			r.Use(api.requireAuthentication)
			r.Get("/", api.UserGet)
			r.Put("/", api.UserUpdate)
		})

		r.Route("/.well-known", func(r *router) {
			r.Get("/openid-configuration", openidConf.getConfiguration)
			r.Get("/jwks.json", jwks.getJWKS)
		})

		r.Route("/admin", func(r *router) {
			r.Use(api.requireAdminCredentials)

			r.Route("/audit", func(r *router) {
				r.Get("/", api.adminAuditLog)
			})

			r.Route("/users", func(r *router) {
				r.Get("/", api.adminUsers)
				r.With(api.requireEmailProvider).Post("/", api.adminUserCreate)

				r.Route("/{email}", func(r *router) {
					r.Use(api.loadUser)

					r.Get("/", api.adminUserGet)
					r.Put("/", api.adminUserUpdate)
					r.Delete("/", api.adminUserDelete)
				})
			})
		})

		r.Route("/saml", func(r *router) {
			r.Route("/acs", func(r *router) {
				r.Use(api.loadSAMLState)
				r.Post("/", api.ExternalProviderCallback)
			})

			r.Get("/metadata", api.SAMLMetadata)
		})
	})

	if globalConfig.MultiInstanceMode {
		// Operator microservice API
		r.WithBypass(logger).With(api.verifyOperatorRequest).Get("/", api.GetAppManifest)
		r.Route("/instances", func(r *router) {
			r.UseBypass(logger)
			r.Use(api.verifyOperatorRequest)

			r.Post("/", api.CreateInstance)
			r.Route("/{instance_id}", func(r *router) {
				r.Use(api.loadInstance)

				r.Get("/", api.GetInstance)
				r.Put("/", api.UpdateInstance)
				r.Delete("/", api.DeleteInstance)
			})
		})
	}

	corsHandler := cors.New(cors.Options{
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", audHeaderName, useCookieHeader},
		AllowCredentials: true,
	})

	api.handler = corsHandler.Handler(chi.ServerBaseContext(ctx, r))
	return api
}

// NewAPIFromConfigFile creates a new REST API using the provided configuration file.
func NewAPIFromConfigFile(filename string, version string) (*API, *conf.Configuration, error) {
	globalConfig, err := conf.LoadGlobal(filename)
	if err != nil {
		return nil, nil, err
	}

	config, err := conf.LoadConfig(filename)
	if err != nil {
		return nil, nil, err
	}

	ctx, err := WithInstanceConfig(context.Background(), config, uuid.Nil)
	if err != nil {
		log.Fatal().Msgf("Error loading instance config: %+v", err)
	}

	db, err := tigris.OpenDatabase(context.TODO(), nil, &models.AuditLogEntry{})
	if err != nil {
		log.Fatal().Msgf("Error opening database 2 : %+v", err)
	}

	return NewAPIWithVersion(ctx, globalConfig, config, db, version), config, nil
}

// HealthCheck ...
func (a *API) HealthCheck(w http.ResponseWriter, r *http.Request) error {
	return sendJSON(w, http.StatusOK, map[string]string{
		"version":     a.version,
		"name":        "GoTrue",
		"description": "GoTrue is a user registration and authentication API",
	})
}

// WithInstanceConfig ...
func WithInstanceConfig(ctx context.Context, config *conf.Configuration, instanceID uuid.UUID) (context.Context, error) {
	ctx = withConfig(ctx, config)
	ctx = withInstanceID(ctx, instanceID)
	return ctx, nil
}

// Mailer ...
func (a *API) Mailer(ctx context.Context) mailer.Mailer {
	config := a.getConfig(ctx)
	return mailer.NewMailer(config)
}

func (a *API) getConfig(ctx context.Context) *conf.Configuration {
	obj := ctx.Value(configKey)
	if obj == nil {
		return nil
	}

	config := obj.(*conf.Configuration)

	extConfig := (*a.config).External
	if err := mergo.MergeWithOverwrite(&extConfig, config.External); err != nil {
		return nil
	}
	config.External = extConfig

	smtpConfig := (*a.config).SMTP
	if err := mergo.MergeWithOverwrite(&smtpConfig, config.SMTP); err != nil {
		return nil
	}
	config.SMTP = smtpConfig

	return config
}
