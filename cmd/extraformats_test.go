package cmd

import "testing"

// Only a command that declares an extra -o format accepts it; every other
// command keeps the global set.
func TestAcceptsExtraFormat(t *testing.T) {
	if !acceptsExtraFormat(lambdaActivityCmd, "xlsx") || !acceptsExtraFormat(lambdaActivityCmd, "XLSX") {
		t.Error("lambda activity should accept -o xlsx")
	}
	if acceptsExtraFormat(lambdaActivityCmd, "pdf") {
		t.Error("an undeclared format must not be accepted")
	}
	if acceptsExtraFormat(lambdaFunctionsCmd, "xlsx") {
		t.Error("lambda functions did not declare xlsx and must reject it")
	}
}
