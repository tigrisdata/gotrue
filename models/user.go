package models

import (
	"context"
	"encoding/base64"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/netlify/gotrue/crypto"
	"github.com/netlify/gotrue/storage/namespace"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
	"github.com/tigrisdata/tigris-client-go/fields"
	"github.com/tigrisdata/tigris-client-go/filter"
	"github.com/tigrisdata/tigris-client-go/tigris"
)

const SystemUserID = "0"

var SystemUserUUID = uuid.Nil

type UserAppMetadata struct {
	TigrisNamespace string   `json:"tigris_namespace,omitempty"`
	TigrisProject   string   `json:"tigris_project,omitempty"`
	CreatedBy       string   `json:"created_by,omitempty"`
	Name            string   `json:"name,omitempty"`
	Description     string   `json:"description,omitempty"`
	Provider        string   `json:"provider,omitempty"`
	Roles           []string `json:"roles,omitempty"`
	Custom          JSONMap  `json:"custom,omitempty"`
}

// User represents a registered user with email/password authentication
type User struct {
	InstanceID        uuid.UUID `json:"instance_id" db:"instance_id" tigris:"index"`
	ID                uuid.UUID `json:"id" db:"id"  tigris:"primaryKey:1"`
	Aud               string    `json:"aud" db:"aud" tigris:"index"`
	Role              string    `json:"role" db:"role"`
	Email             string    `json:"email" db:"email" tigris:"primaryKey:2"`
	EncryptedPassword string    `json:"encrypted_password" db:"encrypted_password"`
	EncryptionIV      string    `json:"encryption_iv" db:"encryption_iv"`

	ConfirmedAt *time.Time `json:"confirmed_at,omitempty" db:"confirmed_at"`
	InvitedAt   *time.Time `json:"invited_at,omitempty" db:"invited_at"`

	ConfirmationToken  string     `json:"confirmation_token" db:"confirmation_token"`
	ConfirmationSentAt *time.Time `json:"confirmation_sent_at,omitempty" db:"confirmation_sent_at"`

	RecoveryToken  string     `json:"recovery_token" db:"recovery_token"`
	RecoverySentAt *time.Time `json:"recovery_sent_at,omitempty" db:"recovery_sent_at"`

	EmailChangeToken  string     `json:"email_change_token" db:"email_change_token"`
	EmailChange       string     `json:"new_email,omitempty" db:"email_change"`
	EmailChangeSentAt *time.Time `json:"email_change_sent_at,omitempty" db:"email_change_sent_at"`

	LastSignInAt *time.Time `json:"last_sign_in_at,omitempty" db:"last_sign_in_at"`

	AppMetaData  *UserAppMetadata `json:"app_metadata" db:"app_metadata"`
	UserMetaData JSONMap          `json:"user_metadata" db:"user_metadata"`

	IsSuperAdmin bool `json:"is_super_admin" db:"is_super_admin" tigris:"index"`

	CreatedAt *time.Time `json:"created_at,omitempty" db:"created_at" tigris:"default:now(),createdAt"`
	UpdatedAt *time.Time `json:"updated_at,omitempty" db:"updated_at" tigris:"default:now(),updatedAt"`
}

// NewUser initializes a new user from an email, password and user data.
func NewUser(instanceID uuid.UUID, email, password, aud string, userData map[string]interface{}, encrypter *crypto.AESBlockEncrypter) (*User, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return nil, errors.Wrap(err, "Error generating unique id")
	}
	var pw, iv string
	pw, iv = encrypter.Encrypt(password)

	user := &User{
		InstanceID:        instanceID,
		ID:                id,
		Aud:               aud,
		Email:             email,
		UserMetaData:      userData,
		EncryptedPassword: pw,
		EncryptionIV:      iv,
	}

	return user, nil
}

// NewUserWithAppData initializes a new user from an email, password and user data.
func NewUserWithAppData(instanceID uuid.UUID, email, password, aud string, userData map[string]interface{}, appData UserAppMetadata, encrypter *crypto.AESBlockEncrypter) (*User, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return nil, errors.Wrap(err, "Error generating unique id")
	}
	pw, iv := encrypter.Encrypt(password)
	user := &User{
		InstanceID:        instanceID,
		ID:                id,
		Aud:               aud,
		Email:             email,
		UserMetaData:      userData,
		AppMetaData:       &appData,
		EncryptedPassword: pw,
		EncryptionIV:      iv,
	}

	return user, nil
}

func NewSystemUser(instanceID uuid.UUID, aud string) *User {
	return &User{
		InstanceID:   instanceID,
		ID:           SystemUserUUID,
		Aud:          aud,
		IsSuperAdmin: true,
	}
}

