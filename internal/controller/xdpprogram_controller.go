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
	"os"
	"time"

	networkingv1alpha1 "github.com/River41/xdp-operator/api/v1alpha1"
	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/vishvananda/netlink"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const xdpProgramFinalizer = "networking.zylu.dev/finalizer"

// XdpProgramReconciler reconciles a XdpProgram object
type XdpProgramReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	NodeName string
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
			logger.Info("XdpProgram resource not found. Ignoring since object must be deleted.")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get XdpProgram")
		return ctrl.Result{}, err
	}

	if err := rlimit.RemoveMemlock(); err != nil {
		logger.Error(err, "unable to remove memlock")
		os.Exit(1)
	}

	// 2. Check if this controller should reconcile this resource.
	if xdp.Spec.NodeName != r.NodeName {
		logger.Info("Skipping reconciliation, XdpProgram is not for this node", "XdpProgramNode", xdp.Spec.NodeName, "ThisNode", r.NodeName)
		return ctrl.Result{}, nil
	}

	// 3. Handle finalizer logic for deletion
	if !xdp.DeletionTimestamp.IsZero() {
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

	// 4. Reconcile the desired state
	logger.Info("Reconciling XdpProgram",
		"Name", xdp.Name,
		"Interface", xdp.Spec.Interface,
		"Mode", xdp.Spec.Mode)

	// Check if the network interface exists
	iface, err := netlink.LinkByName(xdp.Spec.Interface)
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
	logger.Info("Found interface", "Index", iface.Attrs().Index)

	// Check if BPF file exists
	if _, err := os.Stat(xdp.Spec.BpfPath); os.IsNotExist(err) {
		logger.Error(err, "BPF object file not found", "path", xdp.Spec.BpfPath)
		xdp.Status.Ready = false
		xdp.Status.Message = fmt.Sprintf("BPF object file not found at %s", xdp.Spec.BpfPath)
		if updateErr := r.Status().Update(ctx, xdp); updateErr != nil {
			logger.Error(updateErr, "Failed to update status for missing BPF file")
			return ctrl.Result{}, updateErr
		}
		// Do not requeue, user must fix the path.
		return ctrl.Result{}, nil
	}

	// 5. Load and attach the XDP program
	if err := r.loadAndAttachXdp(iface, xdp.Spec.BpfPath, xdp.Spec.Mode); err != nil {
		logger.Error(err, "Failed to load and attach XDP program")
		xdp.Status.Ready = false
		xdp.Status.Message = fmt.Sprintf("Failed to load/attach XDP program: %v", err)
		if updateErr := r.Status().Update(ctx, xdp); updateErr != nil {
			logger.Error(updateErr, "Failed to update status after load failure")
			return ctrl.Result{}, updateErr
		}
		return ctrl.Result{}, err
	}

	// 6. Update the Status to reflect success
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

// loadAndAttachXdp loads the XDP program from the specified path and attaches it to the given interface.
// This function is idempotent and will replace an existing program on the interface if necessary.
func (r *XdpProgramReconciler) loadAndAttachXdp(iface netlink.Link, bpfPath, mode string) error {
	// Load the BPF collection spec
	collSpec, err := ebpf.LoadCollectionSpec(bpfPath)
	if err != nil {
		return fmt.Errorf("failed to load BPF collection spec from %s: %w", bpfPath, err)
	}

	// Find the first XDP program in the collection.
	// A more robust solution might involve letting the user specify the program name in the XdpProgramSpec.
	var progSpec *ebpf.ProgramSpec
	for _, p := range collSpec.Programs {
		if p.Type == ebpf.XDP {
			progSpec = p
			break
		}
	}
	if progSpec == nil {
		return fmt.Errorf("no XDP program found in %s", bpfPath)
	}

	// Load the program into the kernel.
	coll, err := ebpf.NewCollection(collSpec)
	if err != nil {
		return fmt.Errorf("failed to create BPF collection: %w", err)
	}
	defer coll.Close()

	xdpProg := coll.Programs[progSpec.Name]
	if xdpProg == nil {
		return fmt.Errorf("program %s not found in collection", progSpec.Name)
	}

	// Determine attach flags from the mode string.
	// The flag constants were introduced in a later version of the netlink library.
	// We use their literal values here for compatibility with older versions.
	// The proper long-term fix is to update the dependency: `go get github.com/vishvananda/netlink@latest`
	const (
		xdpFlagsSkbMode = 1 << 1 // equivalent to netlink.XDP_FLAGS_SKB_MODE
		xdpFlagsDrvMode = 1 << 2 // equivalent to netlink.XDP_FLAGS_DRV_MODE
		xdpFlagsHwMode  = 1 << 3 // equivalent to netlink.XDP_FLAGS_HW_MODE
	)
	var flags int
	switch mode {
	case "generic":
		flags = xdpFlagsSkbMode
	case "native":
		flags = xdpFlagsDrvMode
	case "offload":
		flags = xdpFlagsHwMode
	default:
		return fmt.Errorf("unknown XDP mode: %s", mode)
	}

	// Attach the program using its file descriptor. This will replace any existing program.
	err = netlink.LinkSetXdpFdWithFlags(iface, xdpProg.FD(), flags)
	if err != nil {
		return err
	}

	log.Log.Info("Successfully attached XDP program to kernel", "interface", iface.Attrs().Name, "fd", xdpProg.FD())
	return nil
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
	// In main.go, the reconciler should be initialized with the node name, which can be
	// retrieved from an environment variable.
	//
	// if err := (&XdpProgramReconciler{
	// 	NodeName: os.Getenv("NODE_NAME"),
	// 	...
	// }).SetupWithManager(mgr); err != nil { ... }
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkingv1alpha1.XdpProgram{}).
		Named("xdpprogram").
		Complete(r)
}
