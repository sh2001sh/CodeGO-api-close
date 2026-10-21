package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	gatewayschema "github.com/sh2001sh/new-api/internal/gateway/schema"
	platformarchive "github.com/sh2001sh/new-api/internal/platform/archivex"
	"github.com/stretchr/testify/require"
)

func TestAuditArchivesAcceptNativeIDsAndPreserveRowsOnStorageFailure(t *testing.T) {
	for _, attempt := range []bool{false, true} {
		for _, fail := range []bool{false, true} {
			t.Run(map[bool]string{false: "request", true: "attempt"}[attempt]+map[bool]string{false: "/stored", true: "/storage_failure"}[fail], func(t *testing.T) {
				db := setupExecutionArchiveTestDB(t)
				require.NoError(t, db.AutoMigrate(&gatewayschema.RequestAudit{}, &gatewayschema.RequestAttemptAudit{}))
				now := time.Date(2026, 10, 20, 2, 0, 0, 0, time.UTC)
				old := now.AddDate(0, 0, -20)
				var model any = &gatewayschema.RequestAudit{}
				dataset := "request-audits"
				archive := ArchiveRequestAuditsBatch
				if attempt {
					model, dataset, archive = &gatewayschema.RequestAttemptAudit{}, "request-attempt-audits", ArchiveRequestAttemptAuditsBatch
					require.NoError(t, db.Create(&[]gatewayschema.RequestAttemptAudit{
						{AttemptID: "request:attempt:1", CompletedAt: old},
						{AttemptID: "request/attempt:2", CompletedAt: old},
						{AttemptID: "recent:attempt", CompletedAt: now},
					}).Error)
				} else {
					require.NoError(t, db.Create(&[]gatewayschema.RequestAudit{
						{RequestID: "request:1", CompletedAt: old, Status: gatewayschema.RequestAuditStatusSucceeded},
						{RequestID: "request/2", CompletedAt: old, Status: gatewayschema.RequestAuditStatusFailed},
						{RequestID: "recent:request", CompletedAt: now, Status: gatewayschema.RequestAuditStatusSucceeded},
					}).Error)
				}
				root := t.TempDir()
				fileSink, err := platformarchive.NewFileSink(root)
				require.NoError(t, err)
				var sink platformarchive.Sink = fileSink
				if fail {
					sink = &gatewayArchiveSink{err: errors.New("archive disk full")}
				}
				deleted, err := archive(context.Background(), sink, now, 7, 100)
				if fail {
					require.ErrorContains(t, err, "archive disk full")
					require.Zero(t, deleted)
					assertExecutionArchiveRowCount(t, db, model, 3)
					return
				}
				require.NoError(t, err)
				require.EqualValues(t, 2, deleted)
				assertExecutionArchiveRowCount(t, db, model, 1)
				files, err := filepath.Glob(filepath.Join(root, dataset, old.Format(time.DateOnly), "*.jsonl.gz"))
				require.NoError(t, err)
				require.Len(t, files, 1)
				deleted, err = archive(context.Background(), sink, now, 7, 100)
				require.NoError(t, err)
				require.Zero(t, deleted)
			})
		}
	}
}
