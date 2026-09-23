package api

import (
	"context"
	"strings"

	tuttigenerated "github.com/tutti-os/tutti/services/tuttid/api/generated"
	"github.com/tutti-os/tutti/services/tuttid/apierrors"
	"github.com/tutti-os/tutti/services/tuttid/service/agent"
)

func (api DaemonAPI) PreviewCursorSkillImport(_ context.Context, request tuttigenerated.PreviewCursorSkillImportRequestObject) (tuttigenerated.PreviewCursorSkillImportResponseObject, error) {
	if request.Body == nil || strings.TrimSpace(request.Body.SourceDir) == "" {
		return tuttigenerated.PreviewCursorSkillImport400JSONResponse{InvalidRequestErrorJSONResponse: cursorSkillImportInvalidRequest("select a .cursor directory or its skills child")}, nil
	}
	preview, err := agent.PreviewCursorSkills(request.Body.SourceDir)
	if err != nil {
		return tuttigenerated.PreviewCursorSkillImport400JSONResponse{InvalidRequestErrorJSONResponse: cursorSkillImportInvalidRequest(err.Error())}, nil
	}
	entries := make([]tuttigenerated.CursorSkillImportEntry, 0, len(preview.Skills))
	for _, item := range preview.Skills {
		entries = append(entries, tuttigenerated.CursorSkillImportEntry{
			Name: item.Name, Status: tuttigenerated.CursorSkillImportEntryStatus(item.Status), Reason: optionalCursorImportReason(item.Reason),
		})
	}
	return tuttigenerated.PreviewCursorSkillImport200JSONResponse{Destination: preview.Destination, Skills: entries}, nil
}

func (api DaemonAPI) ImportCursorSkills(_ context.Context, request tuttigenerated.ImportCursorSkillsRequestObject) (tuttigenerated.ImportCursorSkillsResponseObject, error) {
	if request.Body == nil || strings.TrimSpace(request.Body.SourceDir) == "" || len(request.Body.Names) == 0 {
		return tuttigenerated.ImportCursorSkills400JSONResponse{InvalidRequestErrorJSONResponse: cursorSkillImportInvalidRequest("select a source directory and at least one skill")}, nil
	}
	results, err := agent.ImportCursorSkills(request.Body.SourceDir, request.Body.Names)
	if err != nil {
		return tuttigenerated.ImportCursorSkills400JSONResponse{InvalidRequestErrorJSONResponse: cursorSkillImportInvalidRequest(err.Error())}, nil
	}
	items := make([]tuttigenerated.CursorSkillImportResult, 0, len(results))
	for _, item := range results {
		items = append(items, tuttigenerated.CursorSkillImportResult{
			Name: item.Name, Status: tuttigenerated.CursorSkillImportResultStatus(item.Status), Reason: optionalCursorImportReason(item.Reason),
		})
	}
	return tuttigenerated.ImportCursorSkills200JSONResponse{Results: items}, nil
}

func cursorSkillImportInvalidRequest(message string) tuttigenerated.InvalidRequestErrorJSONResponse {
	return invalidRequestError(apierrors.InvalidRequest(apierrors.ReasonMalformedRequest, apierrors.WithDeveloperMessage(message)))
}

func optionalCursorImportReason(reason string) *string {
	if reason == "" {
		return nil
	}
	return &reason
}
