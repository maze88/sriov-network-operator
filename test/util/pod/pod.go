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

package pod

import (
	"bytes"
	"context"
	"io"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/utils/pointer"

	testclient "github.com/k8snetworkplumbingwg/sriov-network-operator/test/util/client"
	"github.com/k8snetworkplumbingwg/sriov-network-operator/test/util/images"
	"github.com/k8snetworkplumbingwg/sriov-network-operator/test/util/namespaces"
)

const hostnameLabel = "kubernetes.io/hostname"

func GetDefinition() *corev1.Pod {
	podObject := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "testpod-",
			Namespace:    namespaces.Test},
		Spec: corev1.PodSpec{
			TerminationGracePeriodSeconds: pointer.Int64Ptr(0),
			Containers: []corev1.Container{{Name: "test",
				Image: images.Test(),
				SecurityContext: &corev1.SecurityContext{
					Capabilities: &corev1.Capabilities{
						Add: []corev1.Capability{"NET_RAW"},
					}},
				Command: []string{"/bin/bash", "-c", "sleep INF"}}}}}

	return podObject
}

func DefineWithNetworks(networks []string) *corev1.Pod {
	podObject := GetDefinition()
	podObject.Annotations = map[string]string{"k8s.v1.cni.cncf.io/networks": strings.Join(networks, ",")}

	return podObject
}

func DefineWithHostNetwork(nodeName string) *corev1.Pod {
	podObject := GetDefinition()
	podObject.Spec.HostNetwork = true
	podObject.Spec.NodeSelector = map[string]string{
		"kubernetes.io/hostname": nodeName,
	}

	return podObject
}

// RedefineAsPrivileged updates the pod to be privileged
func RedefineAsPrivileged(pod *corev1.Pod) *corev1.Pod {
	pod.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{}
	b := true
	pod.Spec.Containers[0].SecurityContext.Privileged = &b
	return pod
}

// RedefineWithHostNetwork updates the pod definition Spec.HostNetwork to true
func RedefineWithHostNetwork(pod *corev1.Pod) *corev1.Pod {
	pod.Spec.HostNetwork = true
	return pod
}

// RedefineWithNodeSelector updates the pod definition with a node selector
func RedefineWithNodeSelector(pod *corev1.Pod, node string) *corev1.Pod {
	pod.Spec.NodeSelector = map[string]string{
		hostnameLabel: node,
	}
	return pod
}

// RedefineWithMount updates the pod definition with a volume and volume mount
func RedefineWithMount(pod *corev1.Pod, volume corev1.Volume, mount corev1.VolumeMount) *corev1.Pod {
	pod.Spec.Containers[0].VolumeMounts = []corev1.VolumeMount{mount}
	pod.Spec.Volumes = []corev1.Volume{volume}

	return pod
}

// RedefineWithCommand updates the pod definition with a different command
func RedefineWithCommand(pod *corev1.Pod, command []string, args []string) *corev1.Pod {
	pod.Spec.Containers[0].Command = command
	pod.Spec.Containers[0].Args = args
	return pod
}

// RedefineWithRestartPolicy updates the pod definition with a restart policy
func RedefineWithRestartPolicy(pod *corev1.Pod, restartPolicy corev1.RestartPolicy) *corev1.Pod {
	pod.Spec.RestartPolicy = restartPolicy
	return pod
}

func RedefineWithCapabilities(pod *corev1.Pod, capabilitiesList []corev1.Capability) *corev1.Pod {
	pod.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{Capabilities: &corev1.Capabilities{Add: capabilitiesList}}
	return pod
}

// ExecCommand runs command in the pod and returns buffer output
func ExecCommand(cs *testclient.ClientSet, pod *corev1.Pod, command ...string) (string, string, error) {
	var buf, errbuf bytes.Buffer
	req := cs.CoreV1Interface.RESTClient().
		Post().
		Namespace(pod.Namespace).
		Resource("pods").
		Name(pod.Name).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: pod.Spec.Containers[0].Name,
			Command:   command,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(cs.Config, "POST", req.URL())
	if err != nil {
		return buf.String(), errbuf.String(), err
	}

	err = exec.StreamWithContext(context.Background(), remotecommand.StreamOptions{
		Stdout: &buf,
		Stderr: &errbuf,
	})
	if err != nil {
		return buf.String(), errbuf.String(), err
	}

	return buf.String(), errbuf.String(), nil
}

// GetLog connects to a pod and fetches log
func GetLog(cs *testclient.ClientSet, p *corev1.Pod, s time.Duration) (string, error) {
	logStart := int64(s.Seconds())
	req := cs.Pods(p.Namespace).GetLogs(p.Name, &corev1.PodLogOptions{SinceSeconds: &logStart})
	log, err := req.Stream(context.Background())
	if err != nil {
		return "", err
	}
	defer log.Close()

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, log)

	if err != nil {
		return "", err
	}

	return buf.String(), nil
}
