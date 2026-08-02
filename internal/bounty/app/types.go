package app

import (
	"encoding/json"
	"time"
)

const (
	DefaultPageSize     = 20
	MaxPageSize         = 100
	ReviewWindow        = 72 * time.Hour
	MaterialReplyWindow = 48 * time.Hour
)

type CreateTaskRequest struct {
	Title            string   `json:"title"`
	Description      string   `json:"description"`
	RepoURL          string   `json:"repo_url"`
	TaskType         string   `json:"task_type"`
	Tags             []string `json:"tags"`
	RewardWalletType string   `json:"reward_wallet_type"`
	RewardAmount     int64    `json:"reward_amount"`
	DeadlineAt       string   `json:"deadline_at"`
	IdempotencyKey   string   `json:"idempotency_key"`
}

type ListTasksRequest struct {
	Scope      string
	Keyword    string
	WalletType string
	Status     string
	Tag        string
	MinReward  int64
	MaxReward  int64
	Sort       string
	Page       int
	PageSize   int
}

type CreateApplicationRequest struct {
	Message             string `json:"message"`
	EstimatedDeliveryAt string `json:"estimated_delivery_at"`
}

type AssignApplicationRequest struct {
	ApplicationID string `json:"application_id"`
}

type MaterialRequestInput struct {
	Content    string `json:"content"`
	IsBlocking bool   `json:"is_blocking"`
}

type MaterialReplyInput struct {
	Content    string `json:"content"`
	SourceType string `json:"source_type"`
	SourceURL  string `json:"source_url"`
}

type MaterialTimeoutInput struct {
	Action         string `json:"action"`
	ExtensionHours int    `json:"extension_hours"`
}

type ReportInput struct {
	Reason  string `json:"reason"`
	Details string `json:"details"`
}

type AdminReportResolutionInput struct {
	ReportID string `json:"report_id"`
	Note     string `json:"note"`
}

type SubmissionInput struct {
	RepoURL           string   `json:"repo_url"`
	IssueURL          string   `json:"issue_url"`
	PullRequestURL    string   `json:"pull_request_url"`
	CommitSHA         string   `json:"commit_sha"`
	CompletionSummary string   `json:"completion_summary"`
	EffectImages      []string `json:"effect_images"`
	TestReport        string   `json:"test_report"`
	KnownLimitations  string   `json:"known_limitations"`
}

type ReviewInput struct {
	Action  string `json:"action"`
	Comment string `json:"comment"`
}

type DisputeInput struct {
	Reason         string   `json:"reason"`
	DesiredOutcome string   `json:"desired_outcome"`
	EvidenceText   string   `json:"evidence_text"`
	GitHubURLs     []string `json:"github_urls"`
}

type AdminResolutionInput struct {
	DisputeID      string `json:"dispute_id"`
	ResolutionType string `json:"resolution_type"`
	Amount         int64  `json:"amount"`
	Note           string `json:"note"`
}

type TaskListResponse struct {
	Items    []TaskView `json:"items"`
	Total    int64      `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"page_size"`
}

