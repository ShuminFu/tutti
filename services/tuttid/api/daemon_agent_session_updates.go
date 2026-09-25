package api

import (
	"context"

	tuttigenerated "github.com/tutti-os/tutti/services/tuttid/api/generated"
	"github.com/tutti-os/tutti/services/tuttid/apierrors"
)

func (api DaemonAPI) UpdateWorkspaceAgentSessionSettings(ctx context.Context, request tuttigenerated.UpdateWorkspaceAgentSessionSettingsRequestObject) (tuttigenerated.UpdateWorkspaceAgentSessionSettingsResponseObject, error) {
	if api.AgentSessionService == nil {
		return tuttigenerated.UpdateWorkspaceAgentSessionSettings503JSONResponse{
			ServiceUnavailableErrorJSONResponse: agentSessionServiceUnavailableError(),
		}, nil
	}
	if request.Body == nil {
		return tuttigenerated.UpdateWorkspaceAgentSessionSettings400JSONResponse{
			InvalidRequestErrorJSONResponse: invalidRequestError(apierrors.EmptyBody(apierrors.WithDeveloperMessage("empty body"))),
		}, nil
	}
	session, err := api.AgentSessionService.UpdateSettings(
		ctx,
		string(request.WorkspaceID),
		string(request.AgentSessionID),
		composerSettingsPatchFromGenerated(*request.Body),
	)
	if err != nil {
		return writeUpdateWorkspaceAgentSessionSettingsError(err), nil
	}
	generatedSession, err := generatedAgentSession(session)
	if err != nil {
		return writeUpdateWorkspaceAgentSessionSettingsError(err), nil
	}
	if !isRendererEngineCommandOrigin(
		request.Params.XTuttiAgentCommandOrigin,
	) {
		api.recordAgentStimulus(ctx, "session.settings.update", string(request.WorkspaceID), string(request.AgentSessionID), map[string]any{
			"settings": *request.Body,
		})
	}
	return tuttigenerated.UpdateWorkspaceAgentSessionSettings200JSONResponse{
		Session: generatedSession,
	}, nil
}

func (api DaemonAPI) UpdateWorkspaceAgentSessionArchive(ctx context.Context, request tuttigenerated.UpdateWorkspaceAgentSessionArchiveRequestObject) (tuttigenerated.UpdateWorkspaceAgentSessionArchiveResponseObject, error) {
	if api.AgentSessionService == nil {
		return tuttigenerated.UpdateWorkspaceAgentSessionArchive503JSONResponse{ServiceUnavailableErrorJSONResponse: agentSessionServiceUnavailableError()}, nil
	}
	if request.Body == nil {
		return tuttigenerated.UpdateWorkspaceAgentSessionArchive400JSONResponse{InvalidRequestErrorJSONResponse: invalidRequestError(apierrors.EmptyBody())}, nil
	}
	if request.Body.Archived == nil {
		return tuttigenerated.UpdateWorkspaceAgentSessionArchive400JSONResponse{InvalidRequestErrorJSONResponse: invalidRequestError(apierrors.InvalidRequest("archived_required"))}, nil
	}
	session, err := api.AgentSessionService.UpdateArchive(ctx, string(request.WorkspaceID), string(request.AgentSessionID), *request.Body.Archived)
	if err == nil {
		generated, projectionErr := generatedAgentSession(session)
		if projectionErr == nil {
			return tuttigenerated.UpdateWorkspaceAgentSessionArchive200JSONResponse{Session: generated}, nil
		}
		err = projectionErr
	}
	protocolErr := apierrors.Classify(err)
	switch protocolErr.Code {
	case tuttigenerated.WorkspaceNotFound:
		return tuttigenerated.UpdateWorkspaceAgentSessionArchive404JSONResponse{WorkspaceNotFoundErrorJSONResponse: workspaceNotFoundError(protocolErr)}, nil
	case tuttigenerated.InvalidRequest:
		return tuttigenerated.UpdateWorkspaceAgentSessionArchive400JSONResponse{InvalidRequestErrorJSONResponse: invalidRequestError(protocolErr)}, nil
	default:
		return tuttigenerated.UpdateWorkspaceAgentSessionArchive502JSONResponse{WorkspaceOperationErrorJSONResponse: workspaceOperationError(protocolErr)}, nil
	}
}

