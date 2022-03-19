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
	"encoding/json"
	"fmt"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	//"sigs.k8s.io/controller-runtime/pkg/log"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ref "k8s.io/client-go/tools/reference"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	elasticscalev1 "backup-operator/api/v1"
)

// BackupDeploymentReconciler reconciles a BackupDeployment object
type BackupDeploymentReconciler struct {
	client.Client
	kubernetes.Clientset
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

type IndexedDeploy struct {
	deploy *appsv1.Deployment
	index  int
}

// +kubebuilder:docs-gen:collapse=IndexedDeploy

var (
	ScheduledTimeAnnotation = "elasticscale.com.sjtu.cit/scheduled-at"
	DeployStateAnnotation   = "elasticscale.com.sjtu.cit/state"
	DeployIDAnnotation      = "elasticscale.com.sjtu.cit/id"
	jobOwnerKey             = ".metadata.controller"
	apiGVStr                = elasticscalev1.GroupVersion.String()
	deploy_delete_policy    = metav1.DeletePropagationForeground
)

type deployType string

var backupType deployType = "backup"
var waitingType deployType = "waiting"

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
	qpslabels := r.findLabels(ctx, backdeploy)
	initdur := time.Duration(*backdeploy.Spec.InitalWait) * time.Millisecond
	if qpslabels == nil {
		log.V(1).Info("some service are not ready", "depend service", fmt.Sprintf("%v", backdeploy.Spec.ServiceName))
		return ctrl.Result{RequeueAfter: initdur * time.Millisecond}, nil
	}

	var childDeploys appsv1.DeploymentList
	if err := r.List(ctx, &childDeploys, client.InNamespace(req.Namespace), client.MatchingFields{jobOwnerKey: req.Name}); err != nil {
		log.Error(err, "unable to list child Jobs")
		return ctrl.Result{}, err
	}

	var activeDeploys map[int]*appsv1.Deployment = make(map[int]*appsv1.Deployment)
	var backDeploys []*appsv1.Deployment
	var waitDeploys map[int]*appsv1.Deployment = make(map[int]*appsv1.Deployment)

	runningreplicas := 0
	backupreplicas := 0

	for i, deploy := range childDeploys.Items {
		_, ok2, deployStatus, idx := isDeployStatus(&deploy)
		switch deployStatus {
		case string(elasticscalev1.ActiveState):
			if !ok2 {
				activeDeploys[int(*backdeploy.Status.LastID)] = &childDeploys.Items[i]
				tmpvalue := *backdeploy.Status.LastID
				*backdeploy.Status.LastID = (tmpvalue + 1)
			} else {
				activeDeploys[idx] = &childDeploys.Items[i]
			}
			runningreplicas += int(*(&childDeploys.Items[i]).Spec.Replicas)
		case string(elasticscalev1.WaitingState):
			if !ok2 {
				waitDeploys[int(*backdeploy.Status.LastID)] = &childDeploys.Items[i]
				tmpvalue := *backdeploy.Status.LastID
				*backdeploy.Status.LastID = (tmpvalue + 1)
			} else {
				waitDeploys[idx] = &childDeploys.Items[i]
			}
			runningreplicas += int(*(&childDeploys.Items[i]).Spec.Replicas)
		case string(elasticscalev1.BackupState):
			backDeploys = append(backDeploys, &childDeploys.Items[i])
			backupreplicas += int(*(&childDeploys.Items[i]).Spec.Replicas)
		case "":
			continue
		}
	}

	if runningreplicas == int(*backdeploy.Spec.RunningReplicas) && backupreplicas == int(*backdeploy.Spec.BackupReplicas) {
		if backdeploy.Spec.Action == string(elasticscalev1.Running) {
			if backdeploy.Status.Status == string(elasticscalev1.Scale) {
				backdeploy.Status.Status = string(elasticscalev1.Keep)
				patchData := map[string]interface{}{
					"metadata": map[string]interface{}{
						"annotations": map[string]interface{}{
							DeployStateAnnotation: string(elasticscalev1.ActiveState),
						},
					},
				}
				for idx, deploy := range waitDeploys {
					annotationBytes, _ := json.Marshal(patchData)
					_, err := r.AppsV1().Deployments(deploy.Namespace).Patch(deploy.Name, types.StrategicMergePatchType, annotationBytes)
					if err != nil {
						log.Error(err, "unable to patch annotation")
						return ctrl.Result{}, err
					}
					activeDeploys[idx] = deploy
					delete(waitDeploys, idx)
					options := metav1.ListOptions{
						LabelSelector: strings.Join([]string{elasticscalev1.DeploymentName, "=", deploy.Name}, ""),
					}
					podList, err := r.CoreV1().Pods(deploy.Namespace).List(options)
					if err != nil {
						log.Error(err, "unable to find pods from deployment: "+deploy.Name)
						return ctrl.Result{}, err
					}
					for _, pods := range podList.Items {
						patchData := map[string]interface{}{
							"metadata": map[string]interface{}{
								"labels": qpslabels,
							},
						}
						loadData, _ := json.Marshal(patchData)
						_, err := r.CoreV1().Pods(pods.Namespace).Patch(pods.Name, types.StrategicMergePatchType, loadData)
						if err != nil {
							log.Error(err, "unable to patch qpslabel")
							return ctrl.Result{}, err
						}
					}
				}
			}
			return ctrl.Result{}, nil
		}

	} else {
		if runningreplicas < int(*backdeploy.Spec.RunningReplicas) {

		} else {
			if runningreplicas > int(*backdeploy.Spec.RunningReplicas) {
				aim_scale_in := runningreplicas - int(*backdeploy.Spec.RunningReplicas)
				now_scale := 0
				deletes := make(map[string][]*IndexedDeploy)
				deletes["wait"] = []*IndexedDeploy{}
				deletes["active"] = []*IndexedDeploy{}
				scalein := make(map[string]map[int][]*IndexedDeploy)
				scalein["wait"] = make(map[int][]*IndexedDeploy)
				scalein["active"] = make(map[int][]*IndexedDeploy)
				if len(waitDeploys) > 0 {
					waitid := []int{}
					for k, _ := range waitDeploys {
						waitid = append(waitid, k)
					}
					sort.Ints(waitid)
					for j := len(waitid) - 1; j >= 0; j-- {
						tmp_rep := int(*waitDeploys[waitid[j]].Spec.Replicas)
						if now_scale+tmp_rep <= aim_scale_in {
							select_wait := new(IndexedDeploy)
							select_wait.deploy = waitDeploys[waitid[j]]
							select_wait.index = waitid[j]
							deletes["wait"] = append(deletes["wait"], select_wait)
							now_scale = now_scale + tmp_rep
						} else {
							if now_scale >= aim_scale_in {
								break
							}
							tmp_scale_in, _ := scalein["wait"][int(aim_scale_in-now_scale)]
							select_wait := new(IndexedDeploy)
							select_wait.deploy = waitDeploys[waitid[j]]
							select_wait.index = waitid[j]
							tmp_scale_in = append(tmp_scale_in, select_wait)
							scalein["wait"][int(aim_scale_in-now_scale)] = tmp_scale_in
							now_scale = aim_scale_in
							break
						}
					}
				}
				if now_scale < aim_scale_in {
					if len(activeDeploys) > 0 {
						activeid := []int{}
						for k, _ := range activeDeploys {
							activeid = append(activeid, k)
						}
						sort.Ints(activeid)
						for j := len(activeid) - 1; j >= 0; j-- {
							tmp_rep := int(*activeDeploys[activeid[j]].Spec.Replicas)
							if now_scale+tmp_rep <= aim_scale_in {
								select_active := new(IndexedDeploy)
								select_active.deploy = waitDeploys[activeid[j]]
								select_active.index = activeid[j]
								deletes["active"] = append(deletes["active"], select_active)
								now_scale = now_scale + tmp_rep
							} else {
								if now_scale >= aim_scale_in {
									break
								}
								tmp_scale_in, _ := scalein["active"][int(aim_scale_in-now_scale)]
								select_active := new(IndexedDeploy)
								select_active.deploy = waitDeploys[activeid[j]]
								select_active.index = activeid[j]
								tmp_scale_in = append(tmp_scale_in, select_active)
								scalein["active"][int(aim_scale_in-now_scale)] = tmp_scale_in
								now_scale = aim_scale_in
								break
							}
						}
					}
				}
				if len(deletes["wait"]) > 0 {
					for _, delete2 := range deletes["wait"] {
						if err := r.Delete(ctx, delete2.deploy, client.PropagationPolicy(deploy_delete_policy)); client.IgnoreNotFound(err) != nil {
							log.Error(err, "unable to delete the deployment", "deployment", delete2)
							//delete(waitDeploys,delete2.index)
							continue
						} else {
							log.V(0).Info("deleted the deployment", "deployment", delete2)
							delete(waitDeploys, delete2.index)
						}
					}
				}

				if len(deletes["active"]) > 0 {
					for _, delete2 := range deletes["active"] {
						if err := r.Delete(ctx, delete2.deploy, client.PropagationPolicy(deploy_delete_policy)); client.IgnoreNotFound(err) != nil {
							log.Error(err, "unable to delete the deployment", "deployment", delete2)
							//delete(activeDeploys,delete2.index)
							continue
						} else {
							log.V(0).Info("deleted the deployment", "deployment", delete2)
							delete(activeDeploys, delete2.index)
						}
					}
				}

				for to_in, deploys2 := range scalein["wait"] {
					for _, deploy2 := range deploys2 {
						if int32(to_in) >= *(deploy2.deploy.Spec.Replicas) {
							err := r.DeleteDeploy(ctx, deploy2.deploy)
							if err != nil {
								log.Error(err, "unable to delete the deployment", "deployment", deploy2)
								//delete(waitDeploys,deploy2.index)
								continue
							}
							delete(waitDeploys, deploy2.index)
						} else {
							err := r.Scalein(ctx, deploy2.deploy, to_in)
							if err != nil {
								log.Error(err, "unable to scale in the deployment", "deployment", deploy2)
								continue
							}
						}
					}
				}

				for to_in, deploys2 := range scalein["active"] {
					for _, deploy2 := range deploys2 {
						if int32(to_in) >= *(deploy2.deploy.Spec.Replicas) {
							err := r.DeleteDeploy(ctx, deploy2.deploy)
							if err != nil {
								log.Error(err, "unable to delete the deployment", "deployment", deploy2)
								//delete(activeDeploys,deploy2.index)
								continue
							}
							delete(activeDeploys, deploy2.index)
						} else {
							err := r.Scalein(ctx, deploy2.deploy, to_in)
							if err != nil {
								log.Error(err, "unable to scale in the deployment", "deployment", deploy2)
								continue
							}
						}
					}
				}
			}
		}

		if backupreplicas != int(*backdeploy.Spec.BackupReplicas) {
			if backupreplicas < int(*backdeploy.Spec.BackupReplicas) { //scale out
				var replicas int32 = int32(*backdeploy.Spec.BackupReplicas) - int32(backupreplicas)
				tmpvalue := *backdeploy.Status.LastID
				name, err := createDeployment(ctx, r, &backdeploy, req, backupType, &replicas)
				if err != nil {
					log.Error(err, "create deployment fault")
					return ctrl.Result{}, err
				}

				deploy, err := r.AppsV1().Deployments(backdeploy.Namespace).Get(name, metav1.GetOptions{})
				if err != nil {
					log.Error(err, "unable to get deployment")
					return ctrl.Result{}, err
				}
				backDeploys[tmpvalue] = deploy
			} else {

			}

		}
	}

	for _, activeDeploy := range activeDeploys {
		deployRef, err := ref.GetReference(r.Scheme, activeDeploy)
		if err != nil {
			log.Error(err, "unable to make reference to the running deployment", "deployment", activeDeploy)
			continue
		}
		backdeploy.Status.Active = append(backdeploy.Status.Active, *deployRef)
	}

	for _, backDeploy := range backDeploys {
		deployRef, err := ref.GetReference(r.Scheme, backDeploy)
		if err != nil {
			log.Error(err, "unable to make reference to the running deployment", "deployment", backDeploy)
			continue
		}
		backdeploy.Status.Back = append(backdeploy.Status.Back, *deployRef)
	}

	for _, waitDeploy := range waitDeploys {
		deployRef, err := ref.GetReference(r.Scheme, waitDeploy)
		if err != nil {
			log.Error(err, "unable to make reference to the running deployment", "deployment", waitDeploy)
			continue
		}
		backdeploy.Status.Wait = append(backdeploy.Status.Wait, *deployRef)
	}

	//for i,deploy

	return ctrl.Result{}, nil
}

