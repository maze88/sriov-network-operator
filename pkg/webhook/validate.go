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

package webhook

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	v1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	sriovnetworkv1 "github.com/k8snetworkplumbingwg/sriov-network-operator/api/v1"
	"github.com/k8snetworkplumbingwg/sriov-network-operator/pkg/consts"
	"github.com/k8snetworkplumbingwg/sriov-network-operator/pkg/vars"
)

const (
	IntelID    = "8086"
	MellanoxID = "15b3"
	MlxMaxVFs  = 128
)

var (
	nodesSelected     bool
	interfaceSelected bool
)

func validateSriovOperatorConfig(cr *sriovnetworkv1.SriovOperatorConfig, operation v1.Operation) (bool, []string, error) {
	log.Log.V(2).Info("validateSriovOperatorConfig", "object", cr)
	var warnings []string

	if operation == v1.Delete {
		return true, warnings, nil
	}

	if cr.GetName() != consts.DefaultConfigName || cr.GetNamespace() != vars.Namespace {
		return false, warnings, fmt.Errorf("only default SriovOperatorConfig in %s namespace is used", vars.Namespace)
	}

	if cr.Spec.DisableDrain {
		warnings = append(warnings, "Node draining is disabled for applying SriovNetworkNodePolicy, it may result in workload interruption.")
	}

	err := validateSriovOperatorConfigDisableDrain(cr)
	if err != nil {
		return false, warnings, err
	}

	return true, warnings, nil
}

// validateSriovOperatorConfigDisableDrain checks if the user is setting `.Spec.DisableDrain` from false to true while
// operator is updating one or more nodes. Disabling the drain at this stage would prevent the operator to uncordon a node at
// the end of the update operation, keeping nodes un-schedulable until manual intervention.
func validateSriovOperatorConfigDisableDrain(cr *sriovnetworkv1.SriovOperatorConfig) error {
	if !cr.Spec.DisableDrain {
		return nil
	}
	previousConfig := &sriovnetworkv1.SriovOperatorConfig{}
	err := client.Get(context.Background(), runtimeclient.ObjectKey{Name: cr.Name, Namespace: namespace}, previousConfig)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("can't validate SriovOperatorConfig[%s] DisableDrain against its previous value: %q", cr.Name, err)
	}

	if previousConfig.Spec.DisableDrain == cr.Spec.DisableDrain {
		// DisableDrain didn't change
		return nil
	}

	// DisableDrain has been changed `false -> true`, check if any node is updating
	nodeStates := &sriovnetworkv1.SriovNetworkNodeStateList{}
	err = client.List(context.Background(), nodeStates, &runtimeclient.ListOptions{Namespace: namespace})
	if err != nil {
		return fmt.Errorf("can't validate SriovOperatorConfig[%s] DisableDrain transition to true: %q", cr.Name, err)
	}

	for _, nodeState := range nodeStates.Items {
		if nodeState.Status.SyncStatus == "InProgress" {
			return fmt.Errorf("can't set Spec.DisableDrain = true while node[%s] is updating", nodeState.Name)
		}
	}

	return nil
}

// validateSriovNetworkPoolConfig checks if the use tries to remove the default pool config and block it
func validateSriovNetworkPoolConfig(cr *sriovnetworkv1.SriovNetworkPoolConfig, operation v1.Operation) (bool, []string, error) {
	log.Log.V(2).Info("validateSriovNetworkPoolConfig", "object", cr)
	var warnings []string

	if (cr.Spec.MaxUnavailable != nil || cr.Spec.NodeSelector != nil) && cr.Spec.OvsHardwareOffloadConfig.Name != "" {
		return false, warnings, fmt.Errorf("SriovOperatorConfig can't have both parallel configuration and OvsHardwareOffloadConfig")
	}

	if cr.Spec.MaxUnavailable != nil {
		_, err := cr.MaxUnavailable(0)
		if err != nil {
			return false, warnings, fmt.Errorf("SriovOperatorConfig invalid maxUnavailable: %v", err)
		}
	}

	return true, warnings, nil
}