type UserView struct {
	ID          int64  `json:"id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

type TaskView struct {
	TaskID                   string     `json:"task_id"`
	Publisher                UserView   `json:"publisher"`
	Executor                 *UserView  `json:"executor,omitempty"`
	Title                    string     `json:"title"`
	Description              string     `json:"description"`
	RepoURL                  string     `json:"repo_url"`
	TaskType                 string     `json:"task_type"`
	Tags                     []string   `json:"tags"`
	RewardWalletType         string     `json:"reward_wallet_type"`
	RewardAmount             int64      `json:"reward_amount"`
	ReservationID            string     `json:"reservation_id,omitempty"`
	Status                   string     `json:"status"`
	DeadlineAt               time.Time  `json:"deadline_at"`
	ReviewDeadlineAt         *time.Time `json:"review_deadline_at,omitempty"`
	RevisionLimit            int        `json:"revision_limit"`
	RevisionCount            int        `json:"revision_count"`
	CanApply                 bool       `json:"can_apply"`
	CanManage                bool       `json:"can_manage"`
	CanStart                 bool       `json:"can_start"`
	CanSubmit                bool       `json:"can_submit"`
	CanHandleMaterialTimeout bool       `json:"can_handle_material_timeout"`
	CanDispute               bool       `json:"can_dispute"`
	CanReport                bool       `json:"can_report"`
	CreatedAt                time.Time  `json:"created_at"`
	UpdatedAt                time.Time  `json:"updated_at"`
}

type ApplicationView struct {
	ApplicationID       string     `json:"application_id"`
	TaskID              string     `json:"task_id"`
	Applicant           UserView   `json:"applicant"`
	Message             string     `json:"message"`
	EstimatedDeliveryAt *time.Time `json:"estimated_delivery_at,omitempty"`
	Status              string     `json:"status"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

type MaterialReplyView struct {
	ReplyID    string    `json:"reply_id"`
	Author     UserView  `json:"author"`
	Content    string    `json:"content"`
	SourceType string    `json:"source_type"`
	SourceURL  string    `json:"source_url,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type MaterialRequestView struct {
	RequestID     string              `json:"request_id"`
	Requester     UserView            `json:"requester"`
	Content       string              `json:"content"`
	IsBlocking    bool                `json:"is_blocking"`
	Status        string              `json:"status"`
	CreatedAt     time.Time           `json:"created_at"`
	ResolvedAt    *time.Time          `json:"resolved_at,omitempty"`
	TimeoutAt     *time.Time          `json:"timeout_at,omitempty"`
	TimeoutAction string              `json:"timeout_action,omitempty"`
	Replies       []MaterialReplyView `json:"replies"`
}

type SubmissionView struct {
	SubmissionID      string    `json:"submission_id"`
	TaskID            string    `json:"task_id"`
	Executor          UserView  `json:"executor"`
	Version           int       `json:"version"`
	RepoURL           string    `json:"repo_url"`
	IssueURL          string    `json:"issue_url,omitempty"`
	PullRequestURL    string    `json:"pull_request_url,omitempty"`
	CommitSHA         string    `json:"commit_sha"`
	CompletionSummary string    `json:"completion_summary"`
	EffectImages      []string  `json:"effect_images"`
	TestReport        string    `json:"test_report"`
	KnownLimitations  string    `json:"known_limitations,omitempty"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"created_at"`
}

type DisputeView struct {
	DisputeID        string          `json:"dispute_id"`
	TaskID           string          `json:"task_id"`
	OpenedBy         UserView        `json:"opened_by"`
	Reason           string          `json:"reason"`
	DesiredOutcome   string          `json:"desired_outcome"`
	EvidenceText     string          `json:"evidence_text"`
	AIAnalysis       json.RawMessage `json:"ai_analysis,omitempty"`
	AIModel          string          `json:"ai_model,omitempty"`
	AIStatus         string          `json:"ai_status"`
	Status           string          `json:"status"`
	ResolutionType   string          `json:"resolution_type,omitempty"`
	ResolutionAmount int64           `json:"resolution_amount,omitempty"`
	ResolutionNote   string          `json:"resolution_note,omitempty"`
	ResolvedBy       *UserView       `json:"resolved_by,omitempty"`
	ResolvedAt       *time.Time      `json:"resolved_at,omitempty"`
	CreatedAt        time.Time       `json:"created_at"`
}

type EventView struct {
	EventID   string          `json:"event_id"`
	TaskID    string          `json:"task_id"`
	EventType string          `json:"event_type"`
	Actor     *UserView       `json:"actor,omitempty"`
	ActorRole string          `json:"actor_role"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

type TaskDetailView struct {
	Task             TaskView              `json:"task"`
	Applications     []ApplicationView     `json:"applications"`
	MaterialRequests []MaterialRequestView `json:"material_requests"`
	Submissions      []SubmissionView      `json:"submissions"`
	Disputes         []DisputeView         `json:"disputes"`
	Timeline         []EventView           `json:"timeline"`
	MyApplication    *ApplicationView      `json:"my_application,omitempty"`
}

type BalanceView struct {
	WalletType       string `json:"wallet_type"`
	AvailableBalance int64  `json:"available_balance"`
	ReservedBalance  int64  `json:"reserved_balance"`
}

type NotificationView struct {
	NotificationID string     `json:"notification_id"`
	TaskID         string     `json:"task_id"`
	Type           string     `json:"type"`
	Title          string     `json:"title"`
	Content        string     `json:"content"`
	ReadAt         *time.Time `json:"read_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}

type NotificationListResponse struct {
	Items       []NotificationView `json:"items"`
	UnreadCount int64              `json:"unread_count"`
}

type AdminTaskListResponse struct {
	Items    []TaskView `json:"items"`
	Total    int64      `json:"total"`
	Page     int        `json:"page"`
	PageSize int        `json:"page_size"`
}

type ReportView struct {
	ReportID       string     `json:"report_id"`
	TaskID         string     `json:"task_id"`
	Reporter       UserView   `json:"reporter"`
	Reason         string     `json:"reason"`
	Details        string     `json:"details"`
	Status         string     `json:"status"`
	ResolutionNote string     `json:"resolution_note,omitempty"`
	ResolvedBy     *UserView  `json:"resolved_by,omitempty"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
}
