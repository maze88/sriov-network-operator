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

package k8sreporter

import (
	"errors"
	"os"
	"strings"

	kniK8sReporter "github.com/openshift-kni/k8sreporter"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"

	sriovv1 "github.com/k8snetworkplumbingwg/sriov-network-operator/api/v1"
	"github.com/k8snetworkplumbingwg/sriov-network-operator/test/util/namespaces"
)

func New(reportPath string) (*kniK8sReporter.KubernetesReporter, error) {
	addToScheme := func(s *runtime.Scheme) error {
		err := sriovv1.AddToScheme(s)
		if err != nil {
			return err
		}

		err = monitoringv1.AddToScheme(s)
		if err != nil {
			return err
		}

		err = rbacv1.AddToScheme(s)
		if err != nil {
			return err
		}

		return nil
	}

	operatorNamespace := os.Getenv("OPERATOR_NAMESPACE")
	if operatorNamespace == "" {
		operatorNamespace = "openshift-sriov-network-operator"
	}

	multusNamespace := os.Getenv("MULTUS_NAMESPACE")

	dumpNamespace := func(ns string) bool {
		switch {
		case ns == namespaces.Test:
			return true
		case ns == operatorNamespace:
			return true
		case strings.HasPrefix(ns, "sriov-"):
			return true
		case multusNamespace != "" && ns == multusNamespace:
			return true
		case ns == "openshift-monitoring":
			return true
		}
		return false
	}

	crds := []kniK8sReporter.CRData{
		{Cr: &sriovv1.SriovNetworkNodeStateList{}},
		{Cr: &sriovv1.SriovNetworkNodePolicyList{}},
		{Cr: &sriovv1.SriovNetworkList{}},
		{Cr: &sriovv1.SriovOperatorConfigList{}},
		{Cr: &sriovv1.SriovNetworkPoolConfigList{}},
		{Cr: &monitoringv1.ServiceMonitorList{}, Namespace: &operatorNamespace},
		{Cr: &monitoringv1.PrometheusRuleList{}, Namespace: &operatorNamespace},
		{Cr: &rbacv1.RoleList{}, Namespace: &operatorNamespace},
		{Cr: &rbacv1.RoleBindingList{}, Namespace: &operatorNamespace},
	}

	err := os.Mkdir(reportPath, 0755)
	if err != nil && !errors.Is(err, os.ErrExist) {
		return nil, err
	}

	reporter, err := kniK8sReporter.New("", addToScheme, dumpNamespace, reportPath, crds...)
	if err != nil {
		return nil, err
	}
	return reporter, nil
}
