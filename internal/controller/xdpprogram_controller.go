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
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	networkingv1alpha1 "github.com/River41/xdp-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/vishvananda/netlink"
)

const xdpProgramFinalizer = "networking.zylu.dev/finalizer"

// XdpProgramReconciler reconciles a XdpProgram object
type XdpProgramReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=networking.zylu.dev,resources=xdpprograms,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.zylu.dev,resources=xdpprograms/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=networking.zylu.dev,resources=xdpprograms/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the XdpProgram object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.23.1/pkg/reconcile
func (r *XdpProgramReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// 1. Fetch the XdpProgram instance
	xdp := &networkingv1alpha1.XdpProgram{}
	if err := r.Get(ctx, req.NamespacedName, xdp); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("XdpProgram resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get XdpProgram")
		return ctrl.Result{}, err
	}

	// 2. Handle finalizer logic for deletion
	// WARNING: The logic to filter reconciliation to a specific node has been removed.
	// If this controller is deployed as a DaemonSet, every pod will attempt to reconcile
	// this resource, leading to race conditions and incorrect status updates.
	// This controller will now attempt to reconcile the resource on whatever node it is running on,
	// which may not match the `spec.nodeName` field.
	if !xdp.ObjectMeta.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(xdp, xdpProgramFinalizer) {
			logger.Info("Performing cleanup for XdpProgram")
			if err := r.unloadXdp(xdp.Spec.Interface); err != nil {
				// If cleanup fails, we don't remove the finalizer so we can retry.
				logger.Error(err, "Failed to unload XDP program during cleanup")
				return ctrl.Result{}, err
			}

			logger.Info("XDP program successfully detached", "interface", xdp.Spec.Interface)

			// Remove the finalizer. Once all finalizers are removed, the object will be deleted.
			controllerutil.RemoveFinalizer(xdp, xdpProgramFinalizer)
			if err := r.Update(ctx, xdp); err != nil {
				logger.Error(err, "Failed to remove finalizer")
				return ctrl.Result{}, err
			}
		}
		// Stop reconciliation as the item is being deleted
		return ctrl.Result{}, nil
	}

	// Add finalizer for this CR if it doesn't exist. This allows us to handle cleanup.
	if !controllerutil.ContainsFinalizer(xdp, xdpProgramFinalizer) {
		controllerutil.AddFinalizer(xdp, xdpProgramFinalizer)
		if err := r.Update(ctx, xdp); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
	}

	// 3. Reconcile the desired state
	logger.Info("Reconciling XdpProgram",
		"Name", xdp.Name,
		"Interface", xdp.Spec.Interface,
		"Mode", xdp.Spec.Mode)

	// Check if the network interface exists
	link, err := netlink.LinkByName(xdp.Spec.Interface)
	if err != nil {
		logger.Error(err, "Unable to find interface", "interface", xdp.Spec.Interface)
		xdp.Status.Ready = false
		xdp.Status.Message = "Interface not found on host"
		if updateErr := r.Status().Update(ctx, xdp); updateErr != nil {
			logger.Error(updateErr, "Failed to update status for missing interface")
			return ctrl.Result{}, updateErr
		}
		// Do not requeue, as the interface is missing. The user must fix the spec.
		return ctrl.Result{}, nil
	}
	logger.Info("Found interface", "Index", link.Attrs().Index)

	// 4. Logic: Check if the eBPF program is already loaded (Idempotency)
	// This is a placeholder for actual state checking.
	isLoaded, err := r.checkIfAlreadyLoaded(link)
	if err != nil {
		logger.Error(err, "Failed to check if XDP program is loaded")
		// Requeue to try again
		return ctrl.Result{}, err
	}

	if !isLoaded {
		logger.Info("Loading XDP program onto interface", "interface", xdp.Spec.Interface)
		// Simulate loading logic. In a real implementation, you would:
		// - Load the eBPF object file (xdp.Spec.BpfPath)
		// - Attach the program to the interface (link) with the specified mode (xdp.Spec.Mode)
		// err := r.loadXdp(xdp.Spec.Interface, xdp.Spec.BpfPath, xdp.Spec.Mode)
		// if err != nil { ... }
	} else {
		logger.Info("XDP program already present, skipping attachment", "interface", xdp.Spec.Interface)
	}

	// 5. Update the Status to reflect success
	// We only update if something has changed to avoid unnecessary writes.
	// If the status is not yet "Ready", we'll update it.
	if !xdp.Status.Ready || xdp.Status.Message != "XDP program successfully reconciled" {
		xdp.Status.Ready = true
		xdp.Status.Message = "XDP program successfully reconciled"
		now := metav1.NewTime(time.Now())
		xdp.Status.LoadedAt = &now

		logger.Info("Updating status to reflect successful reconciliation")
		if err := r.Status().Update(ctx, xdp); err != nil {
			logger.Error(err, "Failed to update XdpProgram status after reconciliation")
			return ctrl.Result{}, err
		}
	}

	// Return a successful result to stop the current loop
	return ctrl.Result{}, nil
}

// Helper function to check the current state of the system
func (r *XdpProgramReconciler) checkIfAlreadyLoaded(link netlink.Link) (bool, error) {
	// The Xdp info is part of the link attributes.
	// We need to re-fetch the link to get the most up-to-date attributes,
	// including any attached XDP program info.
	freshLink, err := netlink.LinkByIndex(link.Attrs().Index)
	if err != nil {
		return false, fmt.Errorf("failed to get link by index %d: %w", link.Attrs().Index, err)
	}

	attrs := freshLink.Attrs()
	if attrs.Xdp != nil && attrs.Xdp.Attached {
		return true, nil
	}
	return false, nil
}

// unloadXdp detaches an XDP program from a given interface.
func (r *XdpProgramReconciler) unloadXdp(ifaceName string) error {
	link, err := netlink.LinkByName(ifaceName)
	if err != nil {
		// If the interface is already gone, we can consider the cleanup successful.
		return client.IgnoreNotFound(err)
	}

	// Setting fd to -1 detaches the program.
	return netlink.LinkSetXdpFd(link, -1)
}

// SetupWithManager sets up the controller with the Manager.
func (r *XdpProgramReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkingv1alpha1.XdpProgram{}).
		Named("xdpprogram").
		Complete(r)
}
