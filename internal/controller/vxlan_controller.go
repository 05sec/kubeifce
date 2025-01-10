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

	"github.com/05sec/kubeifce/pkg/netifce"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	interfacev1 "github.com/05sec/kubeifce/api/v1"
)

// VxlanReconciler reconciles a Vxlan object
type VxlanReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	Recorder     record.EventRecorder
	NodeName     string
	VxlanManager netifce.VxlanManager
}

var defaultVxlanConf = netifce.Vxlan{
	Master: "eth0",
	VNI:    1000,
	MTU:    1450,
	Port:   4789,
}

// +kubebuilder:rbac:groups=interface.kubeifce.lwsec.cn,resources=vxlans,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=interface.kubeifce.lwsec.cn,resources=vxlans/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=interface.kubeifce.lwsec.cn,resources=vxlans/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *VxlanReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	log.Info("Reconcile started", "namespace", req.Namespace, "name", req.Name)

	// 获取CRD中所有vxlan配置
	var crdVxlans interfacev1.VxlanList
	log.Info("list VXLAN CRDs")
	if err := r.List(ctx, &crdVxlans); err != nil {
		log.Error(err, "failed to list VXLAN CRDs", "namespace", req.Namespace, "name", req.Name)
		return ctrl.Result{RequeueAfter: time.Second * 5}, fmt.Errorf("failed to list VXLAN CRDs: %v", err)
	}
	log.Info("list VXLAN CRDs completed", "count", len(crdVxlans.Items), "node", r.NodeName)

	// 获取节点上实际存在的vxlan接口
	log.Info("get node VXLAN interfaces")
	nodeVxlans, err := r.VxlanManager.List(ctx)
	if err != nil {
		log.Error(err, "failed to get node VXLAN interfaces", "node", r.NodeName)
		return ctrl.Result{}, fmt.Errorf("failed to get node VXLAN interfaces: %v", err)
	}
	log.Info("get node VXLAN interfaces completed", "count", len(nodeVxlans), "node", r.NodeName)

	// finalizerName := "vxlan.interface.kubeifce.lwsec.cn/finalizer"

	// 同步CRD和实际接口状态
	for _, crdVxlan := range crdVxlans.Items {
		if !crdVxlan.ObjectMeta.DeletionTimestamp.IsZero() {
			// if controllerutil.ContainsFinalizer(&crdVxlan, finalizerName) {
			//	log.Info("handle VXLAN interface delete")
			//	r.Recorder.Event(&crdVxlan, corev1.EventTypeNormal, "DeletingVxlanInterface", "Deleting VXLAN interface")
			//
			//	err = r.VxlanManager.Delete(ctx, crdVxlan.Annotations[InterfaceNameAnnotation])
			//	if err != nil {
			//		r.Recorder.Event(&crdVxlan, corev1.EventTypeWarning, "FailedDeletingVxlanInterface", err.Error())
			//		log.Error(err, "failed to delete VXLAN interface")
			//		return ctrl.Result{RequeueAfter: time.Second * 5}, err
			//	}
			//
			//	// remove our finalizer from the list and update it.
			//	controllerutil.RemoveFinalizer(&crdVxlan, finalizerName)
			//	if err := r.Update(ctx, &crdVxlan); err != nil {
			//		return ctrl.Result{}, err
			//	}
			// }
			continue
		}

		crdVxlanConv := defaultVxlanConf
		if crdVxlan.Spec.Master != nil {
			crdVxlanConv.Master = *crdVxlan.Spec.Master
		}
		if crdVxlan.Spec.VNI > 0 {
			crdVxlanConv.VNI = crdVxlan.Spec.VNI
		} else {
			// 自动分配新的VNI
			vni, err := r.getNextAvailableVNI(ctx, crdVxlanConv.Master)
			if err != nil {
				log.Error(err, "failed to get next available VNI")
				continue
			}
			crdVxlanConv.VNI = vni
		}
		if crdVxlan.Spec.MTU != nil {
			crdVxlanConv.MTU = *crdVxlan.Spec.MTU
		}
		if crdVxlan.Spec.GroupIP != nil {
			crdVxlanConv.Group = crdVxlan.Spec.GroupIP
		}
		if crdVxlan.Spec.RemoteIP != nil {
			crdVxlanConv.Remote = crdVxlan.Spec.RemoteIP
		}
		if crdVxlan.Spec.LocalIP != nil {
			crdVxlanConv.Local = crdVxlan.Spec.LocalIP
		}
		if crdVxlan.Spec.TTL != nil {
			crdVxlanConv.TTL = *crdVxlan.Spec.TTL
		}
		if crdVxlan.Spec.Port != nil {
			crdVxlanConv.Port = *crdVxlan.Spec.Port
		}
		crdVxlanConv.Name = fmt.Sprintf("ki.%s.%d", fmt.Sprintf("%x", sha256.Sum256([]byte(crdVxlanConv.Master)))[:4], crdVxlanConv.VNI)

		// 如果创建过接口，则跳过
		if lo.SomeBy(nodeVxlans, func(nodeVxlan *netifce.Vxlan) bool {
			return crdVxlanConv.Name == nodeVxlan.Name
		}) {
			continue
		}

		// 跳过冲突的接口
		if lo.SomeBy(nodeVxlans, func(nodeVxlan *netifce.Vxlan) bool {
			return crdVxlanConv.VNI == nodeVxlan.VNI && crdVxlanConv.Master == nodeVxlan.Master
		}) {
			log.Error(err, "conflict vxlan interface", "interface", crdVxlanConv.Name)
			r.Recorder.Event(&crdVxlan, corev1.EventTypeWarning, "ConflictVxlanInterface", "Conflict VXLAN interface")
			continue
		}

		log.Info("create VXLAN interface", "interface", crdVxlanConv.Name)
		err = r.VxlanManager.Create(ctx, &crdVxlanConv)
		if err != nil {
			log.Error(err, "failed to create VXLAN interface")
			r.Recorder.Event(&crdVxlan, corev1.EventTypeWarning, "FailedCreatedVxlanInterface", err.Error())
			continue
		}
		r.Recorder.Event(&crdVxlan, corev1.EventTypeNormal, "CreatedVxlanInterface", fmt.Sprintf("Created VXLAN interface %s", crdVxlanConv.Name))

		if crdVxlan.Annotations == nil {
			crdVxlan.Annotations = make(map[string]string)
		}
		crdVxlan.Annotations[InterfaceNameAnnotation] = crdVxlanConv.Name
		crdVxlan.Annotations[VxlanVNIAnnotation] = fmt.Sprintf("%d", crdVxlanConv.VNI)
		crdVxlan.Annotations[VxlanMasterAnnotation] = crdVxlanConv.Master
		// 创建成功后再加finalizer，因为涉及到多节点，所以不配置finalizer，要做好多节点资源状态管理才能做好资源删除
		// if !controllerutil.ContainsFinalizer(&crdVxlan, finalizerName) {
		//	crdVxlan.ObjectMeta.Finalizers = append(crdVxlan.ObjectMeta.Finalizers, finalizerName)
		//	log.Info("finalizer added", "vxlan", crdVxlan.Name)
		// }
		if err = r.Update(ctx, &crdVxlan); err != nil {
			return ctrl.Result{RequeueAfter: time.Second * 5}, err
		}
		log.Info("create VXLAN interface completed", "interface", crdVxlanConv.Name)
	}

	// 删除节点上有但CRD中没有的ki.开头接口
	for _, nodeVxlan := range nodeVxlans {
		if !strings.HasPrefix(nodeVxlan.Name, "ki.") {
			continue
		}
		if lo.SomeBy(crdVxlans.Items, func(crdVxlan interfacev1.Vxlan) bool {
			return crdVxlan.Annotations[InterfaceNameAnnotation] == nodeVxlan.Name
		}) {
			continue
		}

		if err = r.VxlanManager.Delete(ctx, nodeVxlan.Name); err != nil {
			log.Error(err, "failed to delete VXLAN interface")
			return ctrl.Result{RequeueAfter: time.Second * 5}, err
		}
	}
	return ctrl.Result{RequeueAfter: time.Minute * 1}, nil
}

func (r *VxlanReconciler) getNextAvailableVNI(ctx context.Context, master string) (int, error) {
	intfList, err := r.VxlanManager.List(ctx)
	if err != nil {
		return 0, err
	}
	intfList = lo.Filter(intfList, func(intf *netifce.Vxlan, _ int) bool { return intf.Master == master })

	usedVNIs := lo.SliceToMap(intfList, func(intf *netifce.Vxlan) (int, struct{}) {
		return intf.VNI, struct{}{}
	})
	for vni := 1000; vni <= 16777215; vni++ {
		if _, ok := usedVNIs[vni]; !ok {
			return vni, nil
		}
	}
	return 0, fmt.Errorf("no available VNIs for interface %s", master)
}

// SetupWithManager sets up the controller with the Manager.
func (r *VxlanReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&interfacev1.Vxlan{}).
		Named("vxlan").
		Complete(r)
}
