/*


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

package controllers

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	elasticscalev1 "backup-operator/api/v1"
)

// BackupDeploymentReconciler reconciles a BackupDeployment object
type BackupDeploymentReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
	Clock
}

type realClock struct {
}

func (_ realClock) Now() time.Time {
	return time.Now()
}

type Clock interface {
	Now() time.Time
}

// +kubebuilder:docs-gen:collapse=Clock

// +kubebuilder:rbac:groups=elasticscale.com.sjtu.cit,resources=backupdeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=elasticscale.com.sjtu.cit,resources=backupdeployments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs/status,verbs=get
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments/status,verbs=get
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services/status,verbs=get
func (r *BackupDeploymentReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	//_ = context.Background()
	//_ = r.Log.WithValues("backupdeployment", req.NamespacedName)

	// your logic here
	ctx := context.Background() //context
	/*
		logging handle 用于记录日志, controller-runtime 通过 logr 库结构化日志记录。
		稍后我们将看到，日志记录通过将 key-value 添加到静态消息中而起作用
		我们可以在 reconcile 方法的顶部提前分配一些 key-value ,以便查找在这个 reconciler 中所有的日志

	*/
	log := r.Log.WithValues("backupdeployment", req.NamespacedName) //logging handle

	var backdeploy elasticscalev1.BackupDeployment
	if err := r.Get(ctx, req.NamespacedName, &backdeploy); err != nil {
		log.Error(err, "unable to fetch BackupDeployment")
		//	// we'll ignore not-found errors, since they can't be fixed by an immediate
		//        // requeue (we'll need to wait for a new notification), and we can get them
		//        // on deleted requests.
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	//if backdeploy.Spec.ServiceName == nil{
	//
	//}
	qpslabels := r.findLabels(ctx,backdeploy)
	initdur := time.Duration(*backdeploy.Spec.InitalWait) * time.Millisecond
	if qpslabels == nil{
		log.V(1).Info("some service are not ready","depend service",fmt.Sprintf("%v",backdeploy.Spec.ServiceName))
		return ctrl.Result{RequeueAfter: initdur*time.Millisecond}, nil
	}



	return ctrl.Result{}, nil
}

func (r *BackupDeploymentReconciler) findLabels(ctx context.Context,bd elasticscalev1.BackupDeployment)(labels map[string]string){
	services := bd.Spec.ServiceName
	labels = make(map[string]string)
	service := new(corev1.Service)
	for _,svc := range services {
		namespacedname := types.NamespacedName{bd.Namespace, svc}
		err := r.Get(ctx,namespacedname,service)
		if err != nil {
			labels = nil
			break
		}else{
			labelselector := service.Spec.Selector
			for k,v := range labelselector{
				labels[k] = v
			}
		}
	}
	return
}

func (r *BackupDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&elasticscalev1.BackupDeployment{}).
		Complete(r)
}
