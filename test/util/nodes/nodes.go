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

package nodes

import (
	"context"
	"fmt"
	"os"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	sriovv1 "github.com/k8snetworkplumbingwg/sriov-network-operator/api/v1"
	"github.com/k8snetworkplumbingwg/sriov-network-operator/test/util/client"
)

// NodesSelector represent the label selector used to filter impacted nodes.
var NodesSelector string

func init() {
	NodesSelector = os.Getenv("NODES_SELECTOR")
}

// MatchingOptionalSelectorByName filter the given slice with only the nodes matching the optional selector.
// If no selector is set, it returns the same list.
// The NODES_SELECTOR must be in the form of label=value.
// For example: NODES_SELECTOR="sctp=true"
func MatchingOptionalSelectorState(clients *client.ClientSet, toFilter []sriovv1.SriovNetworkNodeState) ([]sriovv1.SriovNetworkNodeState, error) {
	if NodesSelector == "" {
		return toFilter, nil
	}
	toMatch, err := clients.CoreV1Interface.Nodes().List(context.Background(), metav1.ListOptions{
		LabelSelector: NodesSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("error in getting nodes matching %s, %v", NodesSelector, err)
	}
	if len(toMatch.Items) == 0 {
		return nil, fmt.Errorf("failed to get nodes matching %s, %v", NodesSelector, err)
	}

	res := make([]sriovv1.SriovNetworkNodeState, 0)
	for _, n := range toFilter {
		for _, m := range toMatch.Items {
			if n.Name == m.Name {
				res = append(res, n)
			}
		}
	}
	if len(res) == 0 {
		return nil, fmt.Errorf("failed to find matching nodes with %s", NodesSelector)
	}
	return res, nil
}

// MatchingOptionalSelector filter the given slice with only the nodes matching the optional selector.
// If no selector is set, it returns the same list.
// The NODES_SELECTOR must be set with a labelselector expression.
// For example: NODES_SELECTOR="sctp=true"
func MatchingOptionalSelector(clients *client.ClientSet, toFilter []corev1.Node) ([]corev1.Node, error) {
	if NodesSelector == "" {
		return toFilter, nil
	}
	toMatch, err := clients.CoreV1Interface.Nodes().List(context.Background(), metav1.ListOptions{
		LabelSelector: NodesSelector,
	})
	if err != nil {
		return nil, fmt.Errorf("error in getting nodes matching the %s label selector, %v", NodesSelector, err)
	}
	if len(toMatch.Items) == 0 {
		return nil, fmt.Errorf("failed to get nodes matching %s label selector", NodesSelector)
	}

	res := make([]corev1.Node, 0)
	for _, n := range toFilter {
		for _, m := range toMatch.Items {
			if n.Name == m.Name {
				res = append(res, n)
				break
			}
		}
	}
	if len(res) == 0 {
		return nil, fmt.Errorf("failed to find matching nodes with %s label selector", NodesSelector)
	}
	return res, nil
}
