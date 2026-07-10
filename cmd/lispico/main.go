package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"golang.org/x/term"

	"github.com/victorzhuk/go-lispico/clojure"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/data"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
	"github.com/victorzhuk/go-lispico/runtime"
)

func main() {
	os.Exit(run(os.Stdin, os.Stdout, os.Stderr, os.Args[1:]))
}

func run(stdin io.Reader, stdout, stderr io.Writer, args []string) int {
	fs := flag.NewFlagSet("lispico", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dialect := fs.String("dialect", "cl", "")
	bytecode := fs.Bool("bytecode", false, "")

	if err := fs.Parse(args); err != nil {
		return 1
	}

	var d runtime.EngineOption
	switch *dialect {
	case "cl":
	case "clojure":
		d = runtime.WithDialect(clojure.Dialect())
	default:
		fmt.Fprintf(stderr, "lispico: unknown dialect %q (valid: cl, clojure)\n", *dialect)
		return 1
	}

	opts := []runtime.EngineOption{}
	if d != nil {
		opts = append(opts, d)
	}
	if *bytecode {
		opts = append(opts, runtime.WithBytecode())
	}

	eng, err := runtime.New(nil, opts...)
	if err != nil {
		fmt.Fprintf(stderr, "lispico: %v\n", err)
		return 1
	}
	defer func() { _ = eng.Close() }()

	if err := eng.Use(stdlib.New()); err != nil {
		fmt.Fprintf(stderr, "lispico: %v\n", err)
		return 1
	}
	if err := eng.Use(data.New()); err != nil {
		fmt.Fprintf(stderr, "lispico: %v\n", err)
		return 1
	}

	if fs.NArg() > 0 {
		for _, path := range fs.Args() {
			result, err := eng.EvalFile(path)
			if err != nil {
				var lerr *core.LispicoError
				if errors.As(err, &lerr) && lerr.Line > 0 {
					fmt.Fprintf(stderr, "lispico: %s:%d: %s\n", path, lerr.Line, lerr.Message)
				} else {
					fmt.Fprintf(stderr, "lispico: %s: %v\n", path, err)
				}
				return 1
			}
			fmt.Fprintln(stdout, result.String())
		}
		return 0
	}

	fd := os.Stdin.Fd()
	if !term.IsTerminal(int(fd)) {
		if err := eng.REPL(stdin, stdout); err != nil {
			fmt.Fprintf(stderr, "lispico: %v\n", err)
			return 1
		}
		return 0
	}

	return runTerminal(eng, stdin, stdout)
}

type ctrlInterceptor struct {
	br      *bufio.Reader
	out     io.Writer
	lastKey byte
}

func (c *ctrlInterceptor) Read(p []byte) (int, error) {
	b, err := c.br.ReadByte()
	if err != nil {
		return 0, err
	}
	if b == 0x03 || b == 0x04 {
		c.lastKey = b
	}
	p[0] = b
	return 1, nil
}

func (c *ctrlInterceptor) Write(p []byte) (int, error) {
	return c.out.Write(p)
}

func historyPath() string {
	if xdg := os.Getenv("XDG_STATE_HOME"); xdg != "" {
		return filepath.Join(xdg, "lispico", "history")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "state", "lispico", "history")
}

func loadHistory(t *term.Terminal, path string) {
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return
	}
	for _, line := range lines {
		if line != "" {
			t.History.Add(line)
		}
	}
}

func saveHistory(t *term.Terminal, path string) {
	if path == "" {
		return
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	var buf strings.Builder
	for i := t.History.Len() - 1; i >= 0; i-- {
		buf.WriteString(t.History.At(i))
		buf.WriteByte('\n')
	}
	_ = os.WriteFile(path, []byte(buf.String()), 0o644)
}

func runTerminal(eng runtime.Engine, stdin io.Reader, stdout io.Writer) int {
	fd := os.Stdin.Fd()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)

	oldState, err := term.MakeRaw(int(fd))
	if err != nil {
		if rerr := eng.REPL(stdin, stdout); rerr != nil {
			return 1
		}
		return 0
	}
	defer func() { _ = term.Restore(int(fd), oldState) }()

	go func() {
		<-sigCh
		_ = term.Restore(int(fd), oldState)
		os.Exit(130)
	}()

	ci := &ctrlInterceptor{
		br:  bufio.NewReader(stdin),
		out: stdout,
	}

	histPath := historyPath()
	t := term.NewTerminal(ci, "lispico> ")
	loadHistory(t, histPath)

	fmt.Fprintln(t, "go-lispico REPL — Ctrl-D to exit")

	var inputBuilder strings.Builder
	inContinuation := false

	for {
		line, err := t.ReadLine()

		if err == io.EOF {
			if ci.lastKey == 0x03 {
				ci.lastKey = 0
				inputBuilder.Reset()
				inContinuation = false
				oldHistory := t.History
				t = term.NewTerminal(ci, "lispico> ")
				for i := oldHistory.Len() - 1; i >= 0; i-- {
					t.History.Add(oldHistory.At(i))
				}
				fmt.Fprintln(t, "")
				continue
			}
			break
		}
		ci.lastKey = 0
		if err != nil {
			break
		}

		if inputBuilder.Len() > 0 {
			inputBuilder.WriteByte('\n')
		}
		inputBuilder.WriteString(line)
		input := inputBuilder.String()

		if runtime.IsExitCommand(input) {
			break
		}

		if !runtime.IsBalanced(input) {
			if !inContinuation {
				t.SetPrompt("       ")
				inContinuation = true
			}
			continue
		}

		if inContinuation {
			t.SetPrompt("lispico> ")
			inContinuation = false
		}

		result, evalErr := eng.Eval(context.Background(), "repl", input)
		if evalErr != nil {
			fmt.Fprintf(t, "Error: %v\n", evalErr)
		} else if result != nil {
			fmt.Fprintln(t, result.String())
		}
		inputBuilder.Reset()
	}
	saveHistory(t, histPath)

	return 0
}
