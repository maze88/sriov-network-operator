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

package host

import (
	"github.com/k8snetworkplumbingwg/sriov-network-operator/pkg/host/internal/bridge"
	"github.com/k8snetworkplumbingwg/sriov-network-operator/pkg/host/internal/cpu"
	"github.com/k8snetworkplumbingwg/sriov-network-operator/pkg/host/internal/infiniband"
	"github.com/k8snetworkplumbingwg/sriov-network-operator/pkg/host/internal/kernel"
	"github.com/k8snetworkplumbingwg/sriov-network-operator/pkg/host/internal/lib/dputils"
	"github.com/k8snetworkplumbingwg/sriov-network-operator/pkg/host/internal/lib/ethtool"
	"github.com/k8snetworkplumbingwg/sriov-network-operator/pkg/host/internal/lib/ghw"
	"github.com/k8snetworkplumbingwg/sriov-network-operator/pkg/host/internal/lib/netlink"
	"github.com/k8snetworkplumbingwg/sriov-network-operator/pkg/host/internal/lib/sriovnet"
	"github.com/k8snetworkplumbingwg/sriov-network-operator/pkg/host/internal/network"
	"github.com/k8snetworkplumbingwg/sriov-network-operator/pkg/host/internal/service"
	"github.com/k8snetworkplumbingwg/sriov-network-operator/pkg/host/internal/sriov"
	"github.com/k8snetworkplumbingwg/sriov-network-operator/pkg/host/internal/systemd"
	"github.com/k8snetworkplumbingwg/sriov-network-operator/pkg/host/internal/udev"
	"github.com/k8snetworkplumbingwg/sriov-network-operator/pkg/host/internal/vdpa"
	"github.com/k8snetworkplumbingwg/sriov-network-operator/pkg/host/types"
	"github.com/k8snetworkplumbingwg/sriov-network-operator/pkg/utils"
)

// Contains all the host manipulation functions
//
//go:generate ../../bin/mockgen -destination mock/mock_host.go -source manager.go
type HostManagerInterface interface {
	types.KernelInterface
	types.NetworkInterface
	types.ServiceInterface
	types.UdevInterface
	types.SriovInterface
	types.VdpaInterface
	types.InfinibandInterface
	types.BridgeInterface
	types.CPUInfoProviderInterface
	types.SystemdInterface
}

type hostManager struct {
	utils.CmdInterface
	types.KernelInterface
	types.NetworkInterface
	types.ServiceInterface
	types.UdevInterface
	types.SriovInterface
	types.VdpaInterface
	types.InfinibandInterface
	types.BridgeInterface
	types.CPUInfoProviderInterface
	types.SystemdInterface
}

func NewHostManager(utilsInterface utils.CmdInterface) (HostManagerInterface, error) {
	dpUtils := dputils.New()
	netlinkLib := netlink.New()
	ethtoolLib := ethtool.New()
	sriovnetLib := sriovnet.New()
	ghwLib := ghw.New()
	k := kernel.New(utilsInterface)
	n := network.New(utilsInterface, dpUtils, netlinkLib, ethtoolLib)
	sv := service.New(utilsInterface)
	u := udev.New(utilsInterface)
	v := vdpa.New(k, netlinkLib)
	ib, err := infiniband.New(netlinkLib, k, n)
	if err != nil {
		return nil, err
	}
	br := bridge.New()
	sr := sriov.New(utilsInterface, k, n, u, v, ib, netlinkLib, dpUtils, sriovnetLib, ghwLib, br)
	cpuInfoProvider := cpu.New(ghwLib)
	s := systemd.New()
	return &hostManager{
		utilsInterface,
		k,
		n,
		sv,
		u,
		sr,
		v,
		ib,
		br,
		cpuInfoProvider,
		s,
	}, nil
}
