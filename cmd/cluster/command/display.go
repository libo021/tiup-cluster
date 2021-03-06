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

package command

import (
	"fmt"
	"sort"
	"strings"

	"github.com/fatih/color"
	"github.com/pingcap-incubator/tiup-cluster/pkg/cliutil"
	"github.com/pingcap-incubator/tiup-cluster/pkg/log"
	"github.com/pingcap-incubator/tiup-cluster/pkg/meta"
	operator "github.com/pingcap-incubator/tiup-cluster/pkg/operation"
	"github.com/pingcap-incubator/tiup-cluster/pkg/task"
	"github.com/pingcap-incubator/tiup-cluster/pkg/utils"
	"github.com/pingcap-incubator/tiup/pkg/set"
	tiuputils "github.com/pingcap-incubator/tiup/pkg/utils"
	"github.com/pingcap/errors"
	"github.com/spf13/cobra"
)

type displayOption struct {
	clusterName string
	filterRole  []string
	filterNode  []string
}

func newDisplayCmd() *cobra.Command {
	opt := displayOption{}

	cmd := &cobra.Command{
		Use:   "display <cluster-name>",
		Short: "Display information of a TiDB cluster",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return cmd.Help()
			}

			opt.clusterName = args[0]
			if err := displayClusterMeta(&opt); err != nil {
				return err
			}
			if err := displayClusterTopology(&opt); err != nil {
				return err
			}

			metadata, err := meta.ClusterMetadata(opt.clusterName)
			if err != nil {
				return errors.AddStack(err)
			}
			return destroyTombstoneIfNeed(opt.clusterName, metadata)
		},
	}

	cmd.Flags().StringSliceVarP(&opt.filterRole, "role", "R", nil, "Only display specified roles")
	cmd.Flags().StringSliceVarP(&opt.filterNode, "node", "N", nil, "Only display specified nodes")

	return cmd
}

func displayClusterMeta(opt *displayOption) error {
	if tiuputils.IsNotExist(meta.ClusterPath(opt.clusterName, meta.MetaFileName)) {
		return errors.Errorf("cannot display non-exists cluster %s", opt.clusterName)
	}

	clsMeta, err := meta.ClusterMetadata(opt.clusterName)
	if err != nil {
		return err
	}

	cyan := color.New(color.FgCyan, color.Bold)

	fmt.Printf("TiDB Cluster: %s\n", cyan.Sprint(opt.clusterName))
	fmt.Printf("TiDB Version: %s\n", cyan.Sprint(clsMeta.Version))

	return nil
}

func destroyTombstoneIfNeed(clusterName string, metadata *meta.ClusterMeta) error {
	topo := metadata.Topology

	if !operator.NeedCheckTomebsome(topo) {
		return nil
	}

	ctx := task.NewContext()
	err := ctx.SetSSHKeySet(meta.ClusterPath(clusterName, "ssh", "id_rsa"),
		meta.ClusterPath(clusterName, "ssh", "id_rsa.pub"))
	if err != nil {
		return errors.AddStack(err)
	}

	err = ctx.SetClusterSSH(topo, metadata.User, sshTimeout)
	if err != nil {
		return errors.AddStack(err)
	}

	nodes, err := operator.DestroyTombstone(ctx, topo, true /* returnNodesOnly */)
	if err != nil {
		return errors.AddStack(err)
	}

	if len(nodes) == 0 {
		return nil
	}

	log.Infof("Start destroy Tombstone nodes: %v ...", nodes)

	_, err = operator.DestroyTombstone(ctx, topo, false /* returnNodesOnly */)
	if err != nil {
		return errors.AddStack(err)
	}

	log.Infof("Destroy success")

	return meta.SaveClusterMeta(clusterName, metadata)
}

func displayClusterTopology(opt *displayOption) error {
	metadata, err := meta.ClusterMetadata(opt.clusterName)
	if err != nil {
		return err
	}

	topo := metadata.Topology

	clusterTable := [][]string{
		// Header
		{"ID", "Role", "Host", "Ports", "Status", "Data Dir", "Deploy Dir"},
	}

	ctx := task.NewContext()
	err = ctx.SetSSHKeySet(meta.ClusterPath(opt.clusterName, "ssh", "id_rsa"),
		meta.ClusterPath(opt.clusterName, "ssh", "id_rsa.pub"))
	if err != nil {
		return errors.AddStack(err)
	}

	err = ctx.SetClusterSSH(topo, metadata.User, sshTimeout)
	if err != nil {
		return errors.AddStack(err)
	}

	filterRoles := set.NewStringSet(opt.filterRole...)
	filterNodes := set.NewStringSet(opt.filterNode...)
	pdList := topo.GetPDList()
	for _, comp := range topo.ComponentsByStartOrder() {
		for _, ins := range comp.Instances() {
			// apply role filter
			if len(filterRoles) > 0 && !filterRoles.Exist(ins.Role()) {
				continue
			}
			// apply node filter
			if len(filterNodes) > 0 && !filterNodes.Exist(ins.ID()) {
				continue
			}

			dataDir := "-"
			insDirs := ins.UsedDirs()
			deployDir := insDirs[0]
			if len(insDirs) > 1 {
				dataDir = insDirs[1]
			}

			status := ins.Status(pdList...)
			// Query the service status
			if status == "-" {
				e, found := ctx.GetExecutor(ins.GetHost())
				if found {
					active, _ := operator.GetServiceStatus(e, ins.ServiceName())
					if parts := strings.Split(strings.TrimSpace(active), " "); len(parts) > 2 {
						if parts[1] == "active" {
							status = "Up"
						} else {
							status = parts[1]
						}
					}
				}
			}
			clusterTable = append(clusterTable, []string{
				color.CyanString(ins.ID()),
				ins.Role(),
				ins.GetHost(),
				utils.JoinInt(ins.UsedPorts(), "/"),
				formatInstanceStatus(status),
				dataDir,
				deployDir,
			})

		}
	}

	// Sort by role,host,ports
	sort.Slice(clusterTable[1:], func(i, j int) bool {
		lhs, rhs := clusterTable[i+1], clusterTable[j+1]
		// column: 1 => role, 2 => host, 3 => ports
		for _, col := range []int{1, 2} {
			if lhs[col] != rhs[col] {
				return lhs[col] < rhs[col]
			}
		}
		return lhs[3] < rhs[3]
	})

	cliutil.PrintTable(clusterTable, true)

	return nil
}

func formatInstanceStatus(status string) string {
	switch strings.ToLower(status) {
	case "up", "healthy":
		return color.GreenString(status)
	case "healthy|l": // PD leader
		return color.HiGreenString(status)
	case "offline", "tombstone", "disconnected":
		return color.YellowString(status)
	case "down", "unhealthy", "err":
		return color.RedString(status)
	default:
		return status
	}
}
