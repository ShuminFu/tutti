package api

import (
	"context"
	"encoding/json"
	"strings"

	storesqlite "github.com/tutti-os/tutti/packages/agent/store-sqlite"
	tuttigenerated "github.com/tutti-os/tutti/services/tuttid/api/generated"
	"github.com/tutti-os/tutti/services/tuttid/apierrors"
)

// AgentMCPAppResourceReader reads content-addressed MCP App UI resource
// snapshots captured by the MCP App resolver (service/mcpapp).
type AgentMCPAppResourceReader interface {
	GetMCPAppResourceSnapshot(context.Context, string) (storesqlite.MCPAppResourceSnapshot, bool, error)
}

// GetWorkspaceAgentMcpAppResource serves the snapshot a tool_call payload's
// mcpApp.resourceSha256 points at. Snapshots are immutable and content
// addressed, so they are not partitioned per workspace; the workspace segment
// keeps the route inside the agent-session API family.
func (api DaemonAPI) GetWorkspaceAgentMcpAppResource(ctx context.Context, request tuttigenerated.GetWorkspaceAgentMcpAppResourceRequestObject) (tuttigenerated.GetWorkspaceAgentMcpAppResourceResponseObject, error) {
	if api.AgentMCPAppResources == nil {
		return tuttigenerated.GetWorkspaceAgentMcpAppResource503JSONResponse{
			ServiceUnavailableErrorJSONResponse: agentSessionServiceUnavailableError(),
		}, nil
	}
	sha := strings.ToLower(strings.TrimSpace(request.ResourceSha256))
	if !storesqlite.ValidMCPAppResourceSHA256(sha) {
		return tuttigenerated.GetWorkspaceAgentMcpAppResource400JSONResponse{
			InvalidRequestErrorJSONResponse: invalidRequestError(apierrors.InvalidRequest(
				apierrors.ReasonMalformedRequest,
				apierrors.WithDeveloperMessage("resourceSha256 must be 64 hex characters"),
			)),
		}, nil
	}
	snapshot, ok, err := api.AgentMCPAppResources.GetMCPAppResourceSnapshot(ctx, sha)
	if err != nil {
		return tuttigenerated.GetWorkspaceAgentMcpAppResource502JSONResponse{
			WorkspaceOperationErrorJSONResponse: workspaceOperationError(apierrors.Classify(err)),
		}, nil
	}
	if !ok {
		return tuttigenerated.GetWorkspaceAgentMcpAppResource404JSONResponse{
			WorkspaceNotFoundErrorJSONResponse: workspaceNotFoundError(apierrors.WorkspaceNotFound(
				apierrors.ReasonWorkspaceAgentMCPAppResourceNotFound,
				apierrors.WithDeveloperMessage("MCP App resource snapshot was not found"),
			)),
		}, nil
	}
	return tuttigenerated.GetWorkspaceAgentMcpAppResource200JSONResponse{
		ResourceSha256: snapshot.SHA256,
		Uri:            snapshot.URI,
		MimeType:       snapshot.MimeType,
		Html:           snapshot.HTML,
		Meta:           generatedMCPAppResourceMeta(snapshot.MetaJSON),
	}, nil
}

// generatedMCPAppResourceMeta projects the stored `_meta.ui` onto the fields
// a display-only host honors. Unknown keys (permissions, domain) are dropped
// on purpose: the sandboxed iframe never grants them.
func generatedMCPAppResourceMeta(metaJSON string) tuttigenerated.WorkspaceAgentMcpAppResourceMeta {
	var raw struct {
		CSP *struct {
			ConnectDomains  *[]string `json:"connectDomains"`
			ResourceDomains *[]string `json:"resourceDomains"`
			FrameDomains    *[]string `json:"frameDomains"`
			BaseURIDomains  *[]string `json:"baseUriDomains"`
		} `json:"csp"`
		PrefersBorder *bool `json:"prefersBorder"`
	}
	meta := tuttigenerated.WorkspaceAgentMcpAppResourceMeta{}
	if err := json.Unmarshal([]byte(metaJSON), &raw); err != nil {
		return meta
	}
	meta.PrefersBorder = raw.PrefersBorder
	if raw.CSP != nil {
		meta.Csp = &tuttigenerated.WorkspaceAgentMcpAppResourceCsp{
			ConnectDomains:  raw.CSP.ConnectDomains,
			ResourceDomains: raw.CSP.ResourceDomains,
			FrameDomains:    raw.CSP.FrameDomains,
			BaseUriDomains:  raw.CSP.BaseURIDomains,
		}
	}
	return meta
}
