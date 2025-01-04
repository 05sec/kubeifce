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
	"encoding/json"
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
		log.Info("vlan not belongs to this node", "vlan", vlan.ObjectMeta.Name, "node", r.NodeName)
		return ctrl.Result{}, nil
	}

	log.Info("reconcile Vlan", "vlan", vlan.ObjectMeta.Name, "node", r.NodeName)

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
				return ctrl.Result{RequeueAfter: time.Second * 5}, err
			}

			// remove our finalizer from the list and update it.
			controllerutil.RemoveFinalizer(vlan, finalizerName)
			if err := r.Update(ctx, vlan); err != nil {
				return ctrl.Result{}, err
			}
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
	r.Recorder.Event(vlan, corev1.EventTypeNormal, "CreatedOrUpdatedVlanInterface", "Created/Updated VLAN interface")

	return ctrl.Result{}, nil
}

func (r *VlanReconciler) getNextAvailableVlanID(ctx context.Context, master string) (int, error) {
	cmd := exec.CommandContext(ctx, "ip", "-j", "-d", "link", "show", "type", "vlan")
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("failed to get VLAN interfaces: %v", err)
	}

	type VlanLink struct {
		Ifname   string `json:"ifname"`
		Link     string `json:"link"`
		Linkinfo struct {
			InfoData struct {
				ID int `json:"id"`
			} `json:"info_data"`
		} `json:"linkinfo"`
	}

	var interfaces []VlanLink
	if err := json.Unmarshal(output, &interfaces); err != nil {
		return 0, fmt.Errorf("failed to parse VLAN interfaces: %v", err)
	}

	// 创建已使用ID的map，只统计指定物理接口上的VLAN ID
	usedIDs := make(map[int]bool)
	for _, iface := range interfaces {
		if iface.Link == master {
			usedIDs[iface.Linkinfo.InfoData.ID] = true
		}
	}

	// 从1开始查找第一个未使用的ID
	for id := 1; id <= 4094; id++ {
		if !usedIDs[id] {
			return id, nil
		}
	}

	return 0, fmt.Errorf("no available VLAN IDs for interface %s", master)
}

func (r *VlanReconciler) createOrUpdateVlanInterface(ctx context.Context, vlan *interfacev1.Vlan) error {
	log := log.FromContext(ctx)

	// 将分配的ID保存到annotation
	if vlan.Annotations == nil {
		vlan.Annotations = make(map[string]string)
	}

	// 确定master接口
	master := "eth0"
	if vlan.Spec.Master != nil && *vlan.Spec.Master != "" {
		master = *vlan.Spec.Master
	}
	vlan.Annotations["kubeifce.lwsec.cn/master"] = master

	// 获取实际使用的VLAN ID
	var vlanID int
	const vlanIDAnnotation = "kubeifce.lwsec.cn/vlan-id"

	if vlan.Spec.ID != nil {
		// 如果用户指定了ID，直接使用
		vlanID = *vlan.Spec.ID
	} else {
		// 检查annotation中是否已有分配的ID
		if idStr, exists := vlan.Annotations[vlanIDAnnotation]; exists {
			if _, err := fmt.Sscanf(idStr, "%d", &vlanID); err != nil {
				return fmt.Errorf("invalid vlan id in annotation: %v", err)
			}
		} else {
			// 自动分配新的ID，基于master接口
			id, err := r.getNextAvailableVlanID(ctx, master)
			if err != nil {
				return fmt.Errorf("failed to get next available VLAN ID: %v", err)
			}
			vlanID = id

			vlan.Annotations[vlanIDAnnotation] = fmt.Sprintf("%d", vlanID)

		}
	}
	vlan.Annotations["kubeifce.lwsec.cn/vlan-id"] = fmt.Sprintf("%d", vlanID)

	// Generate interface name if not specified
	if vlan.Spec.Name == nil || *vlan.Spec.Name == "" {
		name := fmt.Sprintf("ki.%s.%d", master, vlanID)
		vlan.Spec.Name = &name
	}
	vlan.Annotations["kubeifce.lwsec.cn/interface-name"] = *vlan.Spec.Name

	// Execute command to create VLAN interface
	cmd := fmt.Sprintf("ip link add link %s name %s type vlan id %d",
		master, *vlan.Spec.Name, vlanID)
	if vlan.Spec.MTU != nil {
		cmd += fmt.Sprintf(" mtu %d", *vlan.Spec.MTU)
	}

	// 直接执行命令
	if out, err := exec.Command("sh", "-c", cmd).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to create VLAN interface: %v, output: %s", err, string(out))
	}

	log.Info("create VLAN interface", "interface", vlan.ObjectMeta.Name, "vlan_id", vlanID, "master", master)

	if err := r.Update(ctx, vlan); err != nil {
		return fmt.Errorf("failed to update VLAN with annotation: %v", err)
	}
	return nil
}

func (r *VlanReconciler) deleteVlanInterface(ctx context.Context, vlan *interfacev1.Vlan) error {
	log := log.FromContext(ctx)

	if vlan.Annotations == nil || vlan.Annotations["kubeifce.lwsec.cn/interface-name"] == "" {
		return nil
	}
	interfaceName := vlan.Annotations["kubeifce.lwsec.cn/interface-name"]

	// Execute command to delete VLAN interface
	cmd := fmt.Sprintf("ip link delete %s", interfaceName)

	// 直接执行命令
	if out, err := exec.Command("sh", "-c", cmd).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to delete VLAN interface: %v, output: %s", err, string(out))
	}

	log.Info("delete VLAN interface", "interface", interfaceName)
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