func validateSriovNetworkNodePolicy(cr *sriovnetworkv1.SriovNetworkNodePolicy, operation v1.Operation) (bool, []string, error) {
	log.Log.V(2).Info("validateSriovNetworkNodePolicy", "object", cr)
	var warnings []string

	if cr.GetName() == consts.DefaultPolicyName && cr.GetNamespace() == vars.Namespace {
		// skip validating (deprecated) default policy
		return true, warnings, nil
	}

	if cr.GetNamespace() != vars.Namespace {
		warnings = append(warnings, cr.GetName()+
			fmt.Sprintf(" is created or updated but not used. Only policy in %s namespace is respected.", vars.Namespace))
	}

	if operation == v1.Delete {
		return true, warnings, nil
	}

	admit, err := staticValidateSriovNetworkNodePolicy(cr)
	if err != nil {
		return admit, warnings, err
	}

	admit, err = dynamicValidateSriovNetworkNodePolicy(cr)
	if err != nil {
		return admit, warnings, err
	}

	return admit, warnings, nil
}

func staticValidateSriovNetworkNodePolicy(cr *sriovnetworkv1.SriovNetworkNodePolicy) (bool, error) {
	var validString = regexp.MustCompile(`^[a-zA-Z0-9_]+$`)
	if !validString.MatchString(cr.Spec.ResourceName) {
		return false, fmt.Errorf("resource name \"%s\" contains invalid characters, the accepted syntax of the regular expressions is: \"^[a-zA-Z0-9_]+$\"", cr.Spec.ResourceName)
	}

	if cr.Spec.NicSelector.Vendor == "" && cr.Spec.NicSelector.DeviceID == "" && len(cr.Spec.NicSelector.PfNames) == 0 && len(cr.Spec.NicSelector.RootDevices) == 0 && cr.Spec.NicSelector.NetFilter == "" {
		return false, fmt.Errorf("at least one of these parameters (vendor, deviceID, pfNames, rootDevices or netFilter) has to be defined in nicSelector in CR %s", cr.GetName())
	}

	devMode := false
	if os.Getenv("DEV_MODE") == "TRUE" {
		devMode = true
		log.Log.V(0).Info("dev mode enabled - Admitting not supported NICs")
	}

	if !devMode {
		if cr.Spec.NicSelector.Vendor != "" {
			if !sriovnetworkv1.IsSupportedVendor(cr.Spec.NicSelector.Vendor) {
				return false, fmt.Errorf("vendor %s is not supported", cr.Spec.NicSelector.Vendor)
			}
			if cr.Spec.NicSelector.DeviceID != "" {
				if !sriovnetworkv1.IsSupportedModel(cr.Spec.NicSelector.Vendor, cr.Spec.NicSelector.DeviceID) {
					return false, fmt.Errorf("vendor/device %s/%s is not supported", cr.Spec.NicSelector.Vendor, cr.Spec.NicSelector.DeviceID)
				}
			}
		} else if cr.Spec.NicSelector.DeviceID != "" {
			if !sriovnetworkv1.IsSupportedDevice(cr.Spec.NicSelector.DeviceID) {
				return false, fmt.Errorf("device %s is not supported", cr.Spec.NicSelector.DeviceID)
			}
		}
	}

	if len(cr.Spec.NicSelector.PfNames) > 0 {
		for _, pf := range cr.Spec.NicSelector.PfNames {
			if strings.Contains(pf, "#") {
				fields := strings.Split(pf, "#")
				if len(fields) != 2 {
					return false, fmt.Errorf("failed to parse %s PF name in nicSelector, probably incorrect separator character usage", pf)
				}
				rng := strings.Split(fields[1], "-")
				if len(rng) != 2 {
					return false, fmt.Errorf("failed to parse %s PF name nicSelector, probably incorrect range character usage", pf)
				}
				rngSt, err := strconv.Atoi(rng[0])
				if err != nil {
					return false, fmt.Errorf("failed to parse %s PF name nicSelector, start range is incorrect", pf)
				}
				rngEnd, err := strconv.Atoi(rng[1])
				if err != nil {
					return false, fmt.Errorf("failed to parse %s PF name nicSelector, end range is incorrect", pf)
				}
				if rngEnd < rngSt {
					return false, fmt.Errorf("failed to parse %s PF name nicSelector, end range shall not be smaller than start range", pf)
				}
				if !(rngEnd < cr.Spec.NumVfs) {
					return false, fmt.Errorf("failed to parse %s PF name nicSelector, end range exceeds the maximum VF index ", pf)
				}
			}
		}
	}

	// To configure RoCE on baremetal or virtual machine:
	// BM: DeviceType = netdevice && isRdma = true
	// VM: DeviceType = vfio-pci && isRdma = false
	if cr.Spec.DeviceType == consts.DeviceTypeVfioPci && cr.Spec.IsRdma {
		return false, fmt.Errorf("'deviceType: vfio-pci' conflicts with 'isRdma: true'; Set 'deviceType' to (string)'netdevice' Or Set 'isRdma' to (bool)'false'")
	}

	// switchdev mode can be used only with ethernet links
	if cr.Spec.LinkType != "" && !strings.EqualFold(cr.Spec.LinkType, consts.LinkTypeETH) && cr.Spec.EswitchMode == sriovnetworkv1.ESwithModeSwitchDev {
		return false, fmt.Errorf("'eSwitchMode: switchdev' can be used only with ethernet links")
	}

	// vdpa: deviceType must be set to 'netdevice'
	if cr.Spec.DeviceType != consts.DeviceTypeNetDevice && (cr.Spec.VdpaType == consts.VdpaTypeVirtio || cr.Spec.VdpaType == consts.VdpaTypeVhost) {
		return false, fmt.Errorf("'deviceType: %s' conflicts with '%s'; Set 'deviceType' to (string)'netdevice' Or Remove 'vdpaType'", cr.Spec.DeviceType, cr.Spec.VdpaType)
	}
	// vdpa: device must be configured in switchdev mode
	if (cr.Spec.VdpaType == consts.VdpaTypeVirtio || cr.Spec.VdpaType == consts.VdpaTypeVhost) && cr.Spec.EswitchMode != sriovnetworkv1.ESwithModeSwitchDev {
		return false, fmt.Errorf("vdpa requires the device to be configured in switchdev mode")
	}
	// software bridge management: device must be configured in switchdev mode
	if !cr.Spec.Bridge.IsEmpty() && cr.Spec.EswitchMode != sriovnetworkv1.ESwithModeSwitchDev {
		return false, fmt.Errorf("software bridge management requires the device to be configured in switchdev mode")
	}
	// software bridge management: device can't be externally managed
	if !cr.Spec.Bridge.IsEmpty() && cr.Spec.ExternallyManaged {
		return false, fmt.Errorf("software bridge management can't be used when the device externally managed")
	}
	return true, nil
}

