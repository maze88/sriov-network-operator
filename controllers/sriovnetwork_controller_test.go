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

package controllers

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	netattdefv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	dynclient "sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	sriovnetworkv1 "github.com/k8snetworkplumbingwg/sriov-network-operator/api/v1"
	"github.com/k8snetworkplumbingwg/sriov-network-operator/test/util"
)

const (
	on         = "on"
	emptyCurls = "{}"
)

var _ = Describe("SriovNetwork Controller", Ordered, func() {
	var cancel context.CancelFunc
	var ctx context.Context

	BeforeAll(func() {
		By("Setup controller manager")
		k8sManager, err := setupK8sManagerForTest()
		Expect(err).ToNot(HaveOccurred())

		err = (&SriovNetworkReconciler{
			Client: k8sManager.GetClient(),
			Scheme: k8sManager.GetScheme(),
		}).SetupWithManager(k8sManager)
		Expect(err).ToNot(HaveOccurred())

		ctx, cancel = context.WithCancel(context.Background())

		wg := sync.WaitGroup{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer GinkgoRecover()
			By("Start controller manager")
			err := k8sManager.Start(ctx)
			Expect(err).ToNot(HaveOccurred())
		}()

		DeferCleanup(func() {
			By("Shutdown controller manager")
			cancel()
			wg.Wait()
		})
	})

	Context("with SriovNetwork", func() {
		specs := map[string]sriovnetworkv1.SriovNetworkSpec{
			"test-0": {
				ResourceName: "resource_1",
				IPAM:         `{"type":"host-local","subnet":"10.56.217.0/24","rangeStart":"10.56.217.171","rangeEnd":"10.56.217.181","routes":[{"dst":"0.0.0.0/0"}],"gateway":"10.56.217.1"}`,
				Vlan:         100,
				VlanQoS:      5,
				VlanProto:    "802.1ad",
			},
			"test-1": {
				ResourceName:     "resource_1",
				IPAM:             `{"type":"host-local","subnet":"10.56.217.0/24","rangeStart":"10.56.217.171","rangeEnd":"10.56.217.181","routes":[{"dst":"0.0.0.0/0"}],"gateway":"10.56.217.1"}`,
				NetworkNamespace: "default",
			},
			"test-2": {
				ResourceName: "resource_1",
				IPAM:         `{"type":"host-local","subnet":"10.56.217.0/24","rangeStart":"10.56.217.171","rangeEnd":"10.56.217.181","routes":[{"dst":"0.0.0.0/0"}],"gateway":"10.56.217.1"}`,
				SpoofChk:     on,
			},
			"test-3": {
				ResourceName: "resource_1",
				IPAM:         `{"type":"host-local","subnet":"10.56.217.0/24","rangeStart":"10.56.217.171","rangeEnd":"10.56.217.181","routes":[{"dst":"0.0.0.0/0"}],"gateway":"10.56.217.1"}`,
				Trust:        on,
			},
			"test-4": {
				ResourceName: "resource_1",
				IPAM:         `{"type":"host-local","subnet":"10.56.217.0/24","rangeStart":"10.56.217.171","rangeEnd":"10.56.217.181","routes":[{"dst":"0.0.0.0/0"}],"gateway":"10.56.217.1"}`,
			},
			"test-5": {
				ResourceName: "resource_1",
				IPAM:         `{"type":"host-local","subnet":"10.56.217.0/24","rangeStart":"10.56.217.171","rangeEnd":"10.56.217.181","routes":[{"dst":"0.0.0.0/0"}],"gateway":"10.56.217.1"}`,
				LogLevel:     "debug",
				LogFile:      "/tmp/tmpfile",
			},
		}
		sriovnets := util.GenerateSriovNetworkCRs(testNamespace, specs)
		DescribeTable("should be possible to create/delete net-att-def",
			func(cr sriovnetworkv1.SriovNetwork) {
				var err error
				expect := generateExpectedNetConfig(&cr)

				By("Create the SriovNetwork Custom Resource")
				// get global framework variables
				err = k8sClient.Create(ctx, &cr)
				Expect(err).NotTo(HaveOccurred())
				ns := testNamespace
				if cr.Spec.NetworkNamespace != "" {
					ns = cr.Spec.NetworkNamespace
				}
				netAttDef := &netattdefv1.NetworkAttachmentDefinition{}
				err = util.WaitForNamespacedObject(netAttDef, k8sClient, ns, cr.GetName(), util.RetryInterval, util.Timeout)
				Expect(err).NotTo(HaveOccurred())
				anno := netAttDef.GetAnnotations()

				Expect(anno["k8s.v1.cni.cncf.io/resourceName"]).To(Equal("openshift.io/" + cr.Spec.ResourceName))
				Expect(strings.TrimSpace(netAttDef.Spec.Config)).To(Equal(expect))

				By("Delete the SriovNetwork Custom Resource")
				found := &sriovnetworkv1.SriovNetwork{}
				err = k8sClient.Get(ctx, types.NamespacedName{Namespace: cr.GetNamespace(), Name: cr.GetName()}, found)
				Expect(err).NotTo(HaveOccurred())
				err = k8sClient.Delete(ctx, found, []dynclient.DeleteOption{}...)
				Expect(err).NotTo(HaveOccurred())

				netAttDef = &netattdefv1.NetworkAttachmentDefinition{}
				err = util.WaitForNamespacedObjectDeleted(netAttDef, k8sClient, ns, cr.GetName(), util.RetryInterval, util.Timeout)
				Expect(err).NotTo(HaveOccurred())
			},
			Entry("with vlan, vlanQoS and vlanProto flag", sriovnets["test-0"]),
			Entry("with networkNamespace flag", sriovnets["test-1"]),
			Entry("with SpoofChk flag on", sriovnets["test-2"]),
			Entry("with Trust flag on", sriovnets["test-3"]),
			Entry("with LogLevel and LogFile", sriovnets["test-5"]),
		)

		newSpecs := map[string]sriovnetworkv1.SriovNetworkSpec{
			"new-0": {
				ResourceName: "resource_1",
				IPAM:         `{"type":"dhcp"}`,
				Vlan:         200,
				VlanProto:    "802.1q",
			},
			"new-1": {
				ResourceName: "resource_1",
				IPAM:         `{"type":"host-local","subnet":"10.56.217.0/24","rangeStart":"10.56.217.171","rangeEnd":"10.56.217.181","routes":[{"dst":"0.0.0.0/0"}],"gateway":"10.56.217.1"}`,
			},
			"new-2": {
				ResourceName: "resource_1",
				IPAM:         `{"type":"host-local","subnet":"10.56.217.0/24","rangeStart":"10.56.217.171","rangeEnd":"10.56.217.181","routes":[{"dst":"0.0.0.0/0"}],"gateway":"10.56.217.1"}`,
				SpoofChk:     on,
			},
			"new-3": {
				ResourceName: "resource_1",
				IPAM:         `{"type":"host-local","subnet":"10.56.217.0/24","rangeStart":"10.56.217.171","rangeEnd":"10.56.217.181","routes":[{"dst":"0.0.0.0/0"}],"gateway":"10.56.217.1"}`,
				Trust:        on,
			},
		}
		newsriovnets := util.GenerateSriovNetworkCRs(testNamespace, newSpecs)

		DescribeTable("should be possible to update net-att-def",
			func(old, new sriovnetworkv1.SriovNetwork) {
				old.Name = new.GetName()
				err := k8sClient.Create(ctx, &old)
				defer func() {
					// Cleanup the test resource
					Expect(k8sClient.Delete(ctx, &old)).To(Succeed())
				}()
				Expect(err).NotTo(HaveOccurred())
				found := &sriovnetworkv1.SriovNetwork{}
				expect := generateExpectedNetConfig(&new)

				retryErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
					// Retrieve the latest version of SriovNetwork before attempting update
					// RetryOnConflict uses exponential backoff to avoid exhausting the apiserver
					getErr := k8sClient.Get(ctx, types.NamespacedName{Namespace: old.GetNamespace(), Name: old.GetName()}, found)
					if getErr != nil {
						io.WriteString(GinkgoWriter, fmt.Sprintf("Failed to get latest version of SriovNetwork: %v", getErr))
					}
					found.Spec = new.Spec
					found.Annotations = new.Annotations
					updateErr := k8sClient.Update(ctx, found)
					if getErr != nil {
						io.WriteString(GinkgoWriter, fmt.Sprintf("Failed to update latest version of SriovNetwork: %v", getErr))
					}
					return updateErr
				})
				if retryErr != nil {
					Fail(fmt.Sprintf("Update failed: %v", retryErr))
				}

				ns := testNamespace
				if new.Spec.NetworkNamespace != "" {
					ns = new.Spec.NetworkNamespace
				}

				time.Sleep(time.Second * 2)
				netAttDef := &netattdefv1.NetworkAttachmentDefinition{}
				err = util.WaitForNamespacedObject(netAttDef, k8sClient, ns, old.GetName(), util.RetryInterval, util.Timeout)
				Expect(err).NotTo(HaveOccurred())
				anno := netAttDef.GetAnnotations()

				Expect(anno["k8s.v1.cni.cncf.io/resourceName"]).To(Equal("openshift.io/" + new.Spec.ResourceName))
				Expect(strings.TrimSpace(netAttDef.Spec.Config)).To(Equal(expect))
			},
			Entry("with vlan and proto flag and ipam updated", sriovnets["test-4"], newsriovnets["new-0"]),
			Entry("with networkNamespace flag", sriovnets["test-4"], newsriovnets["new-1"]),
			Entry("with SpoofChk flag on", sriovnets["test-4"], newsriovnets["new-2"]),
			Entry("with Trust flag on", sriovnets["test-4"], newsriovnets["new-3"]),
		)

		Context("When a derived net-att-def CR is removed", func() {
			It("should regenerate the net-att-def CR", func() {
				cr := sriovnetworkv1.SriovNetwork{
					TypeMeta: metav1.TypeMeta{
						Kind:       "SriovNetwork",
						APIVersion: "sriovnetwork.openshift.io/v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-5",
						Namespace: testNamespace,
					},
					Spec: sriovnetworkv1.SriovNetworkSpec{
						NetworkNamespace: "default",
						ResourceName:     "resource_1",
						IPAM:             `{"type":"dhcp"}`,
						Vlan:             200,
					},
				}
				var err error
				expect := generateExpectedNetConfig(&cr)

				err = k8sClient.Create(ctx, &cr)
				Expect(err).NotTo(HaveOccurred())
				ns := testNamespace
				if cr.Spec.NetworkNamespace != "" {
					ns = cr.Spec.NetworkNamespace
				}
				netAttDef := &netattdefv1.NetworkAttachmentDefinition{}
				err = util.WaitForNamespacedObject(netAttDef, k8sClient, ns, cr.GetName(), util.RetryInterval, util.Timeout)
				Expect(err).NotTo(HaveOccurred())

				err = k8sClient.Delete(ctx, netAttDef)
				Expect(err).NotTo(HaveOccurred())
				time.Sleep(3 * time.Second)
				err = util.WaitForNamespacedObject(netAttDef, k8sClient, ns, cr.GetName(), util.RetryInterval, util.Timeout)
				Expect(err).NotTo(HaveOccurred())
				anno := netAttDef.GetAnnotations()
				Expect(anno["k8s.v1.cni.cncf.io/resourceName"]).To(Equal("openshift.io/" + cr.Spec.ResourceName))
				Expect(strings.TrimSpace(netAttDef.Spec.Config)).To(Equal(expect))

				found := &sriovnetworkv1.SriovNetwork{}
				err = k8sClient.Get(ctx, types.NamespacedName{Namespace: cr.GetNamespace(), Name: cr.GetName()}, found)
				Expect(err).NotTo(HaveOccurred())
				err = k8sClient.Delete(ctx, found, []dynclient.DeleteOption{}...)
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("When the target NetworkNamespace doesn't exists", func() {
			It("should create the NetAttachDef when the namespace is created", func() {
				cr := sriovnetworkv1.SriovNetwork{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-missing-namespace",
						Namespace: testNamespace,
					},
					Spec: sriovnetworkv1.SriovNetworkSpec{
						NetworkNamespace: "ns-xxx",
						ResourceName:     "resource_missing_namespace",
						IPAM:             `{"type":"dhcp"}`,
						Vlan:             200,
					},
				}
				var err error
				expect := generateExpectedNetConfig(&cr)

				err = k8sClient.Create(ctx, &cr)
				Expect(err).NotTo(HaveOccurred())

				DeferCleanup(func() {
					err = k8sClient.Delete(ctx, &cr)
					Expect(err).NotTo(HaveOccurred())
				})

				// Sleep 3 seconds to be sure the Reconcile loop has been invoked. This can be improved by exposing some information (e.g. the error)
				// in the SriovNetwork.Status field.
				time.Sleep(3 * time.Second)

				netAttDef := &netattdefv1.NetworkAttachmentDefinition{}
				err = k8sClient.Get(ctx, types.NamespacedName{Name: cr.GetName(), Namespace: "ns-xxx"}, netAttDef)
				Expect(err).To(HaveOccurred())

				// Create Namespace
				nsObj := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: "ns-xxx"},
				}
				err = k8sClient.Create(ctx, nsObj)
				Expect(err).NotTo(HaveOccurred())
				DeferCleanup(func() {
					err = k8sClient.Delete(ctx, nsObj)
					Expect(err).NotTo(HaveOccurred())
				})

				// Check that net-attach-def has been created
				err = util.WaitForNamespacedObject(netAttDef, k8sClient, "ns-xxx", cr.GetName(), util.RetryInterval, util.Timeout)
				Expect(err).NotTo(HaveOccurred())

				anno := netAttDef.GetAnnotations()
				Expect(anno["k8s.v1.cni.cncf.io/resourceName"]).To(Equal("openshift.io/" + cr.Spec.ResourceName))
				Expect(strings.TrimSpace(netAttDef.Spec.Config)).To(Equal(expect))
			})
		})

		It("should preserve user defined annotations", func() {
			cr := sriovnetworkv1.SriovNetwork{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-annotations",
					Namespace:   testNamespace,
					Annotations: map[string]string{"foo": "bar"},
				},
				Spec: sriovnetworkv1.SriovNetworkSpec{
					NetworkNamespace: "default",
				},
			}

			err := k8sClient.Create(ctx, &cr)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(k8sClient.Delete, ctx, &cr)

			Eventually(func(g Gomega) {
				network := &sriovnetworkv1.SriovNetwork{}
				err = k8sClient.Get(ctx, types.NamespacedName{Name: cr.GetName(), Namespace: testNamespace}, network)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(network.Annotations).To(HaveKeyWithValue("foo", "bar"))
				g.Expect(network.Annotations).To(HaveKeyWithValue("operator.sriovnetwork.openshift.io/last-network-namespace", "default"))
			}).
				WithPolling(100 * time.Millisecond).
				WithTimeout(5 * time.Second).
				MustPassRepeatedly(10).
				Should(Succeed())
		})
	})
})

