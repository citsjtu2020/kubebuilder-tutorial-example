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

// +kubebuilder:docs-gen:collapse=Apache License

package controllers

import (
	"context"
	"fmt"
	"k8s.io/utils/pointer"

	//"github.com/prometheus/common/log"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	elasticwebv1 "elastic-demo/api/v1"
	appv1 "k8s.io/api/apps/v1"
)

// ElasticWebReconciler reconciles a ElasticWeb object
type ElasticWebReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

var APP_NAME = "elasticapp"
var jobOwnerKey = ".metadata.controller"
var apiGVstr = elasticwebv1.GroupVersion.String()
//const apiGVStr                = elasticwebv1.GroupVersion.String()

// +kubebuilder:rbac:groups=elasticweb.elastic.demo.io,resources=elasticwebs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=elasticweb.elastic.demo.io,resources=elasticwebs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs/status,verbs=get
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments/status,verbs=get
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services/status,verbs=get

func (r *ElasticWebReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	// 会用到context
	ctx := context.Background()
	log := r.Log.WithValues("elasticweb", req.NamespacedName)

	// your logic here
	log.Info("1. start reconcile logic")

	// // 实例化数据结构
	instance := &elasticwebv1.ElasticWeb{}

	// 通过客户端工具查询，查询条件是
	err := r.Get(ctx, req.NamespacedName, instance)

	if err != nil {
		// 如果没有实例，就返回空结果，这样外部就不再立即调用Reconcile方法了

		if errors.IsNotFound(err) {
			log.Info("2.1. instance not found, maybe removed")
			return ctrl.Result{}, nil
		}

		log.Error(err, "2.2 Error")
		// 返回错误信息给外部
		return ctrl.Result{}, err
	}

	log.Info("3. Instance: " + instance.String())
	// 查找deployment
	deploy := &appv1.Deployment{}

	// 用客户端工具查询
	err = r.Get(ctx, req.NamespacedName, deploy)
	// 查找时发生异常，以及查出来没有结果的处理逻辑
	if err != nil {
		// 如果没有实例就要创建了
		if errors.IsNotFound(err) {
			log.Info("4. deployment not exists")

			// 如果对QPS没有需求，此时又没有deployment，就啥事都不做了
			if (*instance.Spec.TotalQPS) < 1 {
				log.Info("5.1 not need deployment")
				// 返回
				return ctrl.Result{}, nil
			}
			if err1 := createServiceIfNotExists(ctx, r, instance, req); err1 != nil {
				log.Error(err1, "5.2 error: when create service")
				// 返回错误信息给外部
				return ctrl.Result{}, err1
			}

			// 立即创建deployment
			if err = createDeployment(ctx, r, instance, req); err != nil {
				log.Error(err, "5.3 error: when create deployment")
				// 返回错误信息给外部
				return ctrl.Result{}, err
			}

			// 如果创建成功就更新状态
			if err = updateStatus(ctx, r, instance, req); err != nil {
				log.Error(err, "5.4. error: when update status of new deployment")
				// 返回错误信息给外部
				return ctrl.Result{}, err
			}

			// 创建成功就可以返回了
			return ctrl.Result{}, nil

		} else {
			log.Error(err, "7. error: some other errors")
			// 返回错误信息给外部
			return ctrl.Result{}, err
		}
	}

	// 如果查到了deployment，并且没有返回错误，就走下面的逻辑

	// 根据单QPS和总QPS计算期望的副本数
	expectReplicas := getExpectReplicas(instance)

	// 当前deployment的期望副本数
	realReplicas := *deploy.Spec.Replicas
	log.Info(fmt.Sprintf("9. expectReplicas [%d], realReplicas [%d]", expectReplicas, realReplicas))

	if expectReplicas == realReplicas {
		log.Info("10. return now")
		return ctrl.Result{}, nil
	}

	// 如果不等，就要调整
	*(deploy.Spec.Replicas) = expectReplicas

	// 通过客户端更新deployment
	if err = r.Update(ctx, deploy); err != nil {
		log.Error(err, "12. update deployment replicas error")
		// 返回错误信息给外部
		return ctrl.Result{}, err
	}

	log.Info("13. update status")

	// 如果更新deployment的Replicas成功，就更新状态
	if err = updateStatus(ctx, r, instance, req); err != nil {
		log.Error(err, "14. update status error")
		// 返回错误信息给外部
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *ElasticWebReconciler) SetupWithManager(mgr ctrl.Manager) error {
	extractValue1 := func(rawObj runtime.Object) []string {
		service := rawObj.(*corev1.Service)
		owner := metav1.GetControllerOf(service)
		if owner == nil {
			return nil
		}
		// ...make sure it's a CronJob...
		if owner.APIVersion != apiGVstr || owner.Kind != "ElasticWeb" {
			return nil
		}
		// ...and if so, return it
		return []string{owner.Name}
	}

	extractValue2 := func(rawObj runtime.Object) []string {
		service := rawObj.(*corev1.Service)
		owner := metav1.GetControllerOf(service)
		if owner == nil {
			return nil
		}
		// ...make sure it's a CronJob...
		if owner.APIVersion != apiGVstr || owner.Kind != "ElasticWeb" {
			return nil
		}
		// ...and if so, return it
		return []string{owner.Name}
	}

	if err := mgr.GetFieldIndexer().IndexField(&corev1.Service{}, jobOwnerKey, extractValue1); err != nil {
		return err
	}

	if err := mgr.GetFieldIndexer().IndexField(&appv1.Deployment{}, jobOwnerKey, extractValue2); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&elasticwebv1.ElasticWeb{}).
		Owns(&corev1.Service{}).Owns(&appv1.Deployment{}).
		Complete(r)
}

