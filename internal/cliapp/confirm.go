package cliapp

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

// ConfirmDelete asks for interactive confirmation before a destructive operation.
// In TTY mode without --yes, it prompts "Delete <resource> <id>? [y/N]".
// In non-TTY mode without --yes, it returns an error requiring --yes.
func ConfirmDelete(stdout, stderr io.Writer, resource, id string, yes bool) error {
	if yes {
		return nil
	}
	// Non-TTY: require --yes flag.
	if f, ok := stdout.(*os.File); !ok || !term.IsTerminal(int(f.Fd())) {
		return &ValidationError{Message: "destructive operation requires --yes flag in non-interactive mode"}
	}
	_, _ = fmt.Fprintf(stdout, "Delete %s %s? [y/N] ", resource, id)
	reader := bufio.NewReader(os.Stdin)
	ans, _ := reader.ReadString('\n')
	if !strings.HasPrefix(strings.TrimSpace(strings.ToLower(ans)), "y") {
		return fmt.Errorf("aborted")
	}
	return nil
}

// Confirm asks "<prompt> [y/N]" in a terminal; without one it requires --yes.
func Confirm(stdout interface{ Write([]byte) (int, error) }, prompt string, yes bool) error {
	if yes {
		return nil
	}
	if f, ok := stdout.(*os.File); !ok || !term.IsTerminal(int(f.Fd())) {
		return &ValidationError{Message: "this operation requires --yes in non-interactive mode"}
	}
	_, _ = fmt.Fprintf(stdout, "%s [y/N] ", prompt)
	ans, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if !strings.HasPrefix(strings.TrimSpace(strings.ToLower(ans)), "y") {
		return fmt.Errorf("aborted")
	}
	return nil
}
