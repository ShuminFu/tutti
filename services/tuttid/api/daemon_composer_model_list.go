package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	tuttigenerated "github.com/tutti-os/tutti/services/tuttid/api/generated"
	"github.com/tutti-os/tutti/services/tuttid/apierrors"
	agentservice "github.com/tutti-os/tutti/services/tuttid/service/agent"
)

type composerModelListProber interface {
	OpenComposerModelDropdown(context.Context, agentservice.ComposerOptionsInput) (agentservice.ComposerOptions, error)
	RefreshComposerModelList(context.Context, agentservice.ComposerOptionsInput) (agentservice.ComposerOptions, error)
}

func (routes daemonRoutes) HandleComposerModelList(w http.ResponseWriter, r *http.Request) {
	handleComposerModelList(routes.api, w, r)
}

func handleComposerModelList(api DaemonAPI, w http.ResponseWriter, r *http.Request) {
	if api.AgentSessionService == nil {
		_ = (tuttigenerated.GetAgentProviderComposerOptions503JSONResponse{
			ServiceUnavailableErrorJSONResponse: agentSessionServiceUnavailableError(),
		}).VisitGetAgentProviderComposerOptionsResponse(w)
		return
	}
	prober, ok := api.AgentSessionService.(composerModelListProber)
	if !ok {
		http.Error(w, "composer model list refresh is unavailable", http.StatusNotImplemented)
		return
	}
	var body struct {
		tuttigenerated.GetAgentProviderComposerOptionsRequest
		Force *bool `json:"force,omitempty"`
	}
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil && !errors.Is(err, io.EOF) {
			_ = (tuttigenerated.GetAgentProviderComposerOptions400JSONResponse{
				InvalidRequestErrorJSONResponse: invalidRequestError(
					apierrors.MalformedRequest(apierrors.WithCause(err)),
				),
			}).VisitGetAgentProviderComposerOptionsResponse(w)
			return
		}
	}
	input := agentservice.ComposerOptionsInput{Provider: r.PathValue("provider")}
	input.AgentTargetID = optionalStringValue(body.AgentTargetId)
	input.Cwd = optionalStringValue(body.Cwd)
	input.WorkspaceID = optionalStringValue(body.WorkspaceId)
	if body.Settings != nil {
		input.Settings = composerSettingsFromGenerated(*body.Settings)
		input.CodexSaverMode = body.Settings.CodexSaverMode
	}
	if body.Locale != nil {
		input.Locale = string(*body.Locale)
	} else {
		input.Locale = api.composerDefaultLocale(r.Context())
	}
	var (
		options agentservice.ComposerOptions
		err     error
	)
	if body.Force != nil && *body.Force {
		options, err = prober.RefreshComposerModelList(r.Context(), input)
	} else {
		options, err = prober.OpenComposerModelDropdown(r.Context(), input)
	}
	if err != nil {
		_ = writeGetAgentProviderComposerOptionsError(err).VisitGetAgentProviderComposerOptionsResponse(w)
		return
	}
	_ = (tuttigenerated.GetAgentProviderComposerOptions200JSONResponse(
		generatedAgentProviderComposerOptions(options),
	)).VisitGetAgentProviderComposerOptionsResponse(w)
}