func (User) TableName() string {
	tableName := "users"

	if namespace.GetNamespace() != "" {
		return namespace.GetNamespace() + "_" + tableName
	}

	return tableName
}

func (u *User) BeforeCreate() error {
	return u.BeforeUpdate()
}

func (u *User) BeforeUpdate() error {
	if u.ID == SystemUserUUID {
		return errors.New("Cannot persist system user")
	}

	return nil
}

func (u *User) BeforeSave() error {
	if u.ID == SystemUserUUID {
		return errors.New("Cannot persist system user")
	}

	if u.ConfirmedAt != nil && u.ConfirmedAt.IsZero() {
		u.ConfirmedAt = nil
	}
	if u.InvitedAt != nil && u.InvitedAt.IsZero() {
		u.InvitedAt = nil
	}
	if u.ConfirmationSentAt != nil && u.ConfirmationSentAt.IsZero() {
		u.ConfirmationSentAt = nil
	}
	if u.RecoverySentAt != nil && u.RecoverySentAt.IsZero() {
		u.RecoverySentAt = nil
	}
	if u.EmailChangeSentAt != nil && u.EmailChangeSentAt.IsZero() {
		u.EmailChangeSentAt = nil
	}
	if u.LastSignInAt != nil && u.LastSignInAt.IsZero() {
		u.LastSignInAt = nil
	}
	return nil
}

// IsConfirmed checks if a user has already being
// registered and confirmed.
func (u *User) IsConfirmed() bool {
	return u.ConfirmedAt != nil
}

// SetRole sets the users Role to roleName
func (u *User) SetRole(ctx context.Context, database *tigris.Database, roleName string) error {
	u.Role = strings.TrimSpace(roleName)

	_, err := tigris.GetCollection[User](database).Update(ctx, filter.Eq("id", u.ID.String()), fields.Set("role", u.Role))
	return err
}

// HasRole returns true when the users role is set to roleName
func (u *User) HasRole(roleName string) bool {
	return u.Role == roleName
}

// UpdateUserMetaData sets all user data from a map of updates,
// ensuring that it doesn't override attributes that are not
// in the provided map.
func (u *User) UpdateUserMetaData(ctx context.Context, database *tigris.Database, updates map[string]interface{}) error {
	if u.UserMetaData == nil {
		u.UserMetaData = updates
	} else if updates != nil {
		for key, value := range updates {
			if value != nil {
				u.UserMetaData[key] = value
			} else {
				delete(u.UserMetaData, key)
			}
		}
	}

	_, err := tigris.GetCollection[User](database).Update(ctx, filter.Eq("id", u.ID.String()), fields.Set("user_metadata", u.UserMetaData))
	return err
}

// UpdateAppMetaData updates all app data from a map of updates
func (u *User) UpdateAppMetaData(ctx context.Context, database *tigris.Database, updates *UserAppMetadata) error {

	if u.AppMetaData != nil {
		// custom process custom field
		for k, v := range updates.Custom {
			if v == nil {
				delete(u.AppMetaData.Custom, k)
			} else {
				if u.AppMetaData.Custom == nil {
					u.AppMetaData.Custom = make(map[string]interface{})
				}
				u.AppMetaData.Custom[k] = v
			}
		}
		u.AppMetaData.Roles = updates.Roles
		u.AppMetaData.Provider = updates.Provider
		u.AppMetaData.Name = updates.Name
		u.AppMetaData.Description = updates.Description
		u.AppMetaData.TigrisNamespace = updates.TigrisNamespace
		u.AppMetaData.TigrisProject = updates.TigrisProject
	} else {
		u.AppMetaData = updates
	}

	_, err := tigris.GetCollection[User](database).Update(ctx, filter.Eq("id", u.ID.String()), fields.Set("app_metadata", u.AppMetaData))
	return err
}

// PatchAppMetaData updates all app data from a map of updates, it leaves rest unset fields untouched.
func (u *User) PatchAppMetaData(ctx context.Context, database *tigris.Database, updates *UserAppMetadata) error {
	if u.AppMetaData == nil {
		u.AppMetaData = updates
	} else if updates != nil {
		if updates.Name != "" {
			u.AppMetaData.Name = updates.Name
		}
		if updates.Description != "" {
			u.AppMetaData.Description = updates.Description
		}
		if updates.Roles != nil {
			u.AppMetaData.Roles = updates.Roles
		}
		if updates.Custom != nil {
			u.AppMetaData.Custom = updates.Custom
		}
	}

	_, err := tigris.GetCollection[User](database).Update(ctx, filter.Eq("id", u.ID.String()), fields.Set("app_metadata", u.AppMetaData))
	return err
}