func (api DaemonAPI) RetryWorkspaceAgentSessionCodexDesktopHold(ctx context.Context, request tuttigenerated.RetryWorkspaceAgentSessionCodexDesktopHoldRequestObject) (tuttigenerated.RetryWorkspaceAgentSessionCodexDesktopHoldResponseObject, error) {
	if api.AgentSessionService == nil {
		return tuttigenerated.RetryWorkspaceAgentSessionCodexDesktopHold503JSONResponse{
			ServiceUnavailableErrorJSONResponse: agentSessionServiceUnavailableError(),
		}, nil
	}
	session, err := api.AgentSessionService.RetryCodexDesktopHold(
		ctx,
		string(request.WorkspaceID),
		string(request.AgentSessionID),
	)
	if err != nil {
		return writeRetryWorkspaceAgentSessionCodexDesktopHoldError(err), nil
	}
	generatedSession, err := generatedAgentSession(session)
	if err != nil {
		return writeRetryWorkspaceAgentSessionCodexDesktopHoldError(err), nil
	}
	return tuttigenerated.RetryWorkspaceAgentSessionCodexDesktopHold200JSONResponse{
		Session: generatedSession,
	}, nil
}

func (api DaemonAPI) UpdateWorkspaceAgentSessionPin(ctx context.Context, request tuttigenerated.UpdateWorkspaceAgentSessionPinRequestObject) (tuttigenerated.UpdateWorkspaceAgentSessionPinResponseObject, error) {
	if api.AgentSessionService == nil {
		return tuttigenerated.UpdateWorkspaceAgentSessionPin503JSONResponse{
			ServiceUnavailableErrorJSONResponse: agentSessionServiceUnavailableError(),
		}, nil
	}
	if request.Body == nil {
		return tuttigenerated.UpdateWorkspaceAgentSessionPin400JSONResponse{
			InvalidRequestErrorJSONResponse: invalidRequestError(apierrors.EmptyBody(apierrors.WithDeveloperMessage("empty body"))),
		}, nil
	}
	session, err := api.AgentSessionService.UpdatePin(
		ctx,
		string(request.WorkspaceID),
		string(request.AgentSessionID),
		request.Body.Pinned,
	)
	if err != nil {
		return writeUpdateWorkspaceAgentSessionPinError(err), nil
	}
	generatedSession, err := generatedAgentSession(session)
	if err != nil {
		return writeUpdateWorkspaceAgentSessionPinError(err), nil
	}
	return tuttigenerated.UpdateWorkspaceAgentSessionPin200JSONResponse{
		Session: generatedSession,
	}, nil
}

func (api DaemonAPI) UpdateWorkspaceAgentSessionTitle(ctx context.Context, request tuttigenerated.UpdateWorkspaceAgentSessionTitleRequestObject) (tuttigenerated.UpdateWorkspaceAgentSessionTitleResponseObject, error) {
	if api.AgentSessionService == nil {
		return tuttigenerated.UpdateWorkspaceAgentSessionTitle503JSONResponse{
			ServiceUnavailableErrorJSONResponse: agentSessionServiceUnavailableError(),
		}, nil
	}
	if request.Body == nil {
		return tuttigenerated.UpdateWorkspaceAgentSessionTitle400JSONResponse{
			InvalidRequestErrorJSONResponse: invalidRequestError(apierrors.EmptyBody(apierrors.WithDeveloperMessage("empty body"))),
		}, nil
	}
	session, err := api.AgentSessionService.UpdateTitle(
		ctx,
		string(request.WorkspaceID),
		string(request.AgentSessionID),
		request.Body.Title,
	)
	if err != nil {
		return writeUpdateWorkspaceAgentSessionTitleError(err), nil
	}
	generatedSession, err := generatedAgentSession(session)
	if err != nil {
		return writeUpdateWorkspaceAgentSessionTitleError(err), nil
	}
	return tuttigenerated.UpdateWorkspaceAgentSessionTitle200JSONResponse{
		Session: generatedSession,
	}, nil
}

func (api DaemonAPI) UpdateWorkspaceAgentSessionVisibility(ctx context.Context, request tuttigenerated.UpdateWorkspaceAgentSessionVisibilityRequestObject) (tuttigenerated.UpdateWorkspaceAgentSessionVisibilityResponseObject, error) {
	if api.AgentSessionService == nil {
		return tuttigenerated.UpdateWorkspaceAgentSessionVisibility503JSONResponse{
			ServiceUnavailableErrorJSONResponse: agentSessionServiceUnavailableError(),
		}, nil
	}
	if request.Body == nil {
		return tuttigenerated.UpdateWorkspaceAgentSessionVisibility400JSONResponse{
			InvalidRequestErrorJSONResponse: invalidRequestError(apierrors.EmptyBody(apierrors.WithDeveloperMessage("empty body"))),
		}, nil
	}
	session, err := api.AgentSessionService.UpdateVisible(
		ctx,
		string(request.WorkspaceID),
		string(request.AgentSessionID),
		request.Body.Visible,
	)
	if err != nil {
		return writeUpdateWorkspaceAgentSessionVisibilityError(err), nil
	}
	generatedSession, err := generatedAgentSession(session)
	if err != nil {
		return writeUpdateWorkspaceAgentSessionVisibilityError(err), nil
	}
	return tuttigenerated.UpdateWorkspaceAgentSessionVisibility200JSONResponse{
		Session: generatedSession,
	}, nil
}
