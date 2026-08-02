package domain

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const (
	WalletTypeDefault = "wallet"
	WalletTypeClaude  = "claude_wallet"

	TaskTypeGeneral  = "general"
	TaskTypeUI       = "ui"
	TaskTypeFrontend = "frontend"
	TaskTypeBackend  = "backend"

	TaskStatusDraft               = "draft"
	TaskStatusPublished           = "published"
	TaskStatusSelecting           = "selecting"
	TaskStatusAssigned            = "assigned"
	TaskStatusInProgress          = "in_progress"
	TaskStatusWaitingForPublisher = "waiting_for_publisher"
	TaskStatusPublisherReplied    = "publisher_replied"
	TaskStatusSubmitted           = "submitted"
	TaskStatusReviewing           = "reviewing"
	TaskStatusChangesRequested    = "changes_requested"
	TaskStatusCompleted           = "completed"
	TaskStatusExpired             = "expired"
	TaskStatusCancelled           = "cancelled"
	TaskStatusDisputed            = "disputed"
	TaskStatusResolved            = "resolved"
	TaskStatusSuspended           = "suspended"

	ApplicationStatusPending  = "pending"
	ApplicationStatusAccepted = "accepted"
	ApplicationStatusRejected = "rejected"

	MaterialStatusOpen                 = "open"
	MaterialStatusReplied              = "replied"
	MaterialStatusAwaitingConfirmation = "awaiting_confirmation"
	MaterialStatusClosed               = "closed"

	SubmissionStatusSubmitted  = "submitted"
	SubmissionStatusSuperseded = "superseded"

	DisputeStatusOpen     = "open"
	DisputeStatusResolved = "resolved"

	DisputeResolutionPayFull = "pay_full"
	DisputeResolutionPayPart = "pay_partial"
	DisputeResolutionRelease = "release"
	DisputeResolutionChanges = "changes_requested"
)

const (
	EventTaskPublished         = "task_published"
	EventTaskDraftSaved        = "task_draft_saved"
	EventRewardHeld            = "task_reward_hold"
	EventApplicationSubmitted  = "application_submitted"
	EventApplicationAccepted   = "application_accepted"
	EventApplicationRejected   = "application_rejected"
	EventTaskStarted           = "task_started"
	EventMaterialRequested     = "material_requested"
	EventMaterialReplied       = "material_replied"
	EventMaterialResolved      = "material_resolved"
	EventMaterialTimeout       = "material_timeout"
	EventMaterialTimeoutAction = "material_timeout_action"
	EventSubmissionCreated     = "submission_created"
	EventReviewStarted         = "review_started"
	EventReviewDeadlineSoon    = "review_deadline_soon"
	EventChangesRequested      = "changes_requested"
	EventReviewApproved        = "review_approved"
	EventTaskCompleted         = "task_completed"
	EventTaskCancelled         = "task_cancelled"
	EventTaskExpired           = "task_expired"
	EventDisputeOpened         = "dispute_opened"
	EventDisputeResolved       = "dispute_resolved"
	EventTaskSuspended         = "task_suspended"
	EventTaskResumed           = "task_resumed"
	EventTaskReported          = "task_reported"
	EventReportResolved        = "report_resolved"
	EventRewardPaid            = "task_reward_paid"
	EventRewardReleased        = "task_reward_release"
)

var commitSHAPattern = regexp.MustCompile(`^[0-9a-fA-F]{7,64}$`)

