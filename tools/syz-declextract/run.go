// Copyright 2024 syzkaller project authors. All rights reserved.
// Use of this source code is governed by Apache 2 LICENSE that can be found in the LICENSE file.

package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/google/syzkaller/pkg/tool"
	"github.com/google/syzkaller/sys/targets"
)

type compileCommand struct {
	Arguments []string
	Directory string
	File      string
	Output    string
}

type output struct {
	stdout string
	stderr string
}

func main() {
	compilationDatabase := flag.String("compile_commands", "compile_commands.json", "path to compilation database")
	binary := flag.String("binary", "syz-declextract", "path to binary")
	outFile := flag.String("output", "out.txt", "output file")
	kernelDir := flag.String("kernel", "", "kernel directory")
	flag.Parse()
	if *kernelDir == "" {
		tool.Failf("path to kernel directory is required")
	}

	fileData, err := os.ReadFile(*compilationDatabase)
	if err != nil {
		tool.Fail(err)
	}

	var cmds []compileCommand
	if err := json.Unmarshal(fileData, &cmds); err != nil {
		tool.Fail(err)
	}

	outputs := make(chan output, len(cmds))
	files := make(chan string, len(cmds))
	for w := 0; w < runtime.NumCPU(); w++ {
		go worker(outputs, files, *binary, *compilationDatabase)
	}

	for _, v := range cmds {
		files <- v.File
	}

	var allOut []string
	syscallNames := readSyscallNames(filepath.Join(*kernelDir, "arch")) // some syscalls have different names and entry
	// points and thus need to be renamed.
	// e.g. SYSCALL_DEFINE1(setuid16, old_uid_t, uid) is referred to in the .tbl file with setuid.
	for range cmds {
		out := <-outputs
		if out.stderr != "" {
			tool.Failf("%s", out.stderr)
		}
		for _, line := range strings.Split(out.stdout, "\n") {
			if line == "" {
				continue
			}
			allOut = append(allOut, renameSyscall(line, syscallNames)...)
		}
	}
	close(files)
	writeOutput(allOut, *outFile)
}

func writeOutput(allOut []string, outFile string) {
	slices.Sort(allOut)
	allOut = slices.CompactFunc(allOut, func(a string, b string) bool {
		// We only compare the part before "$" for cases where the same system call has different parameter names,
		// but share the same syzkaller type. NOTE:Change when we have better type extraction.
		return strings.Split(a, "$")[0] == strings.Split(b, "$")[0]
	})
	err := os.WriteFile(outFile,
		[]byte("# Code generated by syz-declextract. DO NOT EDIT.\n"+strings.Join(allOut, "\n")+"\n_ = __NR_mmap2\n"), 0666)
	if err != nil {
		tool.Fail(err)
	}
}

func worker(outputs chan output, files chan string, binary, compilationDatabase string) {
	for file := range files {
		if !strings.HasSuffix(file, ".c") {
			outputs <- output{}
			return
		}

		cmd := exec.Command(binary, "-p", compilationDatabase, file)
		stdout, err := cmd.Output()
		var stderr string
		if err != nil {
			var error *exec.ExitError
			if errors.As(err, &error) {
				stderr = string(error.Stderr)
			} else {
				stderr = err.Error()
			}
		}
		outputs <- output{string(stdout), stderr}
	}
}

func renameSyscall(desc string, rename map[string][]string) []string {
	var renamed []string
	toReplace := strings.Split(desc, "$")[0]
	if rename[toReplace] == nil {
		// Syscall has no record in the tables for the architectures we support.
		return nil
	}

	for _, name := range rename[toReplace] {
		if isProhibited(name) {
			continue
		}
		renamed = append(renamed, strings.Replace(desc, toReplace, name, 1))
	}
	return renamed
}

func readSyscallNames(kernelDir string) map[string][]string {
	var rename = make(map[string][]string)
	for _, arch := range targets.List[targets.Linux] {
		filepath.Walk(filepath.Join(kernelDir, arch.KernelHeaderArch),
			func(path string, info fs.FileInfo, err error) error {
				if !strings.HasSuffix(path, ".tbl") {
					return nil
				}
				fi, fErr := os.Lstat(path)
				if fErr != nil {
					tool.Fail(err)
				}
				if fi.Mode()&fs.ModeSymlink != 0 { // Some symlinks link to files outside of arch directory.
					return nil
				}
				f, fErr := os.Open(path)
				if fErr != nil {
					tool.Fail(err)
				}
				s := bufio.NewScanner(f)
				for s.Scan() {
					fields := strings.Fields(s.Text())
					if len(fields) < 4 || fields[0] == "#" || strings.HasPrefix(fields[2], "unused") || fields[3] == "-" ||
						strings.HasPrefix(fields[3], "compat") || fields[3] == "sys_ni_syscall" {
						continue
					}
					key := strings.TrimPrefix(fields[3], "sys_")
					rename[key] = append(rename[key], fields[2])
				}
				return nil
			})
	}

	for k := range rename {
		slices.Sort(rename[k])
		rename[k] = slices.Compact(rename[k])
	}

	return rename
}

func isProhibited(syscall string) bool {
	switch syscall {
	case "reboot", "utimesat": // utimesat is not defined for all arches.
		return true
	default:
		return false
	}
}