func dynamicValidateSriovNetworkNodePolicy(cr *sriovnetworkv1.SriovNetworkNodePolicy) (bool, error) {
	nodesSelected = false
	interfaceSelected = false
	nodeInterfaceErrorList := make(map[string][]string)

	nodeList, err := kubeclient.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{
		LabelSelector: labels.Set(cr.Spec.NodeSelector).String(),
	})
	if err != nil {
		return false, err
	}
	nsList := &sriovnetworkv1.SriovNetworkNodeStateList{}
	err = client.List(context.Background(), nsList, &runtimeclient.ListOptions{Namespace: namespace})
	if err != nil {
		return false, err
	}
	npList := &sriovnetworkv1.SriovNetworkNodePolicyList{}
	err = client.List(context.Background(), npList, &runtimeclient.ListOptions{Namespace: namespace})
	if err != nil {
		return false, err
	}
	for _, node := range nodeList.Items {
		if cr.Selected(&node) {
			nodesSelected = true
			err = validatePolicyForNodeStateAndPolicy(nsList, npList, &node, cr, nodeInterfaceErrorList)
			if err != nil {
				return false, err
			}
		}
	}

	if !nodesSelected {
		return false, fmt.Errorf("no matched node is selected by the nodeSelector in CR %s", cr.GetName())
	}
	if !interfaceSelected {
		for nodeName, messages := range nodeInterfaceErrorList {
			for _, message := range messages {
				log.Log.V(2).Info("interface selection errors", "nodeName", nodeName, "message", message)
			}
		}
		return false, fmt.Errorf("no supported NIC is selected by the nicSelector in CR %s", cr.GetName())
	}

	return true, nil
}

func validatePolicyForNodeStateAndPolicy(nsList *sriovnetworkv1.SriovNetworkNodeStateList, npList *sriovnetworkv1.SriovNetworkNodePolicyList, node *corev1.Node, cr *sriovnetworkv1.SriovNetworkNodePolicy, nodeInterfaceErrorList map[string][]string) error {
	for _, ns := range nsList.Items {
		if ns.GetName() == node.GetName() {
			interfaceAndErrorList, err := validatePolicyForNodeState(cr, &ns, node)
			if err != nil {
				return err
			}
			if interfaceAndErrorList != nil {
				nodeInterfaceErrorList[ns.GetName()] = interfaceAndErrorList
			}
			break
		}
	}

	// validate current policy against policies in API (may not be converted to SriovNetworkNodeState yet)
	for _, np := range npList.Items {
		if np.GetName() != cr.GetName() && np.Selected(node) {
			if err := validatePolicyForNodePolicy(cr, &np); err != nil {
				return err
			}
		}
	}
	return nil
}