var taskTransitions = map[string]map[string]bool{
	TaskStatusDraft: {
		TaskStatusPublished: true,
	},
	TaskStatusPublished: {
		TaskStatusSelecting: true,
		TaskStatusAssigned:  true,
		TaskStatusCancelled: true,
		TaskStatusExpired:   true,
		TaskStatusSuspended: true,
	},
	TaskStatusSelecting: {
		TaskStatusAssigned:  true,
		TaskStatusCancelled: true,
		TaskStatusExpired:   true,
		TaskStatusSuspended: true,
	},
	TaskStatusAssigned: {
		TaskStatusInProgress:          true,
		TaskStatusWaitingForPublisher: true,
		TaskStatusCancelled:           true,
		TaskStatusExpired:             true,
		TaskStatusSuspended:           true,
	},
	TaskStatusInProgress: {
		TaskStatusWaitingForPublisher: true,
		TaskStatusSubmitted:           true,
		TaskStatusExpired:             true,
		TaskStatusDisputed:            true,
		TaskStatusSuspended:           true,
	},
	TaskStatusWaitingForPublisher: {
		TaskStatusPublisherReplied: true,
		TaskStatusCancelled:        true,
		TaskStatusDisputed:         true,
		TaskStatusExpired:          true,
		TaskStatusSuspended:        true,
	},
	TaskStatusPublisherReplied: {
		TaskStatusInProgress: true,
		TaskStatusSubmitted:  true,
		TaskStatusDisputed:   true,
		TaskStatusExpired:    true,
		TaskStatusSuspended:  true,
	},
	TaskStatusSubmitted: {
		TaskStatusReviewing: true,
		TaskStatusDisputed:  true,
		TaskStatusSuspended: true,
	},
	TaskStatusReviewing: {
		TaskStatusCompleted:        true,
		TaskStatusChangesRequested: true,
		TaskStatusDisputed:         true,
		TaskStatusSuspended:        true,
	},
	TaskStatusChangesRequested: {
		TaskStatusInProgress: true,
		TaskStatusSubmitted:  true,
		TaskStatusDisputed:   true,
		TaskStatusExpired:    true,
		TaskStatusSuspended:  true,
	},
	TaskStatusDisputed: {
		TaskStatusResolved:         true,
		TaskStatusChangesRequested: true,
		TaskStatusSuspended:        true,
	},
	TaskStatusCompleted: {
		TaskStatusResolved: true,
	},
	TaskStatusSuspended: {
		TaskStatusPublished:           true,
		TaskStatusSelecting:           true,
		TaskStatusAssigned:            true,
		TaskStatusInProgress:          true,
		TaskStatusWaitingForPublisher: true,
		TaskStatusPublisherReplied:    true,
		TaskStatusSubmitted:           true,
		TaskStatusReviewing:           true,
		TaskStatusChangesRequested:    true,
		TaskStatusDisputed:            true,
	},
}

func CanTransition(from string, to string) bool {
	return from == to || taskTransitions[from][to]
}

func RequireTransition(from string, to string) error {
	if CanTransition(from, to) {
		return nil
	}
	return fmt.Errorf("invalid bounty task transition: %s -> %s", from, to)
}

func NormalizeWalletType(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "wallet", "default", "ordinary", "normal":
		return WalletTypeDefault, nil
	case "claude", "claude_wallet":
		return WalletTypeClaude, nil
	default:
		return "", fmt.Errorf("unsupported reward wallet type")
	}
}

func IsTaskTypeUI(taskType string, tags []string) bool {
	switch strings.ToLower(strings.TrimSpace(taskType)) {
	case TaskTypeUI, TaskTypeFrontend:
		return true
	}
	for _, tag := range tags {
		switch strings.ToLower(strings.TrimSpace(tag)) {
		case "ui", "frontend", "前端", "界面", "交互", "设计":
			return true
		}
	}
	return false
}

func ValidateGitHubURL(raw string, allowImageHost bool) error {
	value := strings.TrimSpace(raw)
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return fmt.Errorf("github URL must use https")
	}
	host := strings.ToLower(parsed.Hostname())
	allowed := host == "github.com" || host == "www.github.com" || host == "gist.github.com"
	if allowImageHost {
		allowed = allowed || host == "raw.githubusercontent.com" || host == "user-images.githubusercontent.com" || strings.HasSuffix(host, ".githubusercontent.com")
	}
	if !allowed {
		return fmt.Errorf("URL must point to GitHub")
	}
	if parsed.Path == "" || parsed.Path == "/" {
		return fmt.Errorf("github URL path is required")
	}
	return nil
}

func ValidateCommitSHA(value string) error {
	if !commitSHAPattern.MatchString(strings.TrimSpace(value)) {
		return fmt.Errorf("commit SHA must contain 7 to 64 hexadecimal characters")
	}
	return nil
}

func IsTerminalStatus(status string) bool {
	switch status {
	case TaskStatusCompleted, TaskStatusExpired, TaskStatusCancelled, TaskStatusResolved:
		return true
	default:
		return false
	}
}