//func getIndexedDeploy (deploys []*appsv1.Deployment)
// apierrors.IsNotFound(err)

func (r *BackupDeploymentReconciler) DeleteDeploy(ctx context.Context, deploy *appsv1.Deployment) error {
	if err := r.Delete(ctx, deploy, client.PropagationPolicy(deploy_delete_policy)); client.IgnoreNotFound(err) != nil {
		return err
	} else {
		return nil
	}
}

// +kubebuilder:docs-gen:collapse=DeleteDeploy

func (r *BackupDeploymentReconciler) Scalein(ctx context.Context, deploy *appsv1.Deployment, to_in int) error {
	tmp_rep := *(deploy.Spec.Replicas)
	*(deploy.Spec.Replicas) = int32(tmp_rep - int32(to_in))
	err := r.Update(ctx, deploy)
	if err != nil {
		if apierrors.IsNotFound(err) {
			err = r.Create(ctx, deploy)
		}
		if client.IgnoreNotFound(err) != nil {
			return err
		}
	}
	return nil
}

// +kubebuilder:docs-gen:collapse=Scalein
// 如果deployment有DeployIDAnnotation，则第二个bool返回为true，并且id为id，否则bool为false，id为-1
// 如果deployment有DeployStateAnnotation, 则第一个bool返回true，并且如果该字段是三种状态中的一种，v1返回该状态
func isDeployStatus(deploy *appsv1.Deployment) (bool, bool, string, int) {
	//for k, c := range deploy.Annotations {
	//	//if (c.Type == kbatch.JobComplete || c.Type == kbatch.JobFailed) && c.Status == corev1.ConditionTrue {
	//	//	return true, c.Type
	//	//}
	//	if strings.EqualFold(k,DeployStateAnnotation){
	//		if strings.EqualFold(c,string(elasticscalev1.WaitingState)) || strings.EqualFold(c,string(elasticscalev1.ActiveState)) || strings.EqualFold(c,string(elasticscalev1.BackupState)){
	//			return true,c
	//		}
	//	}
	//}
	v1, _ := deploy.Annotations[DeployStateAnnotation]
	v2, ok2 := deploy.Annotations[DeployIDAnnotation]
	id := -1
	if !ok2 {
		id = -1
	} else {
		id, _ = strconv.Atoi(v2)
	}

	if strings.EqualFold(v1, string(elasticscalev1.WaitingState)) || strings.EqualFold(v1, string(elasticscalev1.ActiveState)) || strings.EqualFold(v1, string(elasticscalev1.BackupState)) {
		return true, ok2, v1, id
	}
	return false, false, "", 0
}

