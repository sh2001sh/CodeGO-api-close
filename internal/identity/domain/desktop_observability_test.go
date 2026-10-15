package domain

import (
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

func TestDesktopDiagnosticReportPayloadUsesPortableTextType(t *testing.T) {
	reportSchema, err := schema.Parse(&DesktopDiagnosticReport{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatalf("failed to parse schema: %v", err)
	}

	field := reportSchema.LookUpField("Payload")
	if field == nil {
		t.Fatal("expected payload field in desktop diagnostic report schema")
	}

	if got := field.TagSettings["TYPE"]; got != "text" {
		t.Fatalf("expected payload field type tag to be text, got %q", got)
	}
}
