import type { AgentGeneratedImageRowVM } from "./agentGeneratedImageRowVM";
import type { AgentGoalControlRowVM } from "./agentGoalControlRowVM";
import type { AgentMcpAppRowVM } from "./agentMcpAppRowVM";
import type { AgentMessageRowVM } from "./agentMessageRowVM";
import type { AgentProcessingRowVM } from "./agentProcessingRowVM";
import type { AgentToolGroupRowVM } from "./agentToolGroupRowVM";
import type { AgentTurnSummaryRowVM } from "./agentTurnSummaryRowVM";

export type AgentTranscriptRowVM =
  | AgentGeneratedImageRowVM
  | AgentGoalControlRowVM
  | AgentMcpAppRowVM
  | AgentMessageRowVM
  | AgentToolGroupRowVM
  | AgentTurnSummaryRowVM
  | AgentProcessingRowVM;
