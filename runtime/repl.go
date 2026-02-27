package runtime

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
)

func (e *engineImpl) REPL(r io.Reader, w io.Writer) error {
	fmt.Fprintln(w, "go-lispico REPL")
	fmt.Fprintln(w, "Type (exit) or Ctrl+D to exit")

	rd := bufio.NewReader(r)
	ctx := context.Background()

	for {
		fmt.Fprint(w, "lispico> ")

		line, err := rd.ReadString('\n')
		if err == io.EOF {
			fmt.Fprintln(w)
			return nil
		}
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if isExitCommand(line) {
			return nil
		}

		input := line
		for !isBalanced(input) {
			fmt.Fprint(w, "       ")
			cont, err := rd.ReadString('\n')
			if err == io.EOF {
				fmt.Fprintln(w)
				return nil
			}
			if err != nil {
				return fmt.Errorf("read: %w", err)
			}
			input += "\n" + strings.TrimSpace(cont)

			if isExitCommand(input) {
				return nil
			}
		}

		result, err := e.Eval(ctx, "repl", input)
		if err != nil {
			fmt.Fprintf(w, "Error: %v\n", err)
			continue
		}

		fmt.Fprintln(w, result.String())
	}
}

func isExitCommand(input string) bool {
	trimmed := strings.TrimSpace(input)
	return trimmed == "(exit)" || trimmed == ",quit"
}

func isBalanced(input string) bool {
	var stack []rune
	inString := false
	escape := false

	for _, ch := range input {
		if escape {
			escape = false
			continue
		}

		if ch == '\\' && inString {
			escape = true
			continue
		}

		if ch == '"' {
			inString = !inString
			continue
		}

		if inString {
			continue
		}

		switch ch {
		case '(', '[', '{':
			stack = append(stack, ch)
		case ')':
			if len(stack) == 0 || stack[len(stack)-1] != '(' {
				return false
			}
			stack = stack[:len(stack)-1]
		case ']':
			if len(stack) == 0 || stack[len(stack)-1] != '[' {
				return false
			}
			stack = stack[:len(stack)-1]
		case '}':
			if len(stack) == 0 || stack[len(stack)-1] != '{' {
				return false
			}
			stack = stack[:len(stack)-1]
		}
	}

	return len(stack) == 0 && !inString
}
