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
	"strings"
	"time"

	v1 "github.com/05sec/kubeifce/api/ifce/v1"
	"github.com/05sec/kubeifce/pkg/netifce"
	"github.com/hashicorp/go-multierror"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// BridgeReconciler reconciles a Bridge object
type BridgeReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	Recorder      record.EventRecorder
	NodeName      string
	BridgeManager netifce.BridgeManager
}

// +kubebuilder:rbac:groups=interface.kubeifce.lwsec.cn,resources=bridges,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=interface.kubeifce.lwsec.cn,resources=bridges/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=interface.kubeifce.lwsec.cn,resources=bridges/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.1/pkg/reconcile
func (r *BridgeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	log.Info("Reconcile started", "namespace", req.Namespace, "name", req.Name)

	var crdBridgeList v1.BridgeList
	log.Info("list Bridge CRDs")
	if err := r.List(ctx, &crdBridgeList); err != nil {
		log.Error(err, "failed to list Bridge CRDs")
		return ctrl.Result{RequeueAfter: time.Second * 5}, fmt.Errorf("failed to list Bridge CRDs: %v", err)
	}

	log.Info("list Bridge CRDs completed", "count", len(crdBridgeList.Items))
	bridges := lo.Filter(crdBridgeList.Items, func(item v1.Bridge, index int) bool {
		return len(item.Spec.NodeNames) == 0 || lo.Contains(item.Spec.NodeNames, r.NodeName)
	})
	log.Info("list Bridge CRDs filtered", "count", len(bridges))

	hostBriges, err := r.BridgeManager.List(ctx)
	if err != nil {
		log.Error(err, "failed to list host bridges")
		return ctrl.Result{RequeueAfter: time.Second * 5}, fmt.Errorf("failed to list host bridges: %v", err)
	}
	hostBridgeMap := lo.KeyBy(hostBriges, func(item *netifce.Bridge) string {
		return item.Name
	})
	// 找到本节点多余的桥接接口和需要创建的桥接接口
	var errs *multierror.Error
	for _, bridge := range bridges {
		bridgeName := BridgeName(bridge.Name)
		if hostBr, ok := hostBridgeMap[bridgeName]; !ok {
			// 创建桥接接口
			if err = r.BridgeManager.Reconcile(ctx, &netifce.Bridge{
				Name:       bridgeName,
				SlaveNames: bridge.Spec.SlaverNames,
			}); err != nil {
				errs = multierror.Append(errs, err)
			}
		} else {
			// 判断是否一致，不一致则开始协调
			if diff1, diff2 := lo.Difference(hostBr.SlaveNames, bridge.Spec.SlaverNames); len(diff1)+len(diff2) > 0 {
				if err = r.BridgeManager.Reconcile(ctx, &netifce.Bridge{
					Name:       bridgeName,
					SlaveNames: bridge.Spec.SlaverNames,
				}); err != nil {
					errs = multierror.Append(errs, err)
				}
			}
			// 从map中剔除需要的接口，剩下的就是多余的接口/非kubeifce管理的接口
			delete(hostBridgeMap, bridgeName)
		}
	}
	// 删除多余的桥接接口
	for _, hostBridge := range hostBridgeMap {
		if !strings.HasPrefix(hostBridge.Name, "ki.") {
			// 非kubeifce管理
			continue
		}
		if err = r.BridgeManager.Delete(ctx, hostBridge.Name); err != nil {
			errs = multierror.Append(errs, err)
		}
	}
	if errs.ErrorOrNil() != nil {
		log.Error(errs.ErrorOrNil(), "failed to reconcile Bridge CRDs")
		return ctrl.Result{RequeueAfter: time.Second * 5}, errs.ErrorOrNil()
	}
	return ctrl.Result{}, nil
}

func BridgeName(originName string) string {
	return "ki." + originName
}

// SetupWithManager sets up the controller with the Manager.
func (r *BridgeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.Bridge{}).
		Named("bridge").
		Complete(r)
}
