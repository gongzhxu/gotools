// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package gofmt

import (
	"bytes"
	"fmt"
	"go/token"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/visualfc/gotools/pkg/command"
	"github.com/visualfc/gotools/pkg/godiff"
	"golang.org/x/tools/imports"
)

var Command = &command.Command{
	Run:       runGofmt,
	UsageLine: "gofmt [flags] [path ...]",
	Short:     "gofmt formats Go source.",
	Long:      `gofmt formats Go source`,
}

var (
	gofmtList         bool
	gofmtWrite        bool
	gofmtDiff         bool
	gofmtAllErrors    bool
	gofmtFixImports   bool
	gofmtSortImports  bool
	gofmtUseGodiffLib bool

	// layout control
	gofmtComments  bool
	gofmtTabWidth  int
	gofmtTabIndent bool
)

// func init
func init() {
	Command.Flag.BoolVar(
		&gofmtList,
		"l",
		false,
		"list files whose formatting differs from goimport's",
	)
	Command.Flag.BoolVar(&gofmtWrite, "w", false, "write result to (source) file instead of stdout")
	Command.Flag.BoolVar(&gofmtDiff, "d", false, "display diffs instead of rewriting files")
	Command.Flag.BoolVar(
		&gofmtAllErrors,
		"e",
		false,
		"report all errors (not just the first 10 on different lines)",
	)
	Command.Flag.BoolVar(
		&gofmtFixImports,
		"fiximports",
		false,
		"updates Go import lines, adding missing ones and removing unreferenced ones",
	)
	Command.Flag.BoolVar(
		&gofmtSortImports,
		"sortimports",
		false,
		"sort Go import lines use goimports style",
	)
	Command.Flag.BoolVar(&gofmtUseGodiffLib, "godiff", true, "diff use godiff library")

	// layout control
	Command.Flag.BoolVar(&gofmtComments, "comments", true, "print comments")
	Command.Flag.IntVar(&gofmtTabWidth, "tabwidth", 8, "tab width")
	Command.Flag.BoolVar(&gofmtTabIndent, "tabs", true, "indent with tabs")
}

var (
	fileSet  = token.NewFileSet() // per process FileSet
	exitCode = 0

	initModesOnce sync.Once // guards calling initModes
	//parserMode    parser.Mode
	//printerMode   printer.Mode
	options *imports.Options
)

func runGofmt(cmd *command.Command, args []string) error {
	runtime.GOMAXPROCS(runtime.NumCPU())

	if gofmtTabWidth < 0 {
		return os.ErrInvalid
	}

	if gofmtFixImports {
		gofmtSortImports = true
	}

	options = &imports.Options{
		FormatOnly: !gofmtFixImports,
		TabWidth:   gofmtTabWidth,
		TabIndent:  gofmtTabIndent,
		Comments:   gofmtComments,
		AllErrors:  gofmtAllErrors,
		Fragment:   true,
	}

	if len(args) == 0 {
		return processFile("<standard input>", cmd.Stdin, cmd.Stdout, true)
	}
	for _, path := range args {
		switch dir, err := os.Stat(path); {
		case err != nil:
			fmt.Fprintln(cmd.Stderr, err)
		case dir.IsDir():
			walkDir(path)
		default:
			if err := processFile(path, nil, cmd.Stdout, false); err != nil {
				fmt.Fprintln(cmd.Stderr, err)
			}
		}
	}
	return nil
}

func isGoFile(f os.FileInfo) bool {
	// ignore non-Go files
	name := f.Name()
	return !f.IsDir() && !strings.HasPrefix(name, ".") && strings.HasSuffix(name, ".go")
}

func processFile(filename string, in io.Reader, out io.Writer, stdin bool) error {
	var src []byte
	var err error
	if in == nil {
		src, err = ioutil.ReadFile(filename)
	} else {
		src, err = ioutil.ReadAll(in)
	}
	if err != nil {
		return err
	}

	golinesCmd := exec.Command("golines")
	golinesIn, err := golinesCmd.StdinPipe()
	if err != nil {
		return err
	}

	go func() {
		defer golinesIn.Close()
		_, _ = golinesIn.Write(src)
	}()

	res, err := golinesCmd.Output()
	if err != nil {
		return err
	}

	res, err = imports.Process(filename, res, options)
	if err != nil {
		return err
	}

	if !bytes.Equal(src, res) {
		// formatting has changed
		if gofmtList {
			fmt.Fprintln(out, filename)
		}

		if gofmtWrite {
			err = ioutil.WriteFile(filename, res, 0)
			if err != nil {
				return err
			}
		}
		if gofmtDiff {
			var data []byte
			var err error
			if gofmtUseGodiffLib {
				var dataTmp string // because godiff.UnifiedDiffString returns string
				dataTmp, err = godiff.UnifiedDiffString(string(src), string(res))
				data = []byte(dataTmp)
			} else {
				data, err = godiff.UnifiedDiffBytesByCmd(src, res)
			}
			if err != nil {
				return fmt.Errorf("computing diff: %s", err)
			}
			fmt.Fprintf(out, "diff %s gofmt/%s\n", filename, filename)

			out.Write(data)
		}
	}

	if !gofmtList && !gofmtWrite && !gofmtDiff {
		_, err = out.Write(res)
	}

	return err
}

func visitFile(path string, f os.FileInfo, err error) error {
	if err == nil && isGoFile(f) {
		err = processFile(path, nil, os.Stdout, false)
	}
	return err
}

func walkDir(path string) {
	filepath.Walk(path, visitFile)
}
