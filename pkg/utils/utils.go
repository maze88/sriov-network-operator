// Copyright 2025 sriov-network-device-plugin authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package utils

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/k8snetworkplumbingwg/sriov-network-operator/pkg/consts"
	"github.com/k8snetworkplumbingwg/sriov-network-operator/pkg/vars"
)

//go:generate ../../bin/mockgen -destination mock/mock_utils.go -source utils.go
type CmdInterface interface {
	Chroot(string) (func() error, error)
	RunCommand(string, ...string) (string, string, error)
}

type utilsHelper struct {
}

func New() CmdInterface {
	return &utilsHelper{}
}

func (u *utilsHelper) Chroot(path string) (func() error, error) {
	root, err := os.Open("/")
	if err != nil {
		return nil, err
	}

	if err := syscall.Chroot(path); err != nil {
		root.Close()
		return nil, err
	}
	vars.InChroot = true

	return func() error {
		defer root.Close()
		if err := root.Chdir(); err != nil {
			return err
		}
		vars.InChroot = false
		return syscall.Chroot(".")
	}, nil
}

// RunCommand runs a command
func (u *utilsHelper) RunCommand(command string, args ...string) (string, string, error) {
	log.Log.Info("RunCommand()", "command", command, "args", args)
	var stdout, stderr bytes.Buffer

	cmd := exec.Command(command, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	log.Log.V(2).Info("RunCommand()", "output", stdout.String(), "error", err)
	return stdout.String(), stderr.String(), err
}

func IsCommandNotFound(err error) bool {
	if exitErr, ok := err.(*exec.ExitError); ok {
		if status, ok := exitErr.Sys().(syscall.WaitStatus); ok && status.ExitStatus() == 127 {
			return true
		}
	}
	return false
}

func GetHostExtension() string {
	if vars.InChroot {
		return vars.FilesystemRoot
	}
	return filepath.Join(vars.FilesystemRoot, consts.Host)
}

func GetHostExtensionPath(path string) string {
	return filepath.Join(GetHostExtension(), path)
}

func GetChrootExtension() string {
	if vars.InChroot {
		return vars.FilesystemRoot
	}
	return fmt.Sprintf("chroot %s%s", vars.FilesystemRoot, consts.Host)
}