// +kubebuilder:docs-gen:collapse=isDeployStatus

func (r *BackupDeploymentReconciler) findLabels(ctx context.Context, bd elasticscalev1.BackupDeployment) (labels map[string]string) {
	services := bd.Spec.ServiceName
	labels = make(map[string]string)
	service := new(corev1.Service)
	for _, svc := range services {
		namespacedname := types.NamespacedName{bd.Namespace, svc}
		err := r.Get(ctx, namespacedname, service)
		if err != nil {
			labels = nil
			break
		} else {
			labelselector := service.Spec.Selector
			for k, v := range labelselector {
				labels[k] = v
			}
		}
	}
	return
}

func createDeployment(ctx context.Context, r *BackupDeploymentReconciler, deploycrd *elasticscalev1.BackupDeployment,
	req ctrl.Request, types deployType, replicas *int32) (string, error) {
	log := r.Log.WithValues("func", "createDeployment")

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: deploycrd.Namespace,
			Name:      strings.Join([]string{deploycrd.Name, "-", string(*deploycrd.Status.LastID)}, ""),
		},
		Spec: deploycrd.Spec.BackupSpec,
	}
	if types == waitingType {
		deploy.Spec = deploycrd.Spec.RunningSpec
	}
	*deploy.Spec.Replicas = *replicas
	deploy.Spec.Template.Labels[elasticscalev1.DeploymentName] = deploy.Name

	if err := ctrl.SetControllerReference(deploycrd, deploy, r.Scheme); err != nil {
		log.Error(err, "deploymentSetControllerReference error")
		return "", err
	}

	if err := r.Create(ctx, deploy); err != nil {
		log.Error(err, "create deployment error")
		return "", err
	}
	*deploycrd.Status.LastID = *deploycrd.Status.LastID + 1
	log.Info("create deployment success")
	return deploy.Name, nil
}

// +kubebuilder:docs-gen:collapse=findLabels

func (r *BackupDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&elasticscalev1.BackupDeployment{}).
		Complete(r)
}
