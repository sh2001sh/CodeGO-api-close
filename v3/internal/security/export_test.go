package security

import "testing"

func TestExportTextCannotExecuteSpreadsheetFormula(t *testing.T) {
	for _, input := range []string{"=1+1", "+SUM(1,2)", "-CMD()", "@SUM(A1)", " \t\r\n=1+1"} {
		if got := protectCSVText(input); got != "'"+input {
			t.Errorf("unsafe CSV text %q remained %q", input, got)
		}
	}
	for _, input := range []string{"", "9007199254740993", "audit-123", "plain,quoted text", "2026-09-30T12:00:00Z"} {
		if got := protectCSVText(input); got != input {
			t.Errorf("ordinary CSV text %q changed to %q", input, got)
		}
	}
}
