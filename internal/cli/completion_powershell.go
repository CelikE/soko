package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Cobra 1.10 emits an empty string to suppress filename fallback when no
// candidates match. PowerShell's completion engine rejects that empty string
// when converting it to CompletionResult. Keep the typed word unchanged instead,
// without copying or rewriting Cobra's generated completion implementation.
const powerShellEmptyCompletionGuard = `
# Preserve no-file-completion semantics without an invalid empty CompletionResult.
Register-ArgumentCompleter -CommandName 'soko' -ScriptBlock {
    param($WordToComplete, $CommandAst, $CursorPosition)
    & $__sokoCompleterBlock $WordToComplete $CommandAst $CursorPosition | ForEach-Object {
        if ($_ -is [string] -and $_ -eq '') {
            if ($WordToComplete) {
                [System.Management.Automation.CompletionResult]::new(
                    $WordToComplete, $WordToComplete, 'ParameterValue', 'No matching completions')
            }
        } else {
            $_
        }
    }
}
`

func finishShellCompletion(cmd *cobra.Command, _ []string) error {
	if cmd.Name() != "powershell" || cmd.Parent() == nil || cmd.Parent().Name() != "completion" {
		return nil
	}
	_, err := fmt.Fprint(cmd.OutOrStdout(), powerShellEmptyCompletionGuard)
	return err
}
