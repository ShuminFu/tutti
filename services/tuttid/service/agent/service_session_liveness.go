package agent

import (
	"context"
	"fmt"
	"strings"
)

// SessionLivenessMaxIDs 是批量口一次能问的 id 上限。会话栏一屏最多几十行，
// 200 足够覆盖「当前所有分组里渲染着的会话」，再多就是调用方该分页了。
const SessionLivenessMaxIDs = 200

// SessionLivenessEntry 是单条会话的存活答案（补丁 0125）。
//
// Found 与 RuntimeLive 是两件事：Found=false 表示这个工作区根本不认识这个 id；
// Found=true 而 RuntimeLive=false 表示会话记录还在、但 provider（ACP）进程已经
// 不在运行时注册表里了（多半是被空闲回收放掉的，会话状态不会因此改变）。
type SessionLivenessEntry struct {
	Found        bool
	RuntimeLive  bool
	ActiveTurnID string
}

// RuntimeSessionLive 回答「这条会话现在有没有活的 ACP 进程」。
// 判据与 Host 内部那条完全同源（Host.RuntimeSessionLive →
// hostadapter.RuntimeController.RuntimeSessionLive → Controller.HasLiveSession），
// 不另起一套。
func (s *Service) RuntimeSessionLive(workspaceID string, agentSessionID string) bool {
	return s.runtimeSessionLiveIfConfigured(workspaceID, agentSessionID)
}

// runtimeSessionLiveIfConfigured 与 ApplicationHost() 的区别只有一点：没配 Host
// 时它回 false 而不是 panic。存活判据挂在**每一条会话响应**的投影尾巴上，
// 而 Host 是可选装配（大量单测只造 Service 不造 Host），在这条路上炸掉不合理；
// 「不知道」按「不活」处理，与 Runtime 未实现 RuntimeSessionLiveness 时一致。
func (s *Service) runtimeSessionLiveIfConfigured(workspaceID string, agentSessionID string) bool {
	if s == nil {
		return false
	}
	agentSessionID = strings.TrimSpace(agentSessionID)
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" || agentSessionID == "" {
		return false
	}
	s.applicationHostMu.Lock()
	provider := s.applicationHostProvider
	s.applicationHostMu.Unlock()
	if provider == nil {
		return false
	}
	host := provider()
	if host == nil {
		return false
	}
	return host.RuntimeSessionLive(workspaceID, agentSessionID)
}

// SessionLiveness 批量回答存活判据。
//
// 契约：**每个问到的 id 都会出现在结果里**，查不到就 Found=false —— 调用方
// 是轮询的会话栏，缺 key 与「查到了但不活」在它那边无法区分，会退化成沉默。
// 重复 id 归一成一条；空白 id 直接丢掉（它不是一个问题）。
func (s *Service) SessionLiveness(
	ctx context.Context,
	workspaceID string,
	agentSessionIDs []string,
) (map[string]SessionLivenessEntry, error) {
	_ = ctx
	workspaceID = strings.TrimSpace(workspaceID)
	if workspaceID == "" {
		return nil, fmt.Errorf("%w: workspace id is required", ErrInvalidArgument)
	}
	ids := normalizeSessionLivenessIDs(agentSessionIDs)
	if len(ids) == 0 {
		return nil, fmt.Errorf("%w: at least one agent session id is required", ErrInvalidArgument)
	}
	if len(ids) > SessionLivenessMaxIDs {
		return nil, fmt.Errorf(
			"%w: at most %d agent session ids are accepted, got %d",
			ErrInvalidArgument,
			SessionLivenessMaxIDs,
			len(ids),
		)
	}
	result := make(map[string]SessionLivenessEntry, len(ids))
	for _, id := range ids {
		entry := SessionLivenessEntry{}
		// 只读持久化那一份：批量口是 4s 一拍的轮询，不能走 Get 那条会解析
		// provider 能力的重路径。
		if s.SessionReader != nil {
			if persisted, found := s.SessionReader.GetSession(workspaceID, id); found {
				entry.Found = true
				entry.ActiveTurnID = strings.TrimSpace(persisted.ActiveTurnID)
			}
		}
		if entry.Found {
			entry.RuntimeLive = s.RuntimeSessionLive(workspaceID, id)
		}
		result[id] = entry
	}
	return result, nil
}

// NormalizeSessionLivenessIDs 把 "a,b" 与重复的 ids= 两种写法归一成一份去重后的
// 有序清单。导出给 API 层用，好让「上限」判在同一份清单上。
func NormalizeSessionLivenessIDs(raw []string) []string {
	return normalizeSessionLivenessIDs(raw)
}

func normalizeSessionLivenessIDs(raw []string) []string {
	seen := make(map[string]struct{}, len(raw))
	result := make([]string, 0, len(raw))
	for _, value := range raw {
		// 一个元素里可能是逗号分隔的一串，也可能就是一个 id，两种都收。
		for _, id := range strings.Split(value, ",") {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if _, duplicated := seen[id]; duplicated {
				continue
			}
			seen[id] = struct{}{}
			result = append(result, id)
		}
	}
	return result
}