// 新建service
func createServiceIfNotExists(ctx context.Context, r *ElasticWebReconciler, elasticWeb *elasticwebv1.ElasticWeb, req ctrl.Request) error {
	log := r.Log.WithValues("func", "createService")

	service := &corev1.Service{}
	err := r.Get(ctx, req.NamespacedName, service)

	// 如果查询结果没有错误，证明service正常，就不做任何操作
	if err == nil {
		log.Info("service exists")
		return nil
	}

	// 如果错误不是NotFound，就返回错误
	if !errors.IsNotFound(err) {
		log.Error(err, "query service error")
		return err
	}

	// 实例化一个数据结构
	service = &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: elasticWeb.Namespace,
			Name:      elasticWeb.Name,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{Name: "http",
					Port:     8080,
					NodePort: *elasticWeb.Spec.Port,
				},
			},
			Selector: map[string]string{
				"app": elasticWeb.Name,
			},
			Type: corev1.ServiceTypeNodePort,
		},
	}
	// 这一步非常关键！
	// 建立关联后，删除elasticweb资源时就会将deployment也删除掉
	log.Info("set reference")
	if err := ctrl.SetControllerReference(elasticWeb, service, r.Scheme); err != nil {
		log.Error(err, "SetControllerReference error")
		return err
	}

	// 创建service
	log.Info("start create service")
	if err := r.Create(ctx, service); err != nil {
		log.Error(err, "create service error")
		return err
	}

	log.Info("create service success")
	return nil
}

// +kubebuilder:docs-gen:collapse=createServiceIfNotExists

