/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"os"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/vishvananda/netlink"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	networkingv1alpha1 "github.com/River41/xdp-operator/api/v1alpha1"
)

var _ = Describe("XdpProgram Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"
		const testNodeName = "test-node"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		xdpprogram := &networkingv1alpha1.XdpProgram{}

		BeforeEach(func() {
			if isCI() {
				Skip("Skipping hardware-dependent tests in GitHub Actions")
			}
			By("creating the custom resource for the Kind XdpProgram")
			err := k8sClient.Get(ctx, typeNamespacedName, xdpprogram)
			if err != nil && errors.IsNotFound(err) {
				resource := &networkingv1alpha1.XdpProgram{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: networkingv1alpha1.XdpProgramSpec{
						Interface: "non-existent-iface",
						BpfPath:   "/tmp/fake.o",
						Mode:      "generic",
						NodeName:  testNodeName,
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &networkingv1alpha1.XdpProgram{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance XdpProgram")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should update status when interface is not found", func() {
			// Set the NODE_NAME env var to match the resource's spec.nodeName
			// This simulates the controller running on the target node.
			Expect(os.Setenv("NODE_NAME", testNodeName)).To(Succeed())
			defer func() { _ = os.Unsetenv("NODE_NAME") }()

			By("Reconciling the created resource")
			controllerReconciler := &XdpProgramReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				NodeName: testNodeName,
			}

			// We expect the reconcile to not return an error, but to update the status.
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Verify the status was updated correctly.
			reconciledXdp := &networkingv1alpha1.XdpProgram{}
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, typeNamespacedName, reconciledXdp); err != nil {
					return false
				}
				return !reconciledXdp.Status.Ready && reconciledXdp.Status.Message == "Interface not found on host"
			}).Should(BeTrue())
		})

		It("should update status when BPF file is not found", func() {
			if isCI() {
				Skip("Skipping this test in GitHub Actions because it needs a real kernel/netlink")
			}
			// This test requires root privileges to create a dummy network interface.
			// We skip it if the test is not run as root.
			if os.Geteuid() != 0 {
				Skip("Skipping test: requires root privileges to create a dummy network interface")
			}

			// Create a dummy interface to get past the first check in the reconciler.
			dummyLinkName := "dummy0"
			dummyLink := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: dummyLinkName}}
			Expect(netlink.LinkAdd(dummyLink)).To(Succeed())
			defer func() { _ = netlink.LinkDel(dummyLink) }()

			// Update the resource to use the dummy interface but a non-existent bpf file.
			xdpToUpdate := &networkingv1alpha1.XdpProgram{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, xdpToUpdate)).To(Succeed())
			xdpToUpdate.Spec.Interface = dummyLinkName
			xdpToUpdate.Spec.BpfPath = "/tmp/this-file-does-not-exist.o"
			Expect(k8sClient.Update(ctx, xdpToUpdate)).To(Succeed())

			// Reconcile.
			controllerReconciler := &XdpProgramReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				NodeName: testNodeName,
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).NotTo(HaveOccurred())

			// Verify the status is updated to reflect the missing BPF file.
			reconciledXdp := &networkingv1alpha1.XdpProgram{}
			Eventually(func() bool {
				if err := k8sClient.Get(ctx, typeNamespacedName, reconciledXdp); err != nil {
					return false
				}
				return !reconciledXdp.Status.Ready && strings.Contains(reconciledXdp.Status.Message, "BPF object file not found")
			}).Should(BeTrue())
		})

		It("should update status when BPF file is invalid", func() {
			if isCI() {
				Skip("Skipping this test in GitHub Actions because it needs a real kernel/netlink")
			}
			if os.Geteuid() != 0 {
				Skip("Skipping test: requires root privileges to create a dummy network interface")
			}

			// Create a dummy interface.
			dummyLinkName := "dummy1"
			dummyLink := &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: dummyLinkName}}
			Expect(netlink.LinkAdd(dummyLink)).To(Succeed())
			defer func() { _ = netlink.LinkDel(dummyLink) }()

			// Create an empty, invalid BPF file.
			invalidBpfFile, err := os.CreateTemp("", "invalid-*.o")
			Expect(err).NotTo(HaveOccurred())
			defer func() { _ = os.Remove(invalidBpfFile.Name()) }()

			// Update the resource to use the dummy interface and the invalid BPF file.
			xdpToUpdate := &networkingv1alpha1.XdpProgram{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, xdpToUpdate)).To(Succeed())
			xdpToUpdate.Spec.Interface = dummyLinkName
			xdpToUpdate.Spec.BpfPath = invalidBpfFile.Name()
			Expect(k8sClient.Update(ctx, xdpToUpdate)).To(Succeed())

			// Reconcile and expect an error because the BPF loading will fail.
			controllerReconciler := &XdpProgramReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), NodeName: testNodeName}
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: typeNamespacedName})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to load BPF collection spec"))
		})
	})
})

func isCI() bool {
	return os.Getenv("GITHUB_ACTIONS") == "true"
}
