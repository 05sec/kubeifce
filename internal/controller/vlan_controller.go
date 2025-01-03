/*
Copyright 2025.

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
	"os/exec"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	interfacev1 "github.com/05sec/kubeifce/api/v1"
)

// VlanReconciler reconciles a Vlan object
type VlanReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	NodeName string
}

// +kubebuilder:rbac:groups=interface.kubeifce.lwsec.cn,resources=vlans,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=interface.kubeifce.lwsec.cn,resources=vlans/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=interface.kubeifce.lwsec.cn,resources=vlans/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *VlanReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Fetch the Vlan instance
	vlan := &interfacev1.Vlan{}
	if err := r.Get(ctx, req.NamespacedName, vlan); err != nil {
		if client.IgnoreNotFound(err) == nil {
			// Object not found, return
			return ctrl.Result{}, nil
		}
		log.Error(err, "unable to fetch Vlan")
		return ctrl.Result{}, err
	}

	// check if the vlan belongs to this node
	if vlan.Spec.NodeName != r.NodeName {
		log.Info("vlan not belongs to this node", "vlan", vlan.Spec.Name, "node", r.NodeName)
		return ctrl.Result{}, nil
	}

	log.Info("reconcile Vlan", "vlan", vlan.Spec.Name, "node", r.NodeName)

	// Check if the object is being deleted

	finalizerName := "vlan.interface.kubeifce.lwsec.cn/finalizer"
	// 如果对象还没被删除且没有设定finalizer 则进行设定
	if vlan.ObjectMeta.DeletionTimestamp.IsZero() {
		if !controllerutil.ContainsFinalizer(vlan, finalizerName) {
			vlan.ObjectMeta.Finalizers = append(vlan.ObjectMeta.Finalizers, finalizerName)
			if err := r.Update(ctx, vlan); err != nil {
				log.Error(err, "failed to add finalizer")
				return ctrl.Result{RequeueAfter: time.Second * 5}, err
			}
		}
	} else {
		if controllerutil.ContainsFinalizer(vlan, finalizerName) {
			r.Recorder.Event(vlan, corev1.EventTypeNormal, "DeletingVlanInterface", "Deleting VLAN interface")
			// Handle deletion
			if err := r.deleteVlanInterface(ctx, vlan); err != nil {
				r.Recorder.Event(vlan, corev1.EventTypeWarning, "FailedDeletingVlanInterface", err.Error())
				log.Error(err, "failed to delete VLAN interface")
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
		// 如果对象被删除则停止后续步骤
		return ctrl.Result{}, nil
	}

	// Create or update VLAN interface
	if err := r.createOrUpdateVlanInterface(ctx, vlan); err != nil {
		r.Recorder.Event(vlan, corev1.EventTypeWarning, "FailedCreateOrUpdateVlanInterface", err.Error())
		log.Error(err, "failed to create/update VLAN interface")
		return ctrl.Result{RequeueAfter: time.Second * 10}, err
	}

	// Update status
	if err := r.updateStatus(ctx, vlan); err != nil {
		r.Recorder.Event(vlan, corev1.EventTypeWarning, "FailedUpdateStatus", err.Error())
		log.Error(err, "failed to update status")
		return ctrl.Result{RequeueAfter: time.Second * 5}, err
	}

	return ctrl.Result{}, nil
}

func (r *VlanReconciler) createOrUpdateVlanInterface(ctx context.Context, vlan *interfacev1.Vlan) error {
	log := log.FromContext(ctx)

	// Generate interface name if not specified
	if vlan.Spec.Name == nil || *vlan.Spec.Name == "" {
		name := fmt.Sprintf("ki.%s.%d", *vlan.Spec.Master, *vlan.Spec.ID)
		vlan.Spec.Name = &name
	}

	// Execute command to create VLAN interface
	cmd := fmt.Sprintf("ip link add link %s name %s type vlan id %d",
		*vlan.Spec.Master, *vlan.Spec.Name, *vlan.Spec.ID)
	if vlan.Spec.MTU != nil {
		cmd += fmt.Sprintf(" mtu %d", *vlan.Spec.MTU)
	}

	// 直接执行命令
	if out, err := exec.Command("sh", "-c", cmd).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create VLAN interface: %v, output: %s", err, string(out))
	}

	log.Info("create VLAN interface", "interface", *vlan.Spec.Name)
	return nil
}

func (r *VlanReconciler) deleteVlanInterface(ctx context.Context, vlan *interfacev1.Vlan) error {
	log := log.FromContext(ctx)

	if vlan.Spec.Name == nil {
		return nil
	}

	// Execute command to delete VLAN interface
	cmd := fmt.Sprintf("ip link delete %s", *vlan.Spec.Name)

	// 直接执行命令
	if out, err := exec.Command("sh", "-c", cmd).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to delete VLAN interface: %v, output: %s", err, string(out))
	}

	log.Info("delete VLAN interface", "interface", *vlan.Spec.Name)
	return nil
}

func (r *VlanReconciler) updateStatus(ctx context.Context, vlan *interfacev1.Vlan) error {
	// TODO: Check actual interface status and update
	vlan.Status.Name = *vlan.Spec.Name
	vlan.Status.State = "up"

	if err := r.Status().Update(ctx, vlan); err != nil {
		return err
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *VlanReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&interfacev1.Vlan{}).
		Named("vlan").
		Complete(r)
}
