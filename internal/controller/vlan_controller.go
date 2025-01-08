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
	"crypto/sha256"
	"fmt"
	"strings"
	"time"

	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	interfacev1 "github.com/05sec/kubeifce/api/v1"
	"github.com/05sec/kubeifce/pkg/netifce"
)

const (
	InterfaceNameAnnotation = "kubeifce.lwsec.cn/interface-name"
	VlanIDAnnotation        = "kubeifce.lwsec.cn/vlan-id"
	VlanMasterAnnotation    = "kubeifce.lwsec.cn/vlan-master"
)

// VlanReconciler reconciles a Vlan object
type VlanReconciler struct {
	client.Client
	Scheme      *runtime.Scheme
	Recorder    record.EventRecorder
	NodeName    string
	VlanManager netifce.VlanManager
}

var defaultVlanConf = netifce.Vlan{
	Master: "eth0",
	Id:     1,
	MTU:    1496,
}

// +kubebuilder:rbac:groups=interface.kubeifce.lwsec.cn,resources=vlans,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=interface.kubeifce.lwsec.cn,resources=vlans/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=interface.kubeifce.lwsec.cn,resources=vlans/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *VlanReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// 获取CRD中所有vlan配置
	var crdVlans interfacev1.VlanList
	if err := r.List(ctx, &crdVlans); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to list VLAN CRDs: %v", err)
	}

	// 获取节点上实际存在的vlan接口
	nodeVlans, err := r.VlanManager.List(ctx)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get node VLAN interfaces: %v", err)
	}

	finalizerName := "vlan.interface.kubeifce.lwsec.cn/finalizer"

	// 同步CRD和实际接口状态
	// 存在几种情况，vlan信息更新了，如vlan配置的名称改变，vlan配置的ID改变，vlan配置的MTU改变，vlan配置的master改变，vlan配置的nodeName改变
	// 当名称改变时，只能删除接口并重新创建
	// 创建CRD中有但节点上没有的接口
	for _, crdVlan := range crdVlans.Items {
		if crdVlan.Spec.NodeName != r.NodeName {
			continue
		}

		if crdVlan.ObjectMeta.DeletionTimestamp.IsZero() {
			if !controllerutil.ContainsFinalizer(&crdVlan, finalizerName) {
				crdVlan.ObjectMeta.Finalizers = append(crdVlan.ObjectMeta.Finalizers, finalizerName)
				if err := r.Update(ctx, &crdVlan); err != nil {
					log.Error(err, "failed to add finalizer")
					return ctrl.Result{RequeueAfter: time.Second * 5}, err
				}
			}
		} else {
			if controllerutil.ContainsFinalizer(&crdVlan, finalizerName) {
				r.Recorder.Event(&crdVlan, corev1.EventTypeNormal, "DeletingVlanInterface", "Deleting VLAN interface")

				err = r.VlanManager.Delete(ctx, crdVlan.Annotations[InterfaceNameAnnotation])
				if err != nil {
					r.Recorder.Event(&crdVlan, corev1.EventTypeWarning, "FailedDeletingVlanInterface", err.Error())
					log.Error(err, "failed to delete VLAN interface")
					return ctrl.Result{RequeueAfter: time.Second * 5}, err
				}

				// remove our finalizer from the list and update it.
				controllerutil.RemoveFinalizer(&crdVlan, finalizerName)
				if err := r.Update(ctx, &crdVlan); err != nil {
					return ctrl.Result{}, err
				}
			}
			continue
		}

		crdVlanConv := defaultVlanConf
		if crdVlan.Spec.Master != nil {
			crdVlanConv.Master = *crdVlan.Spec.Master
		}
		if crdVlan.Spec.ID != nil {
			crdVlanConv.Id = *crdVlan.Spec.ID
		} else {
			// 自动分配新的ID，基于master接口
			id, err := r.getNextAvailableVlanID(ctx, crdVlanConv.Master)
			if err != nil {
				log.Error(err, "failed to get next available VLAN ID")
				continue
			}
			crdVlanConv.Id = id
		}
		if crdVlan.Spec.MTU != nil {
			crdVlanConv.MTU = *crdVlan.Spec.MTU
		}
		crdVlanConv.Name = fmt.Sprintf("ki.%s.%d", fmt.Sprintf("%x", sha256.Sum256([]byte(crdVlanConv.Master)))[:7], crdVlanConv.Id)

		// 如果创建过接口，则跳过
		if lo.SomeBy(nodeVlans, func(nodeVlan *netifce.Vlan) bool {
			return crdVlanConv.Name == nodeVlan.Name
		}) {
			continue
		}

		// 跳过冲突的接口
		if lo.SomeBy(nodeVlans, func(nodeVlan *netifce.Vlan) bool {
			return crdVlanConv.Id == nodeVlan.Id && crdVlanConv.Master == nodeVlan.Master
		}) {
			log.Error(err, "conflict vlan interface", "interface", crdVlanConv.Name)
			r.Recorder.Event(&crdVlan, corev1.EventTypeWarning, "ConflictVlanInterface", "Conflict VLAN interface")
			continue
		}

		log.Info("create VLAN interface", "interface", crdVlanConv.Name)
		err = r.VlanManager.Create(ctx, &crdVlanConv)
		if err != nil {
			log.Error(err, "failed to create VLAN interface")
			r.Recorder.Event(&crdVlan, corev1.EventTypeWarning, "FailedCreatedVlanInterface", err.Error())
			continue
		}
		r.Recorder.Event(&crdVlan, corev1.EventTypeNormal, "CreatedVlanInterface", fmt.Sprintf("Created VLAN interface %s", crdVlanConv.Name))

		crdVlan.Annotations[InterfaceNameAnnotation] = crdVlanConv.Name
		crdVlan.Annotations[VlanIDAnnotation] = fmt.Sprintf("%d", crdVlanConv.Id)
		crdVlan.Annotations[VlanMasterAnnotation] = crdVlanConv.Master
		if err = r.Update(ctx, &crdVlan); err != nil {
			return ctrl.Result{}, err
		}
	}

	// 删除节点上有但CRD中没有的ki.开头接口
	for _, nodeVlan := range nodeVlans {
		if !strings.HasPrefix(nodeVlan.Name, "ki.") {
			continue
		}
		if lo.SomeBy(crdVlans.Items, func(crdVlan interfacev1.Vlan) bool {
			return crdVlan.Spec.NodeName == r.NodeName && crdVlan.Annotations[InterfaceNameAnnotation] == nodeVlan.Name
		}) {
			continue
		}

		if err = r.VlanManager.Delete(ctx, nodeVlan.Name); err != nil {
			log.Error(err, "failed to delete VLAN interface")
			return ctrl.Result{RequeueAfter: time.Second * 5}, err
		}
	}
	return ctrl.Result{}, nil
}

func (r *VlanReconciler) getNextAvailableVlanID(ctx context.Context, master string) (int, error) {
	intfList, err := r.VlanManager.List(ctx)
	if err != nil {
		return 0, err
	}
	intfList = lo.Filter(intfList, func(intf *netifce.Vlan, _ int) bool { return intf.Master == master })

	usedIDs := lo.SliceToMap(intfList, func(intf *netifce.Vlan) (int, struct{}) {
		return intf.Id, struct{}{}
	})
	for id := 1; id <= 4094; id++ {
		if _, ok := usedIDs[id]; !ok {
			return id, nil
		}
	}
	return 0, fmt.Errorf("no available VLAN IDs for interface %s", master)
}

// SetupWithManager sets up the controller with the Manager.
func (r *VlanReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&interfacev1.Vlan{}).
		Named("vlan").
		Complete(r)
}
