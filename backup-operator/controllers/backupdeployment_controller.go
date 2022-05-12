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
	deployOwnerKey          = ".metadata.controller"
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
		dlv debug --headless --listen=:2345 --api-version=2 --accept-multiclient
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
	lastID := new(int64)
	if backdeploy.Status.LastID == nil {
		backdeploy.Status.LastID = lastID
	}

	qpslabels := r.findLabels(ctx, backdeploy)
	initdur := time.Duration(*backdeploy.Spec.InitalWait) * time.Millisecond
	if len(qpslabels) == 0 {
		log.V(1).Info("some service are not ready", "depend service", fmt.Sprintf("%v", backdeploy.Spec.ServiceName))
		return ctrl.Result{Requeue: false, RequeueAfter: initdur}, nil
	}

	var childDeploys appsv1.DeploymentList
	if err := r.List(ctx, &childDeploys, client.InNamespace(req.Namespace), client.MatchingFields{deployOwnerKey: req.Name}); err != nil {
		log.V(1).Info("unable to List Deployments in current namespace")
	}

	var activeDeploys map[int]*appsv1.Deployment = make(map[int]*appsv1.Deployment)
	var backDeploys map[int]*appsv1.Deployment = make(map[int]*appsv1.Deployment)
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
				*backdeploy.Status.LastID = tmpvalue + 1
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
			backDeploys[idx] = &childDeploys.Items[i]
			backupreplicas += int(*(&childDeploys.Items[i]).Spec.Replicas)
		case "":
			continue
		}
	}
	if backdeploy.Status.LastID == nil {
		log.V(1).Info("There don't exist any deployments controlled by" + req.Namespace + "/" + req.Name)
		*backdeploy.Status.LastID = 0
	}
	if backupreplicas != int(*backdeploy.Spec.BackupReplicas) {
		//backup scale out
		if backupreplicas < int(*backdeploy.Spec.BackupReplicas) { //scale out
			var replicas int32 = int32(*backdeploy.Spec.BackupReplicas) - int32(backupreplicas)
			tmpvalue := *backdeploy.Status.LastID
			deploy, err := createDeployment(ctx, r, &backdeploy, req, backupType, &replicas, nil)
			if err != nil {
				log.Error(err, "create backup deployment fault")
				return ctrl.Result{}, err
			}
			backDeploys[int(tmpvalue)] = deploy
			goto RECONCILED
		} else if backupreplicas > int(*backdeploy.Spec.BackupReplicas) {
			deletingNums := backupreplicas - int(*backdeploy.Spec.BackupReplicas)
			var backs []int
			for k, _ := range backDeploys {
				backs = append(backs, k)
			}
			sort.Ints(backs)
			deletedNums := 0
			for _, v := range backs {
				deploy := backDeploys[v]
				if deletedNums+int(*deploy.Spec.Replicas) <= deletingNums {
					deployReplicas := int(*deploy.Spec.Replicas)
					err := r.DeleteDeploy(ctx, deploy)
					if err != nil {
						log.Error(err, "delete backup failed")
						return ctrl.Result{}, err
					}
					delete(backDeploys, v)
					deletedNums += deployReplicas
				} else if deletedNums < deletingNums {
					moreThanDeploys := deletingNums - deletedNums
					*deploy.Spec.Replicas -= int32(moreThanDeploys)
					if *deploy.Spec.Replicas <= 0 {
						log.Info("BackupDeployments: scale in amount exception")
						return ctrl.Result{}, nil
					}
					break
				}
			}
			goto RECONCILED
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
			goto RECONCILED
		}

	} else {
		// 创建waiting deploy。先杀死backdeploy，记录杀死的deploy管理的pod所在node
		// 以这些node为waiting deploy的亲和性字段创建deploy
		// 遍历backup deploy:
		// 分为两种情况。当要创建的pod数量小于当前deploy管理的pod数量，可以直接修改当前deploy.spec.replicas
		// 当要创建的pod数量大于当前deploy管理的pod数量，需要直接杀死当前的backup，并且以这些pod所在node为亲和性字段创建新deploy
		// 注意每次修改deploy的数量都要更新三个临时状态map。这三个临时状态map在程序末尾会写进集群状态
		if runningreplicas < int(*backdeploy.Spec.RunningReplicas) {
			aim_scale_out := int(*backdeploy.Spec.RunningReplicas) - runningreplicas
			nowScaleOut := 0
			if aim_scale_out > backupreplicas {
				log.Info("Do not exist enough backup when scaling out")
				return ctrl.Result{}, nil
			}
			if backupreplicas > 0 {
				var backid []int
				for k, _ := range backDeploys {
					backid = append(backid, k)
				}
				sort.Ints(backid)
				var backLabels []string
				for _, v := range backid {
					tempReplicas := backDeploys[v].Status.Replicas
					tempDeploy := backDeploys[v]
					tempLabelsMap := findNodenames(ctx, r, tempDeploy)
					var tempLabels []string
					for k := range tempLabelsMap {
						tempLabels = append(tempLabels, k)
					}
					// var deleteSlice []*appsv1.Deployment
					if int(tempReplicas)+nowScaleOut <= aim_scale_out {
						// 需要删除这个backdeploy
						//
						// backLabels = append(backLabels, tempLabels...)
						// deleteSlice = append(deleteSlice, tempDeploy)
						nowScaleOut += int(tempReplicas)
						err := r.DeleteDeploy(ctx, tempDeploy)
						if err != nil {
							log.Error(err, "delete backup deploy failed while scaling out waiting replicas")
							return ctrl.Result{}, err
						}
						// backdeploy.Status.Back

					} else if nowScaleOut < aim_scale_out {
						// 不需要删除这个backdeploy 只需要缩减这个deploy，缩减完毕后可以退出
						if *backDeploys[v].Spec.Replicas > int32(aim_scale_out-nowScaleOut) {
							//删除这个deploy管理的若干个pod
							scaleValue := int(aim_scale_out - nowScaleOut)
							nowScaleOut += scaleValue
							backLabels = append(backLabels, tempLabels...)
							err := r.Scalein(ctx, backDeploys[v], scaleValue)
							if err != nil {
								log.Error(err, "back to waiting failed")
								return ctrl.Result{}, err

							}
							break
							// backlabels :=  findNodenames(ctx, r, tempDeploy)
							// deploy, err := createDeployment(ctx, r, &backdeploy, req, waitingType, replicas, &backLabels)
						}
					} else if nowScaleOut > aim_scale_out {
						// 删除过多，不存在这种情况
						log.Info("waiting deploy Scale out exception")
						break
					}
				}
				// for _, v := range deleteSlice {
				// 	replicas := v.Spec.Replicas
				// 	deploy, err := createDeployment(ctx, r, &backdeploy, req, waitingType, replicas, &backLabels)
				// 	if err != nil {
				// 		log.Error(err, "scale out running deploy failed")
				// 		return ctrl.Result{}, err
				// 	}
				// 	tempID := *backdeploy.Status.LastID
				// 	backDeploys[int(tempID)] = deploy
				// }
				// for _, v := range deleteSlice {
				// 	strID := v.ObjectMeta.Annotations[DeployIDAnnotation]
				// 	intID, err := strconv.Atoi(strID)
				// 	if err != nil {
				// 		log.Error(err, "strID to intID failed")
				// 		return ctrl.Result{}, err
				// 	}
				// 	err = r.DeleteDeploy(ctx, v)
				// 	if err != nil {
				// 		log.Error(err, "delete failed when scaling out running")
				// 		return ctrl.Result{}, err
				// 	}
				// 	delete(backDeploys, intID)
				// }
			} else {
				log.Info("back up replicas= 0")
			}

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
	}
RECONCILED:
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
	r.Update(ctx, &backdeploy)
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
	// 根据bd中的ServiceName列表遍历当前Namespace下是否存在对应的service
	// 如果存在对应的Service，就返回这些service的Spec.Selector键值对
	services := bd.Spec.ServiceName
	labels = make(map[string]string)
	service := new(corev1.Service)
	for _, svc := range services {
		namespacedname := types.NamespacedName{Namespace: bd.Namespace, Name: svc}
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

// 根据指定的replicas和nodename创建deployment
func createDeployment(ctx context.Context, r *BackupDeploymentReconciler, deploycrd *elasticscalev1.BackupDeployment,
	req ctrl.Request, types deployType, replicas *int32, hostLabels *[]string) (*appsv1.Deployment, error) {
	log := r.Log.WithValues("func", "createDeployment")

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   deploycrd.Namespace,
			Name:        strings.Join([]string{deploycrd.Name, "-", strconv.Itoa(int(*deploycrd.Status.LastID))}, ""),
			Annotations: map[string]string{DeployIDAnnotation: strconv.FormatInt(*deploycrd.Status.LastID, 10)},
		},
		Spec: deploycrd.Spec.BackupSpec,
	}
	if types == waitingType {
		deploy.Spec = deploycrd.Spec.RunningSpec
		deploy.Annotations[DeployStateAnnotation] = string(elasticscalev1.WaitingState)
		affinity := corev1.NodeAffinity{
			PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
				{
					Weight: 100,
					Preference: corev1.NodeSelectorTerm{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key:      "kubernetes.io/hostname",
								Operator: corev1.NodeSelectorOperator("In"),
								Values:   *hostLabels,
							},
						},
					},
				},
			},
		}
		allocAffinity := new(corev1.Affinity)
		allocAffinity.NodeAffinity = &affinity
		deploy.Spec.Template.Spec.Affinity = allocAffinity
	} else if types == backupType {
		deploy.Spec = deploycrd.Spec.BackupSpec
		deploy.Annotations[DeployStateAnnotation] = string(elasticscalev1.BackupState)

	}
	deploy.Spec.Replicas = replicas
	deploy.Spec.Template.Labels[elasticscalev1.DeploymentName] = deploy.Name

	if err := ctrl.SetControllerReference(deploycrd, deploy, r.Scheme); err != nil {
		log.Error(err, "deploymentSetControllerReference error")
		return nil, err
	}

	deployment, err := r.AppsV1().Deployments(deploy.Namespace).Create(deploy)
	if err != nil {
		log.Error(err, "unable to create deployment in function create")
		return nil, err
	}
	*deploycrd.Status.LastID = *deploycrd.Status.LastID + 1
	log.Info("create deployment success")
	r.Update(ctx, deploycrd)
	return deployment, nil
}