func (u *User) SetEmail(ctx context.Context, database *tigris.Database, email string) error {
	u.Email = email
	_, err := tigris.GetCollection[User](database).Update(ctx, filter.Eq("id", u.ID.String()), fields.Set("email", u.Email))
	return err
}

func (u *User) UpdatePassword(ctx context.Context, database *tigris.Database, encrypter *crypto.AESBlockEncrypter, password string) error {
	pw, iv := encrypter.Encrypt(password)
	u.EncryptedPassword = pw
	u.EncryptionIV = iv

	_, err := tigris.GetCollection[User](database).Update(ctx, filter.EqUUID("id", u.ID), fields.Set("encrypted_password", u.EncryptedPassword).Set("encryption_iv", u.EncryptionIV))
	return err
}

// Authenticate a user from a password
func (u *User) Authenticate(password string, encrypter *crypto.AESBlockEncrypter) bool {
	ivBytes, err := base64.StdEncoding.DecodeString(u.EncryptionIV)
	if err != nil {
		subLogger := log.With().Str("email", u.Email).Logger()
		subLogger.Error().Msg("Failed to retrieve existing IV for user")
		return false
	}
	encryptedPassword, _ := encrypter.EncryptWithIV(password, ivBytes)
	return u.EncryptedPassword == encryptedPassword

}

// Confirm resets the confimation token and the confirm timestamp
func (u *User) Confirm(ctx context.Context, database *tigris.Database) error {
	u.ConfirmationToken = ""
	now := time.Now()
	u.ConfirmedAt = &now

	fieldsToSet, err := fields.UpdateBuilder().
		Set("confirmation_token", u.ConfirmationToken).
		Set("confirmed_at", u.ConfirmedAt).Build()
	if err != nil {
		return err
	}
	_, err = tigris.GetCollection[User](database).Update(ctx, filter.EqUUID("id", u.ID), fieldsToSet)
	return err
}

// ConfirmEmailChange confirm the change of email for a user
func (u *User) ConfirmEmailChange(ctx context.Context, database *tigris.Database) error {
	fieldsToSet, err := fields.UpdateBuilder().
		Set("email", u.Email).
		Set("email_change", u.EmailChange).
		Set("email_change_token", u.EmailChangeToken).
		Build()
	if err != nil {
		return err
	}
	_, err = tigris.GetCollection[User](database).Update(ctx, filter.EqUUID("id", u.ID), fieldsToSet)
	return err
}

// Recover resets the recovery token
func (u *User) Recover(ctx context.Context, database *tigris.Database) error {
	_, err := tigris.GetCollection[User](database).Update(ctx, filter.EqUUID("id", u.ID), fields.Set("recovery_token", u.RecoveryToken))
	return err
}

// CountOtherUsers counts how many other users exist besides the one provided
func CountOtherUsers(ctx context.Context, database *tigris.Database, instanceID, id uuid.UUID) (int, error) {
	it, err := tigris.GetCollection[User](database).Read(ctx, filter.And(filter.EqUUID("instance_id", instanceID), filter.EqUUID("id", id)))
	if err != nil {
		return 0, errors.Wrap(err, "error finding registered users")
	}

	userCount := 0
	var user User
	for it.Next(&user) {
		userCount++
	}
	return userCount, nil
}

func findUser(ctx context.Context, database *tigris.Database, filter filter.Filter) (*User, error) {
	first, err := tigris.GetCollection[User](database).ReadOne(ctx, filter)
	if err != nil {
		return nil, err
	}
	if first == nil {
		return nil, &UserNotFoundError{}
	}

	return first, nil
}

// FindUserByConfirmationToken finds users with the matching confirmation token.
func FindUserByConfirmationToken(ctx context.Context, database *tigris.Database, token string) (*User, error) {
	return findUser(ctx, database, filter.Eq("confirmation_token", token))
}

// FindUserByEmailAndAudience finds a user with the matching email and audience.
func FindUserByEmailAndAudience(ctx context.Context, database *tigris.Database, instanceID uuid.UUID, email, aud string) (*User, error) {
	return findUser(ctx, database, filter.And(filter.EqUUID("instance_id", instanceID), filter.Eq("email", email), filter.Eq("aud", aud)))
}

// FindUserByIdAndAudience finds a user with the matching email and audience.
func FindUserByIdAndAudience(ctx context.Context, database *tigris.Database, instanceID, id uuid.UUID, aud string) (*User, error) {
	return findUser(ctx, database, filter.And(filter.EqUUID("instance_id", instanceID), filter.Eq("id", id), filter.Eq("aud", aud)))
}

// FindUserByID finds a user matching the provided ID.
func FindUserByID(ctx context.Context, database *tigris.Database, id uuid.UUID) (*User, error) {
	return findUser(ctx, database, filter.EqUUID("id", id))
}

