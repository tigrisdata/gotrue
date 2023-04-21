package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/netlify/gotrue/models"
	filter2 "github.com/tigrisdata/tigris-client-go/filter"
	"github.com/tigrisdata/tigris-client-go/tigris"
)

const (
	InvitationStatusPending  = "PENDING"
	InvitationStatusAccepted = "ACCEPTED"
)

type DeleteInvitationsParam struct {
	Email           string `json:"email"`
	CreatedBy       string `json:"created_by"`
	TigrisNamespace string `json:"tigris_namespace"`
	Status          string `json:"status"`
}

type VerifyInvitationParams struct {
	Email string `json:"email"`
	Code  string `json:"code"`
}

type VerifyInvitationResponse struct {
	TigrisNamespace     string `json:"tigris_namespace"`
	TigrisNamespaceName string `json:"tigris_namespace_name"`
	Role                string `json:"role"`
}

func (a *API) CreateInvitation(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	invitation := &models.Invitation{}
	jsonDecoder := json.NewDecoder(r.Body)
	err := jsonDecoder.Decode(invitation)
	if err != nil {
		return badRequestError("Could not read invitation params: %v", err).WithInternalError(err)
	}

	// prepare fields
	invitation.InstanceID = getInstanceID(ctx)
	invitation.Status = InvitationStatusPending
	invitation.Code = GenerateRandomString(a.config.InvitationConfig.CodePrefix, a.config.InvitationConfig.CodeLength)

	err = a.db.Tx(ctx, func(ctx context.Context) error {
		_, err = tigris.GetCollection[models.Invitation](a.db).Insert(ctx, invitation)
		if err != nil {
			return err
		}
		// send the invitation email
		mailer := a.Mailer(ctx)
		err = mailer.TigrisInviteMail(invitation.Email, invitation.CreatedByName, invitation.Code)
		if err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		return internalServerError("Could not create invitation").WithInternalError(err)
	}

	return sendJSON(w, http.StatusOK, invitation)
}

func (a *API) ListInvitations(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	namespaceFilter := r.URL.Query().Get("tigris_namespace")
	createdByFilter := r.URL.Query().Get("created_by")
	statusFilter := r.URL.Query().Get("status")

	if namespaceFilter == "" {
		return badRequestError("tigris_namespace must be specified in query parameter")
	}

	filter := filter2.Eq("tigris_namespace", namespaceFilter)

	if createdByFilter != "" {
		filter = filter2.And(filter, filter2.Eq("created_by", createdByFilter))
	}

	if statusFilter != "" {
		filter = filter2.And(filter, filter2.Eq("status", statusFilter))
	}

	itr, err := tigris.GetCollection[models.Invitation](a.db).Read(ctx, filter)
	if err != nil {
		return internalServerError("Failed to retrieve invitations").WithInternalError(err)
	}
	defer itr.Close()
	var invitations []models.Invitation
	var invitation models.Invitation
	for itr.Next(&invitation) {
		if a.config.InvitationConfig.HideCode {
			invitation.Code = "" // hide it
		}
		invitations = append(invitations, invitation)
	}
	return sendJSON(w, http.StatusOK, invitations)
}

func (a *API) DeleteInvitation(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()

	params := &DeleteInvitationsParam{}
	jsonDecoder := json.NewDecoder(r.Body)
	err := jsonDecoder.Decode(params)
	if err != nil {
		return badRequestError("Could not read DeleteInvitations params: %v", err)
	}

	if params.Email == "" {
		return badRequestError("email must be specified")
	}
	if params.Status == "" {
		params.Status = InvitationStatusPending
	}
	if params.CreatedBy == "" {
		return badRequestError("created_by must be specified")
	}
	if params.TigrisNamespace == "" {
		return badRequestError("tigris_namespace must be specified")
	}

	filter := filter2.Eq("tigris_namespace", params.TigrisNamespace)
	filter = filter2.And(filter, filter2.Eq("created_by", params.CreatedBy))
	filter = filter2.And(filter, filter2.Eq("status", strings.ToUpper(params.Status)))
	filter = filter2.And(filter, filter2.Eq("email", params.Email))
	filter = filter2.And(filter, filter2.Eq("instance_id", getInstanceID(ctx)))

	_, err = tigris.GetCollection[models.Invitation](a.db).Delete(ctx, filter)
	if err != nil {
		return internalServerError("Failed to delete user invitations").WithInternalError(err)
	}
	return sendJSON(w, http.StatusOK, nil)
}

func (a *API) VerifyInvitation(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	params := &VerifyInvitationParams{}
	jsonDecoder := json.NewDecoder(r.Body)
	err := jsonDecoder.Decode(params)
	if err != nil {
		return badRequestError("Could not read VerifyInvitation params: %v", err)
	}
	if params.Email == "" {
		return badRequestError("email must be specified")
	}
	if params.Code == "" {
		return badRequestError("code must be specified")
	}

	filter := filter2.Eq("email", params.Email)
	filter = filter2.And(filter, filter2.Eq("status", InvitationStatusPending))
	filter = filter2.And(filter, filter2.Eq("code", params.Code))
	filter = filter2.Eq("instance_id", getInstanceID(ctx))

	itr, err := tigris.GetCollection[models.Invitation](a.db).Read(ctx, filter)
	if err != nil {
		return internalServerError("Failed to verify user invitations").WithInternalError(err)
	}
	defer itr.Close()
	var invitation models.Invitation
	for itr.Next(&invitation) {
		if invitation.Code == params.Code && time.Now().UnixMilli() <= invitation.ExpirationTime && invitation.Status == InvitationStatusPending {
			// mark invitation as accepted
			invitation.Status = InvitationStatusAccepted
			_, err := tigris.GetCollection[models.Invitation](a.db).InsertOrReplace(ctx, &invitation)
			if err != nil {
				return internalServerError("Failed to verify invitation").WithInternalError(err).WithInternalMessage("Failed to update status on successful verification")
			}
			return sendJSON(w, http.StatusOK, VerifyInvitationResponse{TigrisNamespace: invitation.TigrisNamespace, TigrisNamespaceName: invitation.TigrisNamespaceName, Role: invitation.Role})
		}
	}
	return unauthorizedError("Could not validate the invitation code against email. Please check the code and expiration.")
}