func findNodenames(ctx context.Context, r *BackupDeploymentReconciler, dp *appsv1.Deployment) map[string]int {
	log := r.Log.WithValues("fun", "findNodenames")
	options := metav1.ListOptions{
		LabelSelector: strings.Join([]string{elasticscalev1.DeploymentName, "=", dp.Name}, ""),
	}
	podList, err := r.CoreV1().Pods(dp.Namespace).List(options)
	if err != nil {
		log.Error(err, "findNodenames error")
	}
	var hostNames map[string]int = make(map[string]int)
	for _, pods := range podList.Items {
		node, err := r.CoreV1().Nodes().Get(pods.Spec.NodeName, metav1.GetOptions{})
		if err != nil {
			log.Error(err, "find hostname error")
			return nil
		}
		hostNames[node.Labels["kubernetes.io/hostname"]] = 1
	}
	return hostNames
}

// +kubebuilder:docs-gen:collapse=findLabels

func (r *BackupDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// 类型断言
	//返回Object Controller的Name
	extractValue := func(rawObj runtime.Object) []string {
		deploys := rawObj.(*appsv1.Deployment)
		owner := metav1.GetControllerOf(deploys)
		if owner == nil {
			return nil
		}
		// ...make sure it's a CronJob...
		if owner.APIVersion != apiGVStr || owner.Kind != "BackupDeployment" {
			return nil
		}
		// ...and if so, return it
		return []string{owner.Name}
	}
	// GetFieldIndexer returns a client.FieldIndexer configured with the client
	// FieldIndexer knows how to index over a particular "field" such that it
	// can later be used by a field selector.
	if err := mgr.GetFieldIndexer().IndexField(&appsv1.Deployment{}, deployOwnerKey, extractValue); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&elasticscalev1.BackupDeployment{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}
