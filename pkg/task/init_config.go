// Copyright 2020 PingCAP, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// See the License for the specific language governing permissions and
// limitations under the License.

package task

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pingcap-incubator/tiup-cluster/pkg/meta"
)

// InitConfig is used to copy all configurations to the target directory of path
type InitConfig struct {
	clusterName    string
	clusterVersion string
	instance       meta.Instance
	deployUser     string
	paths          meta.DirPaths
}

// Execute implements the Task interface
func (c *InitConfig) Execute(ctx *Context) error {
	// Copy to remote server
	exec, found := ctx.GetExecutor(c.instance.GetHost())
	if !found {
		return ErrNoExecutor
	}

	if err := os.MkdirAll(c.paths.Cache, 0755); err != nil {
		return err
	}

	return c.instance.InitConfig(exec, c.clusterName, c.clusterVersion, c.deployUser, c.paths)
}

// Rollback implements the Task interface
func (c *InitConfig) Rollback(ctx *Context) error {
	return ErrUnsupportedRollback
}

// String implements the fmt.Stringer interface
func (c *InitConfig) String() string {
	return fmt.Sprintf("InitConfig: cluster=%s, user=%s, host=%s, path=%s, %s",
		c.clusterName, c.deployUser, c.instance.GetHost(),
		filepath.Join(meta.ClusterPath(c.clusterName, "config", c.instance.ServiceName())), c.paths)
}
