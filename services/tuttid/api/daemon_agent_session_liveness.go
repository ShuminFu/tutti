package api

import (
	"context"
	"fmt"

	tuttigenerated "github.com/tutti-os/tutti/services/tuttid/api/generated"
	"github.com/tutti-os/tutti/services/tuttid/apierrors"
	agentservice "github.com/tutti-os/tutti/services/tuttid/service/agent"
)

// GetWorkspaceAgentSessionLiveness 是补丁 0125 的批量存活口。
//
// 它回答的是「这条会话此刻还有没有活的 provider（ACP）进程」。空闲回收
// （ReleaseIdleLiveSessions）放掉进程时既不改会话状态、也不发事件，所以调用方
// 只能来问；这个口就是为「会话栏 4s 一拍、一次问一屏」设计的。
//
// 三条硬约束：
//  1. 每个问到的 id 都出现在结果里，查不到就 found=false —— 少一个 key 与
//     「查到了但不活」在调用方那边分不开，会退化成沉默。
//  2. `ids` 同时接受逗号分隔和重复参数两种写法，归一去重后再判上限。
//  3. 一个都没有、或超过 SessionLivenessMaxIDs 个，都回 400。
func (api DaemonAPI) GetWorkspaceAgentSessionLiveness(
	ctx context.Context,
	request tuttigenerated.GetWorkspaceAgentSessionLivenessRequestObject,
) (tuttigenerated.GetWorkspaceAgentSessionLivenessResponseObject, error) {
	if api.AgentSessionService == nil {
		return tuttigenerated.GetWorkspaceAgentSessionLiveness503JSONResponse{
			ServiceUnavailableErrorJSONResponse: agentSessionServiceUnavailableError(),
		}, nil
	}
	ids := agentservice.NormalizeSessionLivenessIDs(request.Params.Ids)
	if len(ids) == 0 {
		return workspaceAgentSessionLivenessInvalidRequest(
			"at least one agent session id is required",
		), nil
	}
	if len(ids) > agentservice.SessionLivenessMaxIDs {
		return workspaceAgentSessionLivenessInvalidRequest(fmt.Sprintf(
			"at most %d agent session ids are accepted, got %d",
			agentservice.SessionLivenessMaxIDs,
			len(ids),
		)), nil
	}
	entries, err := api.AgentSessionService.SessionLiveness(ctx, string(request.WorkspaceID), ids)
	if err != nil {
		return writeGetWorkspaceAgentSessionLivenessError(err), nil
	}
	sessions := make(map[string]tuttigenerated.WorkspaceAgentSessionLivenessEntry, len(ids))
	for _, id := range ids {
		// 服务层漏返某个 id 时按「查不到」补齐，绝不让 key 消失。
		entry := entries[id]
		sessions[id] = tuttigenerated.WorkspaceAgentSessionLivenessEntry{
			ActiveTurnId: entry.ActiveTurnID,
			Found:        entry.Found,
			RuntimeLive:  entry.RuntimeLive,
		}
	}
	return tuttigenerated.GetWorkspaceAgentSessionLiveness200JSONResponse{
		Sessions: sessions,
	}, nil
}

func workspaceAgentSessionLivenessInvalidRequest(
	developerMessage string,
) tuttigenerated.GetWorkspaceAgentSessionLivenessResponseObject {
	return tuttigenerated.GetWorkspaceAgentSessionLiveness400JSONResponse{
		InvalidRequestErrorJSONResponse: invalidRequestError(
			apierrors.InvalidRequest(
				apierrors.ReasonMalformedRequest,
				apierrors.WithDeveloperMessage(developerMessage),
			),
		),
	}
}

func writeGetWorkspaceAgentSessionLivenessError(
	err error,
) tuttigenerated.GetWorkspaceAgentSessionLivenessResponseObject {
	protocolErr := apierrors.Classify(err)
	switch protocolErr.Code {
	case tuttigenerated.WorkspaceNotFound:
		return tuttigenerated.GetWorkspaceAgentSessionLiveness404JSONResponse{
			WorkspaceNotFoundErrorJSONResponse: workspaceNotFoundError(protocolErr),
		}
	case tuttigenerated.InvalidRequest:
		return tuttigenerated.GetWorkspaceAgentSessionLiveness400JSONResponse{
			InvalidRequestErrorJSONResponse: invalidRequestError(protocolErr),
		}
	default:
		return tuttigenerated.GetWorkspaceAgentSessionLiveness502JSONResponse{
			WorkspaceOperationErrorJSONResponse: workspaceOperationError(protocolErr),
		}
	}
}