func generateExpectedNetConfig(cr *sriovnetworkv1.SriovNetwork) string {
	spoofchk := ""
	trust := ""
	vlanProto := ""
	logLevel := `"logLevel":"info",`
	logFile := ""
	ipam := emptyCurls

	if cr.Spec.Trust == sriovnetworkv1.SriovCniStateOn {
		trust = `"trust":"on",`
	} else if cr.Spec.Trust == sriovnetworkv1.SriovCniStateOff {
		trust = `"trust":"off",`
	}

	if cr.Spec.SpoofChk == sriovnetworkv1.SriovCniStateOn {
		spoofchk = `"spoofchk":"on",`
	} else if cr.Spec.SpoofChk == sriovnetworkv1.SriovCniStateOff {
		spoofchk = `"spoofchk":"off",`
	}

	state := getLinkState(cr.Spec.LinkState)

	if cr.Spec.IPAM != "" {
		ipam = cr.Spec.IPAM
	}
	vlanQoS := cr.Spec.VlanQoS

	if cr.Spec.VlanProto != "" {
		vlanProto = fmt.Sprintf(`"vlanProto": "%s",`, cr.Spec.VlanProto)
	}
	if cr.Spec.LogLevel != "" {
		logLevel = fmt.Sprintf(`"logLevel":"%s",`, cr.Spec.LogLevel)
	}
	if cr.Spec.LogFile != "" {
		logFile = fmt.Sprintf(`"logFile":"%s",`, cr.Spec.LogFile)
	}

	configStr, err := formatJSON(fmt.Sprintf(
		`{ "cniVersion":"1.0.0", "name":"%s","type":"sriov","vlan":%d,%s%s"vlanQoS":%d,%s%s%s%s"ipam":%s }`,
		cr.GetName(), cr.Spec.Vlan, spoofchk, trust, vlanQoS, vlanProto, state, logLevel, logFile, ipam))
	if err != nil {
		panic(err)
	}
	return configStr
}