func validatePolicyForNodeState(policy *sriovnetworkv1.SriovNetworkNodePolicy, state *sriovnetworkv1.SriovNetworkNodeState, node *corev1.Node) ([]string, error) {
	log.Log.V(2).Info("validatePolicyForNodeState(): validate policy for node", "policy-name",
		policy.GetName(), "node-name", state.GetName())
	interfaceSelectedForNode := false
	var noInterfacesSelectedLog []string
	for _, iface := range state.Status.Interfaces {
		err := validateNicModel(&policy.Spec.NicSelector, &iface, node)
		if err == nil {
			interfaceSelected = true
			interfaceSelectedForNode = true
			if policy.GetName() != consts.DefaultPolicyName && policy.Spec.NumVfs == 0 {
				return nil, fmt.Errorf("numVfs(%d) in CR %s is not allowed", policy.Spec.NumVfs, policy.GetName())
			}
			if policy.Spec.NumVfs > iface.TotalVfs && iface.Vendor == IntelID {
				return nil, fmt.Errorf("numVfs(%d) in CR %s exceed the maximum allowed value(%d) interface(%s)", policy.Spec.NumVfs, policy.GetName(), iface.TotalVfs, iface.Name)
			}
			if policy.Spec.NumVfs > MlxMaxVFs && iface.Vendor == MellanoxID {
				return nil, fmt.Errorf("numVfs(%d) in CR %s exceed the maximum allowed value(%d) interface(%s)", policy.Spec.NumVfs, policy.GetName(), MlxMaxVFs, iface.Name)
			}

			// Externally create validations
			if policy.Spec.ExternallyManaged {
				if policy.Spec.NumVfs > iface.NumVfs {
					return nil, fmt.Errorf("numVfs(%d) in CR %s is higher than the virtual functions allocated for the PF externally value(%d)", policy.Spec.NumVfs, policy.GetName(), iface.NumVfs)
				}

				if policy.Spec.Mtu != 0 && policy.Spec.Mtu > iface.Mtu {
					return nil, fmt.Errorf("MTU(%d) in CR %s is higher than the MTU for the PF externally value(%d)", policy.Spec.Mtu, policy.GetName(), iface.Mtu)
				}

				if policy.Spec.LinkType != "" && strings.ToLower(policy.Spec.LinkType) != strings.ToLower(iface.LinkType) {
					return nil, fmt.Errorf("LinkType(%s) in CR %s is not equal to the LinkType for the PF externally value(%s)", policy.Spec.LinkType, policy.GetName(), iface.LinkType)
				}
			}
			// vdpa: only mellanox cards are supported
			if (policy.Spec.VdpaType == consts.VdpaTypeVirtio || policy.Spec.VdpaType == consts.VdpaTypeVhost) && iface.Vendor != MellanoxID {
				return nil, fmt.Errorf("vendor(%s) in CR %s not supported for vdpa interface(%s)", iface.Vendor, policy.GetName(), iface.Name)
			}
		} else {
			errorMessage := fmt.Sprintf("Interface: %s was not selected, since NIC model could not be validated due to the following error: %s \n", iface.Name, err)
			noInterfacesSelectedLog = append(noInterfacesSelectedLog, errorMessage)
		}
	}

	if !interfaceSelectedForNode {
		return noInterfacesSelectedLog, nil
	}
	return nil, nil
}

func validatePolicyForNodePolicy(current *sriovnetworkv1.SriovNetworkNodePolicy, previous *sriovnetworkv1.SriovNetworkNodePolicy) error {
	log.Log.V(2).Info("validateConflictPolicy(): validate policy against policy",
		"source", current.GetName(), "target", previous.GetName())

	if current.GetName() == previous.GetName() {
		return nil
	}

	err := validatePfNames(current, previous)
	if err != nil {
		return err
	}

	err = validateRootDevices(current, previous)
	if err != nil {
		return err
	}

	err = validateExludeTopologyField(current, previous)
	if err != nil {
		return err
	}

	return nil
}