func createDeployment(ctx context.Context, r *ElasticWebReconciler, elasticWeb *elasticwebv1.ElasticWeb, req ctrl.Request) error {
	log := r.Log.WithValues("func", "createDeployment")

	// 计算期望的pod数量
	expectReplicas := getExpectReplicas(elasticWeb)

	log.Info(fmt.Sprintf("expectReplicas [%d]", expectReplicas))

	// 实例化一个数据结构
	deploy := &appv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: elasticWeb.Namespace,
			Name:      elasticWeb.Name,
		},
		Spec: appv1.DeploymentSpec{
			// 副本数是计算出来的
			Replicas: pointer.Int32Ptr(expectReplicas),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": elasticWeb.Name,
				},
			},

			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": elasticWeb.Name,
					},
				},

				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: APP_NAME,
							// 用指定的镜像
							Image:           elasticWeb.Spec.Image,
							ImagePullPolicy: "IfNotPresent",
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									Protocol:      corev1.ProtocolSCTP,
									ContainerPort: *elasticWeb.Spec.ContainerPort,
								},
							},
							//Resources:
						},
					},
				},
			},
		},
	}
	//deploy.Spec.Template.Spec.Containers[0].Resources
	notnil := elasticwebv1.IsResourceEmpty(elasticWeb.Spec.Resource)
	reuqest_bool := elasticWeb.Spec.Resource.Requests != nil && len(elasticWeb.Spec.Resource.Requests) > 0
	limit_bool := elasticWeb.Spec.Resource.Limits != nil && len(elasticWeb.Spec.Resource.Limits) > 0
	if notnil {
		deploy.Spec.Template.Spec.Containers[0].Resources = *elasticWeb.Spec.Resource.DeepCopy()
	}

	if reuqest_bool {
		for k, v := range elasticWeb.Spec.Resource.Requests {
			deploy.Spec.Template.Spec.Containers[0].Resources.Requests[k] = v
		}
	}
	if limit_bool {
		for k, v := range elasticWeb.Spec.Resource.Limits {
			deploy.Spec.Template.Spec.Containers[0].Resources.Limits[k] = v
		}
	}

	// 这一步非常关键！
	// 建立关联后，删除elasticweb资源时就会将deployment也删除掉
	log.Info("set reference")
	if err := ctrl.SetControllerReference(elasticWeb, deploy, r.Scheme); err != nil {
		log.Error(err, "SetControllerReference error")
		return err
	}
	// 创建deployment
	log.Info("start create deployment")
	if err := r.Create(ctx, deploy); err != nil {
		log.Error(err, "create deployment error")
		return err
	}

	log.Info("create deployment success")
	return nil

}
// +kubebuilder:docs-gen:collapse=createDeployment







// 根据单个QPS和总QPS计算pod数量
func getExpectReplicas(elasticWeb *elasticwebv1.ElasticWeb) int32 {
	// 单个pod的QPS
	singlePodQPS := *(elasticWeb.Spec.SinglePodQPS)

	// 期望的总QPS
	totalQPS := *(elasticWeb.Spec.TotalQPS)

	// Replicas就是要创建的副本数
	replicas := totalQPS / singlePodQPS

	if totalQPS%singlePodQPS > 0 {
		replicas++
	}
	return replicas
}

// +kubebuilder:docs-gen:collapse=getExpectReplicas

//func parseResource(r *ElasticWebReconciler,elasticWeb *elasticwebv1.ElasticWeb) corev1.ResourceList{
//	if (elasticWeb.Spec.Request == nil){
//
//	}
//
//}

// 完成了pod的处理后，更新最新状态
func updateStatus(ctx context.Context, r *ElasticWebReconciler, elasticWeb *elasticwebv1.ElasticWeb, req ctrl.Request) error {
	log := r.Log.WithValues("func", "updateStatus")

	// 单个pod的QPS
	singlePodQPS := *(elasticWeb.Spec.SinglePodQPS)

	// 当pod创建完毕后，当前系统实际的QPS：单个pod的QPS * pod总数
	// 如果该字段还没有初始化，就先做初始化

	if nil == elasticWeb.Status.RealTotalQPS {
		elasticWeb.Status.RealTotalQPS = new(int32)
	}

	if nil == elasticWeb.Status.RealAvailQPS {
		elasticWeb.Status.RealAvailQPS = new(int32)
	}
	deploy := &appv1.Deployment{}
	// 用客户端工具查询
	err := r.Get(ctx, req.NamespacedName, deploy)
	// pod总数
	//replicas := getExpectReplicas(elasticWeb)
	replicas_spec := *(deploy.Spec.Replicas)
	replicas_avail := deploy.Status.Replicas
	replicas_now := (deploy.Status.AvailableReplicas)
	if replicas_now < replicas_avail {
		replicas_avail = replicas_now
	}

	if err != nil {
		*(elasticWeb.Status.RealTotalQPS) = 0
		*(elasticWeb.Status.RealAvailQPS) = 0
	} else {
		*(elasticWeb.Status.RealTotalQPS) = singlePodQPS * replicas_spec
		*(elasticWeb.Status.RealAvailQPS) = singlePodQPS * replicas_avail
	}

	log.Info(fmt.Sprintf("singlePodQPS [%d], replicas [%d], realQPS[%d]", singlePodQPS, replicas_avail, *(elasticWeb.Status.RealAvailQPS)))

	if err := r.Update(ctx, elasticWeb); err != nil {
		log.Error(err, "update instance error")
		return err
	}
	return nil
}
// +kubebuilder:docs-gen:collapse=updateStatus
