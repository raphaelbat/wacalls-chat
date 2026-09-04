export type SessionState = "connecting" | "qr" | "open" | "logged_out";

export type SessionInfo = {
  id: string;
  name: string;
  jid: string;
  state: SessionState;
  paired: boolean;
  ownerId?: string;
  avatarUrl?: string;
  color?: string;
  isDefault?: boolean;
  allowGroups?: boolean;
  integrationToken?: string;
  queueId?: string;
  redirectMinutes?: number;
  flowId?: string;
  chatFlowId?: string;
  greetingMessage?: string;
  completionMessage?: string;
  outOfHoursMessage?: string;
  surveyEnabled?: boolean;
  surveyPrompt?: string;
};
