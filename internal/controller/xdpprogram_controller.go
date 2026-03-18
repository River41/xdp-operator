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
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	networkingv1alpha1 "github.com/River41/xdp-operator/api/v1alpha1"
)

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
	// Initialize the logger with the request context
	logger := log.FromContext(ctx)

	// 1. Fetch the XdpProgram instance from the Kubernetes API
	// We use a pointer to an empty struct to hold the data we retrieve
	xdp := &networkingv1alpha1.XdpProgram{}
	if err := r.Get(ctx, req.NamespacedName, xdp); err != nil {
		if errors.IsNotFound(err) {
			// The resource was deleted. We should stop reconciliation.
			// In a real eBPF operator, you might trigger a cleanup (unmount) here.
			logger.Info("XdpProgram resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request to try again later
		logger.Error(err, "Failed to get XdpProgram")
		return ctrl.Result{}, err
	}

	// 2. Logic: Validate the desired state
	// Even though we have CRD markers, it's good practice to log what we're doing
	logger.Info("Reconciling XdpProgram",
		"Name", xdp.Name,
		"Interface", xdp.Spec.Interface,
		"Mode", xdp.Spec.Mode)

	// 3. Logic: Check if the eBPF program is already loaded (Idempotency)
	// For now, we simulate the "Action" part.
	// Later, you will replace this with actual Netlink or cilium/ebpf calls.
	isLoaded := r.checkIfAlreadyLoaded(xdp.Spec.Interface)

	if !isLoaded {
		logger.Info("Loading XDP program onto interface", "interface", xdp.Spec.Interface)

		// Simulate loading logic
		// err := r.loadXdp(xdp.Spec.Interface, xdp.Spec.BpfPath, xdp.Spec.Mode)
		// if err != nil { ... }
	} else {
		logger.Info("XDP program already present, skipping attachment", "interface", xdp.Spec.Interface)
	}

	// 4. Update the Status of our Custom Resource
	// This tells the user (and kubectl) that the work is done
	if !xdp.Status.Ready {
		xdp.Status.Ready = true
		// Use RFC3339 format for standard Kubernetes timestamps
		xdp.Status.AttachedAt = time.Now().Format(time.RFC3339)
		xdp.Status.Message = "XDP program successfully reconciled"

		if err := r.Status().Update(ctx, xdp); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Return a successful result to stop the current loop
	return ctrl.Result{}, nil
}

// Helper function to check the current state of the system
func (r *XdpProgramReconciler) checkIfAlreadyLoaded(iface string) bool {
	// TODO: Use netlink to check if an XDP program is attached to the interface
	return false
}

// SetupWithManager sets up the controller with the Manager.
func (r *XdpProgramReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkingv1alpha1.XdpProgram{}).
		Named("xdpprogram").
		Complete(r)
}
