package app

import (
	"testing"
	"time"

	"github.com/sh2001sh/new-api/constant"
	identityschema "github.com/sh2001sh/new-api/internal/identity/schema"
	platformdb "github.com/sh2001sh/new-api/internal/platform/db"
	"github.com/stretchr/testify/require"
)

func TestBountyAdminDetailDoesNotExposePublisherActions(t *testing.T) {
	resetBountyTestData(t)
	seedBountyUser(t, 1, "publisher", 500)
	seedBountyUser(t, 2, "admin", 0)
	require.NoError(t, platformdb.DB.Model(&identityschema.User{}).Where("id = ?", 2).Update("role", constant.RoleAdminUser).Error)

	created, err := CreateTask(1, CreateTaskRequest{
		Title:            "管理员只读查看任务",
		Description:      "验证管理员查看任务时不会获得发布者专属的确认和取消操作。",
		RepoURL:          "https://github.com/example/project",
		RewardWalletType: "wallet",
		RewardAmount:     100,
		DeadlineAt:       time.Now().Add(24 * time.Hour).Format(time.RFC3339),
		IdempotencyKey:   "admin-detail-permissions-1",
	})
	require.NoError(t, err)

	detail, err := GetTaskDetail(created.Task.TaskID, 2, constant.RoleAdminUser)
	require.NoError(t, err)
	require.False(t, detail.Task.CanManage)
	require.NotEmpty(t, detail.Task.ReservationID)
}
