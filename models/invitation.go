package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/tigrisdata/gotrue/storage/namespace"
)

// Invitation tigris specific user invitation
type Invitation struct {
	InstanceID          uuid.UUID `json:"instance_id" db:"instance_id" tigris:"index"`
	ID                  uuid.UUID `json:"id" db:"id"  tigris:"primaryKey:1,autoGenerate"`
	Role                string    `json:"role" db:"role"`
	Email               string    `json:"email" db:"email" tigris:"index"`
	Code                string    `json:"code" db:"code"`
	TigrisNamespace     string    `json:"tigris_namespace" tigris:"index"`
	TigrisNamespaceName string    `json:"tigris_namespace_name"`
	CreatedBy           string    `json:"created_by"`
	CreatedByName       string    `json:"created_by_name"`
	Status              string    `json:"status" tigris:"index"`

	ExpirationTime int64 `json:"expiration_time"`

	AppMetaData *InvitationMetadata `json:"metadata" db:"metadata"`
	CreatedAt   *time.Time          `json:"created_at,omitempty" db:"created_at" tigris:"default:now(),createdAt"`
	UpdatedAt   *time.Time          `json:"updated_at,omitempty" db:"updated_at" tigris:"default:now(),updatedAt"`
}

type InvitationMetadata struct {
}

func (Invitation) TableName() string {
	tableName := "invitations"

	if namespace.GetNamespace() != "" {
		return namespace.GetNamespace() + "_" + tableName
	}

	return tableName
}
