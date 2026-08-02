export type BountyWalletType = 'wallet' | 'claude_wallet'

export type BountyTaskStatus =
  | 'draft'
  | 'published'
  | 'selecting'
  | 'assigned'
  | 'in_progress'
  | 'waiting_for_publisher'
  | 'publisher_replied'
  | 'submitted'
  | 'reviewing'
  | 'changes_requested'
  | 'completed'
  | 'expired'
  | 'cancelled'
  | 'disputed'
  | 'resolved'
  | 'suspended'

export interface BountyUser {
  id: number
  username: string
  display_name: string
}

export interface BountyTask {
  task_id: string
  publisher: BountyUser
  executor?: BountyUser
  title: string
  description: string
  repo_url: string
  task_type: string
  tags: string[]
  reward_wallet_type: BountyWalletType
  reward_amount: number
  reservation_id?: string
  status: BountyTaskStatus
  deadline_at: string
  review_deadline_at?: string
  revision_limit: number
  revision_count: number
  can_apply: boolean
  can_manage: boolean
  can_start: boolean
  can_submit: boolean
  can_handle_material_timeout: boolean
  can_dispute: boolean
  can_report: boolean
  created_at: string
  updated_at: string
}

export interface BountyApplication {
  application_id: string
  task_id: string
  applicant: BountyUser
  message: string
  estimated_delivery_at?: string
  status: 'pending' | 'accepted' | 'rejected' | string
  created_at: string
  updated_at: string
}

export interface BountyMaterialReply {
  reply_id: string
  author: BountyUser
  content: string
  source_type: string
  source_url?: string
  created_at: string
}

export interface BountyMaterialRequest {
  request_id: string
  requester: BountyUser
  content: string
  is_blocking: boolean
  status: 'open' | 'replied' | 'awaiting_confirmation' | 'closed' | string
  created_at: string
  resolved_at?: string
  timeout_at?: string
  timeout_action?: string
  replies: BountyMaterialReply[]
}

export interface BountySubmission {
  submission_id: string
  task_id: string
  executor: BountyUser
  version: number
  repo_url: string
  issue_url?: string
  pull_request_url?: string
  commit_sha: string
  completion_summary: string
  effect_images: string[]
  test_report: string
  known_limitations?: string
  status: string
  created_at: string
}

export interface BountyDispute {
  dispute_id: string
  task_id: string
  opened_by: BountyUser
  reason: string
  desired_outcome: string
  evidence_text: string
  ai_analysis?: {
    final_requirement_summary?: string
    requirement_changed?: boolean
    evidence_matches_commit?: boolean
    requirement_checks?: Array<{ item: string; result: string }>
    publisher_response_timely?: string
    executor_followed_replies?: string
    missing_evidence?: string[]
    recommended_resolution?: string
    confidence?: number
    risk_flags?: string[]
    disclaimer?: string
  }
  ai_model?: string
  ai_status: string
  status: string
  resolution_type?: string
  resolution_amount?: number
  resolution_note?: string
  resolved_by?: BountyUser
  resolved_at?: string
  created_at: string
}

export interface BountyEvent {
  event_id: string
  task_id: string
  event_type: string
  actor?: BountyUser
  actor_role: string
  payload?: Record<string, unknown>
  created_at: string
}

export interface BountyDetail {
  task: BountyTask
  applications: BountyApplication[]
  material_requests: BountyMaterialRequest[]
  submissions: BountySubmission[]
  disputes: BountyDispute[]
  timeline: BountyEvent[]
  my_application?: BountyApplication
}

export interface BountyListResponse {
  items: BountyTask[]
  total: number
  page: number
  page_size: number
}

export interface BountyBalance {
  wallet_type: BountyWalletType
  available_balance: number
  reserved_balance: number
}

export interface BountyNotification {
  notification_id: string
  task_id: string
  type: string
  title: string
  content: string
  read_at?: string
  created_at: string
}

export interface BountyNotificationResponse {
  items: BountyNotification[]
  unread_count: number
}

export interface BountyReport {
  report_id: string
  task_id: string
  reporter: BountyUser
  reason: string
  details: string
  status: string
  resolution_note?: string
  resolved_by?: BountyUser
  resolved_at?: string
  created_at: string
}

export interface AdminResolutionPayload {
  dispute_id: string
  resolution_type: 'pay_full' | 'pay_partial' | 'release' | 'changes_requested'
  amount?: number
  note?: string
}

export interface BountySearch {
  scope?: 'all' | 'mine_published' | 'mine_assigned' | 'mine_disputes'
  keyword?: string
  wallet_type?: 'all' | BountyWalletType
  status?: 'all' | 'available' | 'active' | 'ending_soon' | BountyTaskStatus
  sort?: 'latest' | 'reward_desc' | 'deadline_asc'
  tag?: string
  min_reward?: number
  max_reward?: number
  page?: number
}

export interface CreateBountyPayload {
  title: string
  description: string
  repo_url: string
  task_type: string
  tags: string[]
  reward_wallet_type: BountyWalletType
  reward_amount: number
  deadline_at: string
  idempotency_key?: string
}

export interface BountyDraftPayload {
  title?: string
  description?: string
  repo_url?: string
  task_type?: string
  tags?: string[]
  reward_wallet_type?: BountyWalletType
  reward_amount?: number
  deadline_at?: string
  idempotency_key?: string
}