// FindUserByInstanceIDAndID finds a user matching the provided ID.
func FindUserByInstanceIDAndID(ctx context.Context, database *tigris.Database, instanceID, id uuid.UUID) (*User, error) {
	return findUser(ctx, database, filter.And(filter.EqUUID("instance_id", instanceID), filter.EqUUID("id", id)))
}

// FindUserByInstanceIDAndEmail finds a user matching the provided ID.
func FindUserByInstanceIDAndEmail(ctx context.Context, database *tigris.Database, instanceID uuid.UUID, email string) (*User, error) {
	return findUser(ctx, database, filter.And(filter.EqUUID("instance_id", instanceID), filter.EqString("email", email)))
}

// FindUserByRecoveryToken finds a user with the matching recovery token.
func FindUserByRecoveryToken(ctx context.Context, database *tigris.Database, token string) (*User, error) {
	return findUser(ctx, database, filter.Eq("recovery_token", token))

}

// FindUserWithRefreshToken finds a user from the provided refresh token.
func FindUserWithRefreshToken(ctx context.Context, database *tigris.Database, token string) (*User, *RefreshToken, error) {
	c := tigris.GetCollection[RefreshToken](database)
	refreshToken := &RefreshToken{}
	var err error
	refreshToken, err = c.ReadOne(ctx, filter.Eq("token", token))
	if refreshToken == nil || err != nil {
		return nil, nil, RefreshTokenNotFoundError{}
	}

	user, err := findUser(ctx, database, filter.EqUUID("id", refreshToken.UserID))
	if err != nil {
		return nil, nil, err
	}

	return user, refreshToken, nil
}

// FindUsersInAudience finds users with the matching audience.
func FindUsersInAudience(ctx context.Context, database *tigris.Database, instanceID uuid.UUID, aud string, pageParams *Pagination, sortParams *SortParams, qfilter string, tigrisNamespace string, createdBy string, tigrisProject string, encrypter *crypto.AESBlockEncrypter) ([]*User, error) {
	//ToDo: sorting
	/**
	if sortParams != nil && len(sortParams.Fields) > 0 {
		for _, field := range sortParams.Fields {
			q = q.Order(field.Name + " " + string(field.Dir))
		}
	}*/

	// ToDo: pagination
	/**
	var err error
	if pageParams != nil {
		err = q.Paginate(int(pageParams.Page), int(pageParams.PerPage)).All(&users)
		pageParams.Count = uint64(q.Paginator.TotalEntriesSize)
	} else {
		err = q.All(&users)
	}*/

	listUsersFilter := filter.Eq("aud", aud)
	listUsersFilter = filter.And(listUsersFilter, filter.Eq("instance_id", instanceID.String()))

	if tigrisNamespace != "" {
		listUsersFilter = filter.And(listUsersFilter, filter.Eq("app_metadata.tigris_namespace", tigrisNamespace))
	}
	if createdBy != "" {
		listUsersFilter = filter.And(listUsersFilter, filter.Eq("app_metadata.created_by", createdBy))
	}
	it, err := tigris.GetCollection[User](database).Read(ctx, listUsersFilter)
	if err != nil {
		return nil, errors.Wrap(err, "reading user failed")
	}

	defer it.Close()
	qfilter = strings.ToLower(qfilter)
	var users []*User
	var user User
	for it.Next(&user) {
		u := user
		if u.AppMetaData == nil || u.AppMetaData.TigrisProject != tigrisProject {
			continue
		}
		// either the project field doesn't exist - this is required for backward compatibility
		// or it has to match the requested project name
		u.EncryptedPassword = encrypter.Decrypt(u.EncryptedPassword, u.EncryptionIV)
		if qfilter != "" {
			if len(u.Email) > 0 && strings.Contains(strings.ToLower(u.Email), qfilter) {
				users = append(users, &u)
			} else if u.UserMetaData != nil {
				fullName := u.UserMetaData["full_name"]
				if conv, ok := fullName.(string); ok && len(conv) > 0 && strings.Contains(strings.ToLower(conv), qfilter) {
					users = append(users, &u)
				}
			}
		} else {
			users = append(users, &u)
		}
	}
	// return empty array instead of null JSON value
	if users == nil {
		users = make([]*User, 0)
	}
	return users, err
}

// IsDuplicatedEmail returns whether a user exists with a matching email and audience.
func IsDuplicatedEmail(ctx context.Context, database *tigris.Database, instanceID uuid.UUID, email, aud string) (bool, error) {
	_, err := FindUserByEmailAndAudience(ctx, database, instanceID, email, aud)
	if err != nil {
		if IsNotFoundError(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