func validatePfNames(current *sriovnetworkv1.SriovNetworkNodePolicy, previous *sriovnetworkv1.SriovNetworkNodePolicy) error {
	for _, curPf := range current.Spec.NicSelector.PfNames {
		curName, curRngSt, curRngEnd, err := sriovnetworkv1.ParseVfRange(curPf)
		if err != nil {
			return fmt.Errorf("invalid PF name: %s", curPf)
		}
		for _, prePf := range previous.Spec.NicSelector.PfNames {
			// Not validate return err for previous PF
			// since it should already be evaluated in previous run.
			preName, preRngSt, preRngEnd, _ := sriovnetworkv1.ParseVfRange(prePf)
			if curName == preName {
				err = validateExternallyManage(current, previous)
				if err != nil {
					return err
				}

				// Check for overlapping ranges
				if curRngEnd < preRngSt || curRngSt > preRngEnd {
					return nil
				} else {
					return fmt.Errorf("VF index range in %s is overlapped with existing policy %s", curPf, previous.GetName())
				}
			}
		}
	}
	return nil
}

func validateRootDevices(current *sriovnetworkv1.SriovNetworkNodePolicy, previous *sriovnetworkv1.SriovNetworkNodePolicy) error {
	for _, curRootDevice := range current.Spec.NicSelector.RootDevices {
		for _, preRootDevice := range previous.Spec.NicSelector.RootDevices {
			// TODO: (SchSeba) implement range for root devices
			if curRootDevice == preRootDevice {
				return fmt.Errorf("root device %s is overlapped with existing policy %s", curRootDevice, previous.GetName())
			}
		}
	}
	return nil
}

func validateExternallyManage(current, previous *sriovnetworkv1.SriovNetworkNodePolicy) error {
	// reject policy with externallyManage if there is a policy on the same PF without it
	if current.Spec.ExternallyManaged != previous.Spec.ExternallyManaged {
		return fmt.Errorf("externallyManage is inconsistent with existing policy %s", previous.GetName())
	}

	return nil
}

func validateExludeTopologyField(current *sriovnetworkv1.SriovNetworkNodePolicy, previous *sriovnetworkv1.SriovNetworkNodePolicy) error {
	if current.Spec.ResourceName != previous.Spec.ResourceName {
		return nil
	}

	if current.Spec.ExcludeTopology == previous.Spec.ExcludeTopology {
		return nil
	}

	return fmt.Errorf("excludeTopology[%t] field conflicts with policy [%s].ExcludeTopology[%t] as they target the same resource[%s]",
		current.Spec.ExcludeTopology, previous.GetName(), previous.Spec.ExcludeTopology, current.Spec.ResourceName)
}

func validateNicModel(selector *sriovnetworkv1.SriovNetworkNicSelector, iface *sriovnetworkv1.InterfaceExt, node *corev1.Node) error {
	if selector.Vendor != "" && selector.Vendor != iface.Vendor {
		return fmt.Errorf("selector vendor: %s is not equal to the interface vendor: %s", selector.Vendor, iface.Vendor)
	}
	if selector.DeviceID != "" && selector.DeviceID != iface.DeviceID {
		return fmt.Errorf("selector device ID: %s is not equal to the interface device ID: %s", selector.Vendor, iface.Vendor)
	}
	if len(selector.RootDevices) > 0 && !sriovnetworkv1.StringInArray(iface.PciAddress, selector.RootDevices) {
		return fmt.Errorf("interface PCI address: %s not found in root devices", iface.PciAddress)
	}
	if len(selector.PfNames) > 0 {
		var pfNames []string
		for _, p := range selector.PfNames {
			if strings.Contains(p, "#") {
				fields := strings.Split(p, "#")
				pfNames = append(pfNames, fields[0])
			} else {
				pfNames = append(pfNames, p)
			}
		}
		if !sriovnetworkv1.StringInArray(iface.Name, pfNames) {
			return fmt.Errorf("interface name: %s not found in physical function names", iface.PciAddress)
		}
	}

	// check the vendor/device ID to make sure only devices in supported list are allowed.
	if sriovnetworkv1.IsSupportedModel(iface.Vendor, iface.DeviceID) {
		return nil
	}

	// Check the vendor and device ID of the VF only if we are on a virtual environment
	for key := range vars.PlatformsMap {
		if strings.Contains(strings.ToLower(node.Spec.ProviderID), strings.ToLower(key)) &&
			selector.NetFilter != "" && selector.NetFilter == iface.NetFilter &&
			sriovnetworkv1.IsVfSupportedModel(iface.Vendor, iface.DeviceID) {
			return nil
		}
	}

	return fmt.Errorf("vendor and device ID is not in supported list")
}
