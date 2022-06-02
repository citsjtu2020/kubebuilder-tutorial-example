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
	"errors"
	"fmt"
	"k8s.io/client-go/tools/record"

	//"sigs.k8s.io/controller-runtime/pkg/reconcile"

	//"sigs.k8s.io/controller-runtime/pkg/log"

	//"sigs.k8s.io/controller-runtime/pkg/log"
	"sync"

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
	StatusLock *sync.RWMutex
	Eventer record.EventRecorder
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
	deployIDKey             = "deployID"
	apiGVStr                = elasticscalev1.GroupVersion.String()
	deploy_delete_policy    = metav1.DeletePropagationForeground
)

type deployType string

var backupType deployType = "backup"
var waitingType deployType = "waiting"
var activeType deployType = "active"

//two ways of create / scheduling method:
// 1. the pod in the same of mutiple hosts as on deployment
// 2. the pod scheduled to different
type ScheduleStrategy int
var ScheduleOnSameHosts ScheduleStrategy = 1
var ScheduleOnSameHostsHard ScheduleStrategy = 2
var ScheduleRoundBin ScheduleStrategy = 3
var ScheduleRoundBinHard ScheduleStrategy = 4
var PodAntiAffinityWeight int = 10

type AllocateStrategy int
var BinPacking AllocateStrategy = 1
var LeastUsage AllocateStrategy = 2

type ScaleInType int
var ScaleInRunning ScaleInType = 1
var ScaleInBack ScaleInType = 2

//
//type DisPatch int
//var DisPatchOnLimitedHosts DisPatch = 1
//var DisPatchOnLimited

var DefaultUseHosts int = 2
var DefaultUnitReplicas int = 5
var MaxSafeReplicasOnOneHost = 5


//

// +kubebuilder:rbac:groups=elasticscale.com.sjtu.cit,resources=backupdeployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=elasticscale.com.sjtu.cit,resources=backupdeployments/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs/status,verbs=get
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments/status,verbs=get
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=services/status,verbs=get
// +kubebuilder:rbac:groups="",resources=events,verbs=get;list;watch;create;update;patch


func (r *BackupDeploymentReconciler) Reconcile(req ctrl.Request) (ctrlresults ctrl.Result, errdefer error) {
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
		ctrlresults = ctrl.Result{}
		errdefer = client.IgnoreNotFound(err)
		//ctrl.Result{}, client.IgnoreNotFound(err)
		return
	}
	lastID := new(int64)
	if backdeploy.Status.LastID == nil {
		backdeploy.Status.LastID = lastID
	}

	if backdeploy.Status.LastID == nil {
		log.V(1).Info("There don't exist any deployments controlled by" + req.Namespace + "/" + req.Name)
		//*backdeploy.Status.LastID = 0
		_,_,_ = r.UpdateBackupLastID(ctx,&backdeploy,0)
	}

	qpslabels := r.findLabels(ctx, backdeploy)
	initdur := time.Duration(*backdeploy.Spec.InitalWait) * time.Millisecond
	if len(qpslabels) == 0 {
		log.V(1).Info("some service are not ready", "depend service", fmt.Sprintf("%v", backdeploy.Spec.ServiceName))
		//ctrl.Result{Requeue: false, RequeueAfter: initdur}, nil
		ctrlresults = ctrl.Result{Requeue: false, RequeueAfter: initdur}
		errdefer = nil
		return
	}

	var activeDeploys map[int]*appsv1.Deployment = make(map[int]*appsv1.Deployment)
	var backDeploys map[int]*appsv1.Deployment = make(map[int]*appsv1.Deployment)
	var waitDeploys map[int]*appsv1.Deployment = make(map[int]*appsv1.Deployment)

	runningreplicas := 0
	backupreplicas := 0

	//activeDeploys,backDeploys,waitDeploys,backupreplicas,runningreplicas
	activeDeploys,backDeploys,waitDeploys,backupreplicas,runningreplicas = r.SyncBackupDelpoyList(ctx,log,req.Namespace,req.Name,&backdeploy)
	nowScheduleStrategy,ok := r.ReadStrategyStrategy(&backdeploy)
	if !ok {
		err := r.UpdateScheduleStrategy(ctx, &backdeploy)
		log.Error(err, "update the scheduling strategy")
	}
	nowAllocateStrategy,ok := r.ReadAllocateStrategy(&backdeploy)
	if !ok{
		err := r.UpdateAllocateStrategy(ctx, &backdeploy)
		log.Error(err, "update the allocating strategy")
	}

	nowUnitReplicas,ok := r.ReadUnitReplicas(&backdeploy)
	if !ok{
		err := r.UpdateUnitReplicas(ctx,&backdeploy)
		log.Error(err,"update the unit replicas error")
	}

	//var errdefer error = nil

	defer func() {
		ToBeUpdate := make(map[elasticscalev1.DeployState]map[int]*appsv1.Deployment)
		ToBeUpdate[elasticscalev1.ActiveState] = activeDeploys
		ToBeUpdate[elasticscalev1.BackupState] = backDeploys
		ToBeUpdate[elasticscalev1.WaitingState] = waitDeploys
		errdefertmp := r.UpdateBackDeployList(ctx,log,&backdeploy,ToBeUpdate)
		if errdefertmp != nil{
			log.Error(errdefertmp,"update status list error")
			errdefer = errors.New(fmt.Sprintf("%s; %s",errdefer.Error(),errdefertmp))
		}
		errdefertmp = r.Update(ctx, &backdeploy)
		if errdefertmp != nil{
			log.Error(errdefer,"update this backupdeployment")
			errdefer = errors.New(fmt.Sprintf("%s; %s",errdefer.Error(),errdefertmp))
		}
	}()

	if runningreplicas == int(*backdeploy.Spec.RunningReplicas) && backupreplicas == int(*backdeploy.Spec.BackupReplicas) {
		if r.ReadBackupAction(&backdeploy) == string(elasticscalev1.Running) {
			//backdeploy.Spec.Action
			//backdeploy.Status.Status
			if r.ReadBackupStatus(&backdeploy) == string(elasticscalev1.Scale) {
				//	DispatchQPSToPods(ctx context.Context,log logr.Logger,deploycrd *elasticscalev1.BackupDeployment,waitDeploys map[int]*appsv1.Deployment,activeDeploys map[int]*appsv1.Deployment,qpslabels map[string]string)
				err := r.DispatchQPSToPods(ctx, log, &backdeploy, waitDeploys, activeDeploys, qpslabels)
				if err != nil {
					//ctrl.Result{}, err
					ctrlresults = ctrl.Result{}
					errdefer = err
					return
				}
			}
			//goto RECONCILED
		}

	} else {
		// 创建waiting deploy。先杀死backdeploy，记录杀死的deploy管理的pod所在node
		// 以这些node为waiting deploy的亲和性字段创建deploy
		// 遍历backup deploy:
		// 分为两种情况。当要创建的pod数量小于当前deploy管理的pod数量，可以直接修改当前deploy.spec.replicas
		// 当要创建的pod数量大于当前deploy管理的pod数量，需要直接杀死当前的backup，并且以这些pod所在node为亲和性字段创建新deploy
		// 注意每次修改deploy的数量都要更新三个临时状态map。这三个临时状态map在程序末尾会写进集群状态
		//*backdeploy.Spec.RunningReplicas
		if runningreplicas < r.ReadRunningReplicas(&backdeploy) {
			aim_scale_out := r.ReadRunningReplicas(&backdeploy) - runningreplicas
			//nowScaleOut := 0
			usingBackDeploys := make(map[int]*appsv1.Deployment)
			if aim_scale_out > backupreplicas {
				log.Info("Do not exist enough backup when scaling out")
				//backDeploys
				if backupreplicas > 0{
					for k,tmpDelpy := range backDeploys{
						usingBackDeploys[k] = tmpDelpy
					}
				}else{
					usingBackDeploys = nil
				}

				usingBackNode,totalback := r.FindAllNodeMap(ctx,usingBackDeploys)

				creationplans,createreplicas:= r.GenerateCreationPlan(usingBackNode,totalback,aim_scale_out,nowScheduleStrategy,nowAllocateStrategy,nowUnitReplicas)

				if usingBackDeploys != nil && len(usingBackDeploys) > 0{
					for k,tempDeploy := range usingBackDeploys{
						err := r.DeleteDeploy(ctx, tempDeploy)
						if err != nil {
							log.Error(err, "delete backup deploy failed while scaling out waiting replicas")
							//ctrl.Result{}, err
							ctrlresults = ctrl.Result{}
							errdefer = err
							return
						}
						_, err = r.DeleteDeployList(backDeploys,k,tempDeploy)
						if err != nil {
							//ctrl.Result{}, err
							ctrlresults = ctrl.Result{}
							errdefer = err
							return
						}
					}
				}

				for createtypes,creations := range creationplans{
					for id0,tmpMap := range creations{
						aimreplicas := int32(createreplicas[createtypes][id0])
						if aimreplicas <= 0{
							continue
						}
						newid, newdeploy, err := createDeployment(ctx,r,&backdeploy,req,createtypes,nowScheduleStrategy,&aimreplicas,tmpMap)
						if newid < 0 && err == nil{
							continue
						}
						if err != nil {
							//ctrl.Result{}, err
							ctrlresults = ctrl.Result{}
							errdefer = err
							return
						}
						err = r.UpdateDeployList(waitDeploys, newid, newdeploy)
						if err != nil {
							//ctrl.Result{}, err
							ctrlresults = ctrl.Result{}
							errdefer = err
							return
						}
					}
				}

				//tempLabelsMap := findNodenames(ctx, r, tempDeploy)
				//ctrl.Result{}, nil
				//return

			} else if backupreplicas > 0 {
				tmpLabels,deletedkey,scaleinkey,scaleinnum,totalback := r.findDeleteScaleBackup(ctx,backDeploys,aim_scale_out)
				if scaleinkey > 0 && scaleinnum > 0{
					if _,ok0 := backDeploys[scaleinkey];ok0{
						tmpScaleinDeploy := backDeploys[scaleinkey]
						//tmpScaleinDeploy
						before_scale_in_nodes := findNodenames(ctx,r,tmpScaleinDeploy)
						new_results := make(map[string]int)
						err,okscale := r.Scalein(ctx, tmpScaleinDeploy, scaleinnum)
						if err == nil && !okscale{
							deletedkey = append(deletedkey,scaleinkey)
							new_results = before_scale_in_nodes
							totalback += r.ReadDeployReplicas(tmpScaleinDeploy)
						}else{
							after_scale_in_nodes := make(map[string]int)
							if err == nil{
								after_scale_in_nodes = findNodenames(ctx,r,tmpScaleinDeploy)
								new_results = differ_after_scalein(before_scale_in_nodes,after_scale_in_nodes)
							}
						}

						for k,v := range new_results{
							if _,ok1 := tmpLabels[k];ok1{
								tmpLabels[k] += v
							}else{
								tmpLabels[k] = v
							}
						}
					}
				}

				do_not_using_nodes := make([]string,0)
				for k,_ := range tmpLabels{
					if tmpLabels[k] <= 0{
						do_not_using_nodes = append(do_not_using_nodes, k)
					}
				}
				if len(do_not_using_nodes) > 0{
					for _,node := range do_not_using_nodes{
						delete(tmpLabels, node)
					}
				}
				creationplans,createreplicas := r.GenerateCreationPlan(tmpLabels,totalback,aim_scale_out,nowScheduleStrategy,nowAllocateStrategy,nowUnitReplicas)

				if len(deletedkey) > 0{
					for _,deletedid := range deletedkey{
						tempDeploy := backDeploys[deletedid]
						err := r.DeleteDeploy(ctx, tempDeploy)
						if err != nil {
							log.Error(err, "delete backup deploy failed while scaling out waiting replicas")
							//ctrl.Result{}, err
							ctrlresults = ctrl.Result{}
							errdefer = err
							return
						}
						_, err = r.DeleteDeployList(backDeploys,deletedid,tempDeploy)
						if err != nil {
							//ctrl.Result{}, err
							ctrlresults = ctrl.Result{}
							errdefer = err
							return
						}
					}
				}

				for createtypes,creations := range creationplans{
					for id0,tmpMap := range creations{
						aimreplicas := int32(createreplicas[createtypes][id0])
						if aimreplicas <= 0{
							continue
						}
						newid, newdeploy, err := createDeployment(ctx,r,&backdeploy,req,createtypes,nowScheduleStrategy,&aimreplicas,tmpMap)
						if newid < 0 && err == nil{
							continue
						}
						if err != nil {
							//ctrl.Result{}, err
							ctrlresults = ctrl.Result{}
							errdefer = err
							return
						}
						err = r.UpdateDeployList(waitDeploys, newid, newdeploy)
						if err != nil {
							//ctrl.Result{}, err
							ctrlresults = ctrl.Result{}
							errdefer = err
							return
						}
					}
				}
				//if scaleinkey < 0 || scaleinnum <= 0{
				//
				//}
				//else{

					//creationplans,createreplicas := r.GenerateCreationPlan(tmpLabels,totalback,aim_scale_out,nowScheduleStrategy,nowAllocateStrategy,nowUnitReplicas)
					//
					//if len(deletedkey) > 0{
					//	for _,deletedid := range deletedkey{
					//		tempDeploy := backDeploys[deletedid]
					//		err := r.DeleteDeploy(ctx, tempDeploy)
					//		if err != nil {
					//			log.Error(err, "delete backup deploy failed while scaling out waiting replicas")
					//ctrl.Result{}, err
					//			return
					//		}
					//		_, err = r.DeleteDeployList(backDeploys,deletedid,tempDeploy)
					//		if err != nil {
					//ctrl.Result{}, err
					//			return
					//		}
					//	}
					//}
					//
					//for createtypes,creations := range creationplans{
					//	for id0,tmpMap := range creations{
					//		aimreplicas := int32(createreplicas[createtypes][id0])
					//		if aimreplicas <= 0{
					//			continue
					//		}
					//		newid, newdeploy, err := createDeployment(ctx,r,&backdeploy,req,createtypes,nowScheduleStrategy,&aimreplicas,tmpMap)
					//		if newid < 0 && err == nil{
					//			continue
					//		}
					//		if err != nil {
					//ctrl.Result{}, err
					//			return
					//		}
					//		err = r.UpdateDeployList(waitDeploys, newid, newdeploy)
					//ctrl.Result{}, err
					//		if err != nil {
					//			return
					//		}
					//	}
					//}

				//}
				//var backid []int
				//for k, _ := range backDeploys {
				//	backid = append(backid, k)
				//}
				//sort.Ints(backid)
				//var backLabels []string
				//for _, v := range backid {
				//	//backDeploys[v].Status.Replicas
				//	tempReplicas := r.ReadDeployReplicas(backDeploys[v])
				//	tempDeploy := backDeploys[v]
				//	tempLabelsMap := findNodenames(ctx, r, tempDeploy)
				//	var tempLabels []string
				//	for k := range tempLabelsMap {
				//		tempLabels = append(tempLabels, k)
				//	}
				//	// var deleteSlice []*appsv1.Deployment
				//	if int(tempReplicas)+nowScaleOut <= aim_scale_out {
				//		// 需要删除这个backdeploy
				//		//
				//		// backLabels = append(backLabels, tempLabels...)
				//		// deleteSlice = append(deleteSlice, tempDeploy)
				//		nowScaleOut += int(tempReplicas)
				//		err := r.DeleteDeploy(ctx, tempDeploy)
				//		if err != nil {
				//			log.Error(err, "delete backup deploy failed while scaling out waiting replicas")
				//ctrl.Result{}, err
				//			return
				//		}
				//		// backdeploy.Status.Back
				//
				//	} else if nowScaleOut < aim_scale_out {
				//		// 不需要删除这个backdeploy 只需要缩减这个deploy，缩减完毕后可以退出
				//		if *backDeploys[v].Spec.Replicas > int32(aim_scale_out-nowScaleOut) {
				//			//删除这个deploy管理的若干个pod
				//			scaleValue := int(aim_scale_out - nowScaleOut)
				//			nowScaleOut += scaleValue
				//			backLabels = append(backLabels, tempLabels...)
				//			err := r.Scalein(ctx, backDeploys[v], scaleValue)
				//			if err != nil {
				//				log.Error(err, "back to waiting failed")
				//ctrl.Result{}, err
				//				return
				//
				//			}
				//			break
				//			// backlabels :=  findNodenames(ctx, r, tempDeploy)
				//			// deploy, err := createDeployment(ctx, r, &backdeploy, req, waitingType, replicas, &backLabels)
				//		}
				//	} else if nowScaleOut > aim_scale_out {
				//		// 删除过多，不存在这种情况
				//		log.Info("waiting deploy Scale out exception")
				//		break
				//	}
				//}
				// for _, v := range deleteSlice {
				// 	replicas := v.Spec.Replicas
				// 	deploy, err := createDeployment(ctx, r, &backdeploy, req, waitingType, replicas, &backLabels)
				// 	if err != nil {
				// 		log.Error(err, "scale out running deploy failed")
				// ctrl.Result{}, err
				// 		return
				// 	}
				// 	tempID := *backdeploy.Status.LastID
				// 	backDeploys[int(tempID)] = deploy
				// }
				// for _, v := range deleteSlice {
				// 	strID := v.ObjectMeta.Annotations[DeployIDAnnotation]
				// 	intID, err := strconv.Atoi(strID)
				// 	if err != nil {
				// 		log.Error(err, "strID to intID failed")
				//ctrl.Result{}, err
				// 		return
				// 	}
				// 	err = r.DeleteDeploy(ctx, v)
				// 	if err != nil {
				// 		log.Error(err, "delete failed when scaling out running")
				//ctrl.Result{}, err
				// 		return
				// 	}
				// 	delete(backDeploys, intID)
				// }
			}
			//else {
			//	log.Info("back up replicas= 0")
			//}

		} else {
			if runningreplicas > int(r.ReadBackReplicas(&backdeploy)) {
				aim_scale_in := runningreplicas - int(r.ReadBackReplicas(&backdeploy))
				//findScaleInDeploys
				tmpStatedMaps := make(map[elasticscalev1.DeployState]map[int]*appsv1.Deployment)
				tmpStatedMaps[elasticscalev1.WaitingState] = waitDeploys
				tmpStatedMaps[elasticscalev1.ActiveState] = activeDeploys
				deletes,scalein := r.findScaleInDeploys(tmpStatedMaps,aim_scale_in,ScaleInRunning)
				if len(deletes[elasticscalev1.WaitingState]) > 0 {
					for _, delete2 := range deletes[elasticscalev1.WaitingState] {
						if err := r.Delete(ctx, delete2.deploy, client.PropagationPolicy(deploy_delete_policy)); client.IgnoreNotFound(err) != nil {
							log.Error(err, "unable to delete the deployment", "deployment", delete2)
							//delete(waitDeploys,delete2.index)
							continue
						} else {
							log.V(0).Info("deleted the deployment", "deployment", delete2)
							//delete(waitDeploys, delete2.index)
							_, err = r.DeleteDeployList(waitDeploys,delete2.index,delete2.deploy)
							if err != nil {
								//ctrl.Result{}, err
								//return
								continue
							}
						}
					}
				}

				if len(deletes[elasticscalev1.ActiveState]) > 0 {
					for _, delete2 := range deletes[elasticscalev1.ActiveState] {
						if err := r.Delete(ctx, delete2.deploy, client.PropagationPolicy(deploy_delete_policy)); client.IgnoreNotFound(err) != nil {
							log.Error(err, "unable to delete the deployment", "deployment", delete2)
							//delete(activeDeploys,delete2.index)
							continue
						} else {
							log.V(0).Info("deleted the deployment", "deployment", delete2)
							//delete(activeDeploys, delete2.index)
							_,err = r.DeleteDeployList(activeDeploys,delete2.index,delete2.deploy)
							if err != nil{
								continue
							}
						}
					}
				}

				if len(scalein[elasticscalev1.WaitingState]) > 0{
					for to_in, deploys2 := range scalein[elasticscalev1.WaitingState] {
						for _, deploy2 := range deploys2 {
							//*(deploy2.deploy.Spec.Replicas)
							if (to_in) >=  r.ReadDeploySpecReplicas(deploy2.deploy){
								err := r.DeleteDeploy(ctx, deploy2.deploy)
								if err != nil {
									log.Error(err, "unable to delete the deployment", "deployment", deploy2)
									//delete(waitDeploys,deploy2.index)
									continue
								}
								_, err = r.DeleteDeployList(waitDeploys,deploy2.index,deploy2.deploy)
								if err != nil {
									//ctrl.Result{}, err
									//return
									continue
								}
							} else {
								err,okscalein := r.Scalein(ctx, deploy2.deploy, to_in)
								if err != nil {
									log.Error(err, "unable to scale in the deployment", "deployment", deploy2)
									continue
								}else{
									if okscalein == false{
										if err := r.Delete(ctx, deploy2.deploy, client.PropagationPolicy(deploy_delete_policy)); client.IgnoreNotFound(err) != nil {
											log.Error(err, "unable to delete the deployment", "deployment", deploy2)
											//delete(activeDeploys,delete2.index)
											continue
										} else {
											log.V(0).Info("deleted the deployment", "deployment", deploy2)
											//delete(activeDeploys, delete2.index)
											_,err = r.DeleteDeployList(waitDeploys,deploy2.index,deploy2.deploy)
											if err != nil{
												continue
											}
										}
									}
								}
							}
						}
					}
				}


				if len(scalein[elasticscalev1.ActiveState]) > 0{
					for to_in, deploys2 := range scalein[elasticscalev1.ActiveState] {
						for _, deploy2 := range deploys2 {
							//*(deploy2.deploy.Spec.Replicas)
							if (to_in) >= r.ReadDeploySpecReplicas(deploy2.deploy){
								err := r.DeleteDeploy(ctx, deploy2.deploy)
								if err != nil {
									log.Error(err, "unable to delete the deployment", "deployment", deploy2)
									//delete(activeDeploys,deploy2.index)
									continue
								}
								_,err = r.DeleteDeployList(activeDeploys,deploy2.index,deploy2.deploy)
								if err != nil{
									continue
								}
							} else {
								err,okscalein := r.Scalein(ctx, deploy2.deploy, to_in)
								if err != nil {
									log.Error(err, "unable to scale in the deployment", "deployment", deploy2)
									continue
								}else{
									if okscalein == false{
										if err := r.Delete(ctx, deploy2.deploy, client.PropagationPolicy(deploy_delete_policy)); client.IgnoreNotFound(err) != nil {
											log.Error(err, "unable to delete the deployment", "deployment", deploy2)
											//delete(activeDeploys,delete2.index)
											continue
										} else {
											log.V(0).Info("deleted the deployment", "deployment", deploy2)
											//delete(activeDeploys, delete2.index)
											_,err = r.DeleteDeployList(activeDeploys,deploy2.index,deploy2.deploy)
											if err != nil{
												continue
											}
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}

	if backupreplicas != int(*backdeploy.Spec.BackupReplicas) {
		//backup scale out
		if backupreplicas < int(*backdeploy.Spec.BackupReplicas) { //scale out
			var replicas int32 = int32(*backdeploy.Spec.BackupReplicas) - int32(backupreplicas)
			//tmpvalue := *backdeploy.Status.LastID

			//deploy, err := createDeployment(ctx, r, &backdeploy, req, backupType, &replicas, nil)
			tmpvalue, deploy,err:= createDeployment(ctx,r,&backdeploy,req,backupType,nowScheduleStrategy,&replicas,nil)
			if err != nil {
				log.Error(err, "create backup deployment fault")
				//ctrl.Result{}, err
				ctrlresults = ctrl.Result{}
				errdefer = err
				return
			}
			if deploy != nil{
				backDeploys[int(tmpvalue)] = deploy
			}
			//err.Error()
			//goto RECONCILED
		} else if backupreplicas > int(r.ReadBackReplicas(&backdeploy)) {
			//*backdeploy.Spec.BackupReplicas
			deletingNums := backupreplicas - int(r.ReadBackReplicas(&backdeploy))
			tmpStatedMaps := make(map[elasticscalev1.DeployState]map[int]*appsv1.Deployment)
			tmpStatedMaps[elasticscalev1.BackupState] = backDeploys
			//tmpStatedMaps[elasticscalev1.ActiveState] = activeDeploys
			deletes,scalein := r.findScaleInDeploys(tmpStatedMaps,deletingNums,ScaleInBack)

			if len(deletes[elasticscalev1.BackupState]) > 0 {
				for _, delete2 := range deletes[elasticscalev1.BackupState] {
					if err := r.Delete(ctx, delete2.deploy, client.PropagationPolicy(deploy_delete_policy)); client.IgnoreNotFound(err) != nil {
						log.Error(err, "unable to delete the deployment", "deployment", delete2)
						//delete(activeDeploys,delete2.index)
						continue
					} else {
						log.V(0).Info("deleted the deployment", "deployment", delete2)
						//delete(activeDeploys, delete2.index)
						_,err = r.DeleteDeployList(backDeploys,delete2.index,delete2.deploy)
						if err != nil{
							continue
						}
					}
				}
			}

			if len(scalein[elasticscalev1.BackupState]) > 0{
				for to_in, deploys2 := range scalein[elasticscalev1.BackupState] {
					for _, deploy2 := range deploys2 {
						//*(deploy2.deploy.Spec.Replicas)
						if (to_in) >=  r.ReadDeploySpecReplicas(deploy2.deploy){
							err := r.DeleteDeploy(ctx, deploy2.deploy)
							if err != nil {
								log.Error(err, "unable to delete the deployment", "deployment", deploy2)
								//delete(waitDeploys,deploy2.index)
								continue
							}
							_, err = r.DeleteDeployList(backDeploys,deploy2.index,deploy2.deploy)
							if err != nil {
								continue
							}
						} else {
							err,okscalein := r.Scalein(ctx, deploy2.deploy, to_in)
							if err != nil {
								log.Error(err, "unable to scale in the deployment", "deployment", deploy2)
								continue
							}else{
								if okscalein == false{
									if err = r.Delete(ctx, deploy2.deploy, client.PropagationPolicy(deploy_delete_policy)); client.IgnoreNotFound(err) != nil {
										log.Error(err, "unable to delete the deployment", "deployment", deploy2)
										//delete(activeDeploys,delete2.index)
										continue
									} else {
										log.V(0).Info("deleted the deployment", "deployment", deploy2)
										//delete(activeDeploys, delete2.index)
										_,err = r.DeleteDeployList(backDeploys,deploy2.index,deploy2.deploy)
										if err != nil{
											continue
										}
									}
								}
							}
						}
					}
				}
			}



			//var backs []int
			//for k, _ := range backDeploys {
			//	backs = append(backs, k)
			//}
			//sort.Ints(backs)
			//deletedNums := 0
			//for _, v := range backs {
			//	deploy := backDeploys[v]
			//	if deletedNums+int(*deploy.Spec.Replicas) <= deletingNums {
			//		deployReplicas := int(*deploy.Spec.Replicas)
			//		err := r.DeleteDeploy(ctx, deploy)
			//		if err != nil {
			//			log.Error(err, "delete backup failed")
			//ctrl.Result{}, err
			//			return
			//		}
			//		delete(backDeploys, v)
			//		deletedNums += deployReplicas
			//	} else if deletedNums < deletingNums {
			//		moreThanDeploys := deletingNums - deletedNums
			//		*deploy.Spec.Replicas -= int32(moreThanDeploys)
			//		if *deploy.Spec.Replicas <= 0 {
			//			log.Info("BackupDeployments: scale in amount exception")
			//ctrl.Result{}, nil
			//			return
			//		}
			//		break
			//	}
			//}
			//goto RECONCILED
		}
	}

	//RECONCILED:
	//	{
	//		func() {
	//			ToBeUpdate := make(map[elasticscalev1.DeployState]map[int]*appsv1.Deployment)
	//			ToBeUpdate[elasticscalev1.ActiveState] = activeDeploys
	//			ToBeUpdate[elasticscalev1.BackupState] = backDeploys
	//			ToBeUpdate[elasticscalev1.WaitingState] = waitDeploys
	//			errdefer = r.UpdateBackDeployList(ctx, log, &backdeploy, ToBeUpdate)
	//			if errdefer != nil {
	//				log.Error(errdefer, "update status list error")
	//			}
	//			errdefer = r.Update(ctx, &backdeploy)
	//			if errdefer != nil {
	//				log.Error(errdefer, "update this backupdeployment")
	//			}
	//
	//		}()
	//	}
	//	for _, activeDeploy := range activeDeploys {
	//		deployRef, err := ref.GetReference(r.Scheme, activeDeploy)
	//		if err != nil {
	//			log.Error(err, "unable to make reference to the running deployment", "deployment", activeDeploy)
	//			continue
	//		}
	//		backdeploy.Status.Active = append(backdeploy.Status.Active, *deployRef)
	//	}
	//
	//	for _, backDeploy := range backDeploys {
	//		deployRef, err := ref.GetReference(r.Scheme, backDeploy)
	//		if err != nil {
	//			log.Error(err, "unable to make reference to the running deployment", "deployment", backDeploy)
	//			continue
	//		}
	//		backdeploy.Status.Back = append(backdeploy.Status.Back, *deployRef)
	//	}
	//
	//	for _, waitDeploy := range waitDeploys {
	//		deployRef, err := ref.GetReference(r.Scheme, waitDeploy)
	//		if err != nil {
	//			log.Error(err, "unable to make reference to the running deployment", "deployment", waitDeploy)
	//			continue
	//		}
	//		backdeploy.Status.Wait = append(backdeploy.Status.Wait, *deployRef)
	//	}
	//
	//	//for i,deploy
	//	//r.Update(ctx, &backdeploy)
	//ctrl.Result{},
	ctrlresults = ctrl.Result{}
	errdefer = nil
	return
}



func (r *BackupDeploymentReconciler)GenerateCreationPlan(usingNodes map[string]int,usingbacknum int,aim_scale_out int,strategy ScheduleStrategy,alloc_strate AllocateStrategy,unitreplicas int)(map[deployType][]map[string]int,map[deployType][]int){
	//deployType
	total_back_resources := make(map[string]int)
	if usingNodes != nil{
		for k,v := range usingNodes{
			if v > 0{
				total_back_resources[k] = v
			}
		}
	}
	if usingbacknum < 0{
		usingbacknum = 0
	}
	if aim_scale_out < 0{
		aim_scale_out = 0
	}

	if len(total_back_resources) <= 0 || total_back_resources == nil || len(usingNodes) <= 0 || usingNodes == nil{
		usingbacknum = 0
	}


	results := make(map[deployType][]map[string]int)
	resultsreplicas := make(map[deployType][]int)
	//results[activeType] = make([]map[string]int,0)
	//resultsreplicas[activeType] = make([]int,0)
	allocated_backup := 0
	if usingbacknum > 0 && len(usingNodes) > 0{
		can_use_back := usingbacknum
		if aim_scale_out < usingbacknum{
			can_use_back = aim_scale_out
		}
		results[waitingType] = make([]map[string]int,0)
		resultsreplicas[waitingType] = make([]int,0)
		tmpresultsreplicas := make([]int,0)
		numFromBack := 1
		if unitreplicas <= 0{
			numFromBack = 1
			tmpresultsreplicas = append(tmpresultsreplicas,can_use_back)
			//resultsreplicas[activeType] = append(resultsreplicas[activeType],aim_scale_out - usingbacknum)
		}else{
			if can_use_back % DefaultUseHosts > 0{
				numFromBack = int(can_use_back / DefaultUseHosts)+1
				//for j in r
				for j:=0;j<numFromBack-1;j++{
					tmpresultsreplicas = append(tmpresultsreplicas,DefaultUseHosts)
				}
				tmpresultsreplicas = append(tmpresultsreplicas,can_use_back % DefaultUseHosts)

			}else{
				numFromBack = int(can_use_back / DefaultUseHosts)
				for j := 0;j<numFromBack;j++{
					tmpresultsreplicas = append(tmpresultsreplicas,DefaultUseHosts)
				}
			}
		}
		//numFromBack :=
		real_backups := make([]int,0)
		for _,value := range tmpresultsreplicas{
			if len(total_back_resources) <=0{
				break
			}
			tmpMap := make(map[string]int)
			gots := 0
			for;gots < value;{
				if len(total_back_resources) <=0{
					break
				}
				can_be_assigned,keysets := getNumToNode(total_back_resources)
				if strategy == ScheduleOnSameHostsHard || strategy == ScheduleOnSameHosts{
					can_max_nodes := can_be_assigned[keysets[len(keysets)-1]]//gots +=
					can_max := keysets[len(keysets)-1]
					if alloc_strate == BinPacking{
						idx := -1
						for id0 :=0;id0 < len(keysets);id0++{
							if keysets[id0] > 0{
								idx = id0
								break
							}
						}
						if idx > 0 && idx < len(keysets)-1{
							can_max_nodes = can_be_assigned[keysets[idx]]//gots +=
							can_max = keysets[idx]
						}
					}

					if can_max > value - gots{
						can_max = value - gots
					}

					if _,ok := total_back_resources[can_max_nodes[0]];ok{
						total_back_resources[can_max_nodes[0]] -= can_max
						if total_back_resources[can_max_nodes[0]] <= 0{
							delete(total_back_resources,can_max_nodes[0])
						}
						if _,ok0 := tmpMap[can_max_nodes[0]];!ok0{
							tmpMap[can_max_nodes[0]] = can_max
						}else{
							tmpMap[can_max_nodes[0]] += can_max
						}
						gots += can_max
					}
				}else{
					tmpresults, tmpgots := RoundBinFunc(total_back_resources,value,alloc_strate)
					if len(tmpresults) > 0 && tmpresults != nil{
						for tmpk,tmpv := range tmpresults{
							tmpMap[tmpk] = tmpv
						}
						gots = tmpgots
					}
					break
				}
			}
			//gots +=
			allocated_backup += gots
			real_backups = append(real_backups,gots)
			results[waitingType] = append(results[waitingType],tmpMap)
		}
		resultsreplicas[waitingType] = []int{}
		for _,v := range real_backups{
			//[j] = v
			resultsreplicas[waitingType] = append(resultsreplicas[waitingType],v)
		}
		resultsreplicas[waitingType] = resultsreplicas[waitingType][:len(real_backups)]
	}
	activeNum := aim_scale_out - allocated_backup
	if activeNum > 0{
		results[activeType] = make([]map[string]int,0)
		resultsreplicas[activeType] = make([]int,0)
		numNative := 1
		if unitreplicas <= 0{
			resultsreplicas[activeType] = append(resultsreplicas[activeType],activeNum)
			tmpMap := make(map[string]int)
			results[activeType] = append(results[activeType],tmpMap)
		}else{
			if activeNum % DefaultUseHosts > 0{
				numNative = int(activeNum / DefaultUseHosts)+1
				for j := 0;j< numNative - 1;j++{
					resultsreplicas[activeType] = append(resultsreplicas[activeType],DefaultUseHosts)
					tmpMap := make(map[string]int)
					results[activeType] = append(results[activeType],tmpMap)
				}
				resultsreplicas[activeType] = append(resultsreplicas[activeType],activeNum % DefaultUseHosts)
				tmpMap := make(map[string]int)
				results[activeType] = append(results[activeType],tmpMap)
			}else{
				numNative = activeNum / DefaultUseHosts
				for j := 0;j<numNative;j++{
					resultsreplicas[activeType] = append(resultsreplicas[activeType],DefaultUseHosts)
					tmpMap := make(map[string]int)
					results[activeType] = append(results[activeType],tmpMap)
				}
			}
		}
	}
	return results,resultsreplicas
}

func RoundBinFunc(input map[string]int,upperbound int,alloc_strategy AllocateStrategy) (map[string]int,int){
	if len(input) <= 0 || input == nil{
		return nil,0
	}
	tmpmap := make(map[string]int)
	totalNodes := make([]string,0)
	for k,v := range tmpmap{
		if v > 0{
			tmpmap[k] = v
			totalNodes = append(totalNodes,k)
		}
	}
	results := make(map[string]int)


	//totlNodes :=
	if len(totalNodes) > 0{
		allocated := 0
		for;allocated < upperbound;{
			if len(tmpmap) <= 0 || tmpmap == nil{
				break
			}
			NumToNode,keyset := getNumToNode(tmpmap)
			if alloc_strategy == BinPacking{
				for idx := 0;idx < len(keyset);idx++{
					if allocated >= upperbound{
						break
					}
					for _,node := range NumToNode[idx]{
						if allocated >= upperbound{
							break
						}
						if _,ok := tmpmap[node];!ok{
							continue
						}else{
							if tmpmap[node] <= 0{
								delete(tmpmap,node)
								continue
							}
							if _,ok2 := results[node];!ok2{
								results[node] = 0
							}
							results[node] += 1
							allocated += 1
							tmpmap[node] -= 1
							if tmpmap[node] <= 0{
								delete(tmpmap,node)
							}
						}
					}
				}
			}else{
				for idx := len(keyset)-1;idx >= 0;idx--{
					if allocated >= upperbound{
						break
					}
					for _,node := range NumToNode[idx]{
						if allocated >= upperbound{
							break
						}
						if _,ok2 := results[node];!ok2{
							continue
						}else{
							if tmpmap[node] <= 0{
								delete(tmpmap,node)
								continue
							}
							if _,ok := results[node];!ok{
								results[node] = 0
							}
							results[node] += 1
							allocated += 1
							tmpmap[node] -= 1
							if tmpmap[node] <= 0{
								delete(tmpmap,node)
							}
						}
					}
				}
			}
		}

		return results,allocated
	}else{
		return nil,0
	}
}

func differ_after_scalein(before,after map[string]int) map[string]int{
	before_key := make([]string,0)
	after_key := make([]string,0)
	results := make(map[string]int)
	if before == nil{
		before_key = []string{}
	}else{
		for k,_ := range before{
			before_key = append(before_key,k)
		}
	}

	if after == nil{
		after_key = []string{}
	}else{
		for k,_ := range after{
			after_key = append(after_key,k)
		}
	}

	for _,k := range before_key{
		if _,ok := after[k];ok{
			results[k] = before[k] - after[k]
		}else{
			results[k] = before[k]
		}
	}

	for _,k := range after_key{
		if _,ok := before[k];!ok{
			results[k] = after[k]
		}
	}
	return results
}

func getNumToNode(input map[string]int) (map[int][]string,[]int){
	results := make(map[int][]string)
	keyset := make([]int,0)
	for key,value := range input{
		if _,ok := results[value];!ok{
			results[value] = make([]string,0)
			keyset = append(keyset,value)
		}
		results[value] = append(results[value],key)

		sort.Ints(keyset)
	}
	return results,keyset
}

func (r *BackupDeploymentReconciler)FindAllNodeMap(ctx context.Context,input map[int]*appsv1.Deployment)(map[string]int,int){
	if input == nil{
		return nil,0
	}
	allLabels := make(map[string]int)
	totalnumber := 0
	//idx
	for _,deploy := range input{
		tmpLabels := findNodenames(ctx,r,deploy)
		if tmpLabels != nil && len(tmpLabels)>0{
			for node,number := range tmpLabels{
				if _,ok := allLabels[node];!ok{
					allLabels[node] = 0
				}
				allLabels[node] = allLabels[node] + number
				totalnumber += number
			}
		}
	}
	return allLabels,totalnumber

}

func (r *BackupDeploymentReconciler) findDeleteScaleBackup(ctx context.Context,input map[int]*appsv1.Deployment,aim_scale_out int)(map[string]int,[]int,int,int,int){
	r.StatusLock.RLock()
	defer r.StatusLock.RUnlock()
	var backid []int
	for k, _ := range input{
		backid = append(backid, k)
	}
	sort.Ints(backid)
	//var backLabels []string
	scaleinkey := -1
	scaleinnum := 0

	resultsmap := make(map[string]int)
	deletedids := make([]int,0)

	now_scaled_out := 0

	for _,v := range backid{
		tmpDeploy := input[v]
		tmpReplicas := int(input[v].Status.Replicas)
		if now_scaled_out + tmpReplicas >= aim_scale_out{
			if now_scaled_out + tmpReplicas == aim_scale_out{
				tempLabelsMap := findNodenames(ctx, r, tmpDeploy)
				if tempLabelsMap == nil{
					continue
				}
				for mapk,mapv := range tempLabelsMap{
					if _,ok := resultsmap[mapk];!ok{
						resultsmap[mapk] = mapv
					}else{
						resultsmap[mapk] += mapv
					}
				}
				now_scaled_out += tmpReplicas
				deletedids = append(deletedids,v)
				break
			}else{
				scaleinkey = v
				scaleinnum = tmpReplicas - (aim_scale_out - now_scaled_out)
				now_scaled_out = aim_scale_out
				break
			}
		}else{
			tempLabelsMap := findNodenames(ctx, r, tmpDeploy)
			if tempLabelsMap == nil{
				continue
			}
			for mapk,mapv := range tempLabelsMap{
				if _,ok := resultsmap[mapk];!ok{
					resultsmap[mapk] = mapv
				}else{
					resultsmap[mapk] += mapv
				}
			}
			now_scaled_out += tmpReplicas
			deletedids = append(deletedids,v)
		}
	}
	return resultsmap,deletedids,scaleinkey,scaleinnum,now_scaled_out
}

func (r *BackupDeploymentReconciler) Scalein(ctx context.Context, deploy *appsv1.Deployment, to_in int) (error,bool) {
	if int(r.ReadDeployReplicas(deploy) - int(to_in))<=0{
		return nil,false
	}
	r.StatusLock.Lock()
	tmp_rep := *(deploy.Spec.Replicas)
	*(deploy.Spec.Replicas) = int32(tmp_rep - int32(to_in))
	r.StatusLock.Unlock()
	//if *(deploy.Spec.Replicas) <= 0{
	//	return
	//}
	err := r.Update(ctx, deploy)
	if err != nil {
		if apierrors.IsNotFound(err) {
			err = r.Create(ctx, deploy)
		}
		if client.IgnoreNotFound(err) != nil {
			return err,true
		}
	}
	return nil,true
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

//func getIndexedDeploy (deploys []*appsv1.Deployment)
// apierrors.IsNotFound(err)

func (r *BackupDeploymentReconciler) DeleteDeploy(ctx context.Context, deploy *appsv1.Deployment) error {
	if err := r.Delete(ctx, deploy, client.PropagationPolicy(deploy_delete_policy)); client.IgnoreNotFound(err) != nil {
		return err
	} else {
		return nil
	}
}

func (r *BackupDeploymentReconciler) findScaleInDeploys(input map[elasticscalev1.DeployState]map[int]*appsv1.Deployment,aim_scale_in int,scalintype ScaleInType)  (map[elasticscalev1.DeployState][]*IndexedDeploy,map[elasticscalev1.DeployState]map[int][]*IndexedDeploy){

	now_scale := 0
	deletes := make(map[elasticscalev1.DeployState][]*IndexedDeploy)
	//deletes["wait"] = []*IndexedDeploy{}
	//deletes["active"] = []*IndexedDeploy{}
	scalein := make(map[elasticscalev1.DeployState]map[int][]*IndexedDeploy)
	//scalein["wait"] = make(map[int][]*IndexedDeploy)
	//scalein["active"] = make(map[int][]*IndexedDeploy)
	r.StatusLock.RLock()
	defer r.StatusLock.RUnlock()

	if scalintype == ScaleInRunning{
		if _,ok := input[elasticscalev1.WaitingState];ok && len(input[elasticscalev1.WaitingState]) > 0 {
			deletes[elasticscalev1.WaitingState] = []*IndexedDeploy{}
			scalein[elasticscalev1.WaitingState] = make(map[int][]*IndexedDeploy)
			waitid := []int{}
			for k, _ := range input[elasticscalev1.WaitingState] {
				waitid = append(waitid, k)
			}
			sort.Ints(waitid)
			for j := len(waitid) - 1; j >= 0; j-- {
				tmp_rep := int(*input[elasticscalev1.WaitingState][waitid[j]].Spec.Replicas)
				if now_scale+tmp_rep <= aim_scale_in {
					select_wait := new(IndexedDeploy)
					select_wait.deploy = input[elasticscalev1.WaitingState][waitid[j]]
					select_wait.index = waitid[j]
					deletes[elasticscalev1.WaitingState] = append(deletes[elasticscalev1.WaitingState], select_wait)
					now_scale = now_scale + tmp_rep
				} else {
					if now_scale >= aim_scale_in {
						break
					}
					tmp_scale_in, _ := scalein[elasticscalev1.WaitingState][int(aim_scale_in-now_scale)]
					select_wait := new(IndexedDeploy)
					select_wait.deploy = input[elasticscalev1.WaitingState][waitid[j]]
					select_wait.index = waitid[j]
					tmp_scale_in = append(tmp_scale_in, select_wait)
					scalein[elasticscalev1.WaitingState][int(aim_scale_in-now_scale)] = tmp_scale_in
					now_scale = aim_scale_in
					break
				}
			}
		}
		if now_scale < aim_scale_in {
			deletes[elasticscalev1.ActiveState] = []*IndexedDeploy{}
			scalein[elasticscalev1.ActiveState] = make(map[int][]*IndexedDeploy)
			if _,ok := input[elasticscalev1.ActiveState];ok && len(input[elasticscalev1.ActiveState]) > 0 {
				activeid := []int{}
				for k, _ := range input[elasticscalev1.ActiveState] {
					activeid = append(activeid, k)
				}
				sort.Ints(activeid)
				for j := len(activeid) - 1; j >= 0; j-- {
					tmp_rep := int(*input[elasticscalev1.ActiveState][activeid[j]].Spec.Replicas)
					if now_scale+tmp_rep <= aim_scale_in {
						select_active := new(IndexedDeploy)
						select_active.deploy = input[elasticscalev1.ActiveState][activeid[j]]
						select_active.index = activeid[j]
						deletes[elasticscalev1.ActiveState] = append(deletes[elasticscalev1.ActiveState], select_active)
						now_scale = now_scale + tmp_rep
					} else {
						if now_scale >= aim_scale_in {
							break
						}
						tmp_scale_in, _ := scalein[elasticscalev1.ActiveState][int(aim_scale_in-now_scale)]
						select_active := new(IndexedDeploy)
						select_active.deploy = input[elasticscalev1.ActiveState][activeid[j]]
						select_active.index = activeid[j]
						tmp_scale_in = append(tmp_scale_in, select_active)
						scalein[elasticscalev1.ActiveState][int(aim_scale_in-now_scale)] = tmp_scale_in
						now_scale = aim_scale_in
						break
					}
				}
			}
		}
	}else{
		if _,ok := input[elasticscalev1.BackupState];ok && len(input[elasticscalev1.BackupState]) > 0 {
			deletes[elasticscalev1.BackupState] = []*IndexedDeploy{}
			scalein[elasticscalev1.BackupState] = make(map[int][]*IndexedDeploy)
			backid := []int{}
			for k, _ := range input[elasticscalev1.BackupState] {
				backid = append(backid, k)
			}
			sort.Ints(backid)
			for j := len(backid) - 1; j >= 0; j-- {
				tmp_rep := int(*input[elasticscalev1.BackupState][backid[j]].Spec.Replicas)
				if now_scale+tmp_rep <= aim_scale_in {
					select_back := new(IndexedDeploy)
					select_back.deploy = input[elasticscalev1.BackupState][backid[j]]
					select_back.index = backid[j]
					deletes[elasticscalev1.BackupState] = append(deletes[elasticscalev1.BackupState], select_back)
					now_scale = now_scale + tmp_rep
				} else {
					if now_scale >= aim_scale_in {
						break
					}
					tmp_scale_in, _ := scalein[elasticscalev1.BackupState][int(aim_scale_in-now_scale)]
					select_back := new(IndexedDeploy)
					select_back.deploy = input[elasticscalev1.BackupState][backid[j]]
					select_back.index = backid[j]
					tmp_scale_in = append(tmp_scale_in, select_back)
					scalein[elasticscalev1.BackupState][int(aim_scale_in-now_scale)] = tmp_scale_in
					now_scale = aim_scale_in
					break
				}
			}
		}
	}
	return deletes,scalein
}

// +kubebuilder:docs-gen:collapse=DeleteDeploy

// 根据指定的replicas和nodename创建deployment
//*[]string
//two ways of create / scheduling method:
// 1. the pod in the same of mutiple hosts as on deployment
// 2. the pod scheduled to different
func createDeployment(ctx context.Context, r *BackupDeploymentReconciler, deploycrd *elasticscalev1.BackupDeployment,
	req ctrl.Request, types deployType,strategy ScheduleStrategy,replicas *int32, hostLabels map[string]int) (int,*appsv1.Deployment, error) {
	log := r.Log.WithValues("func", "createDeployment")
	if *replicas <= 0{
		return -1,nil,nil
	}
	//useID := r.ReadBackupLastID(deploycrd)
	useID,_,err := r.UpdateBackupLastID(ctx,deploycrd,1)
	if err != nil{
		return -1,nil, err
	}
	deployName := strings.Join([]string{deploycrd.Name, "-", strconv.Itoa(int(useID))}, "")
	if types == backupType{
		deployName = strings.Join([]string{"backup","-",deployName},"")
	}

	deploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   deploycrd.Namespace,
			Name:  deployName,
			Annotations: map[string]string{DeployIDAnnotation: strconv.FormatInt(*deploycrd.Status.LastID, 10)},
		},
		Spec: deploycrd.Spec.BackupSpec,
	}
	deploy.Annotations[ScheduledTimeAnnotation] = strconv.FormatInt(time.Now().Unix(),10)
	if types == backupType{
		deploy.Spec = deploycrd.Spec.BackupSpec
		deploy.Annotations[DeployStateAnnotation] = string(elasticscalev1.BackupState)
	}else if (types == waitingType) || (types == activeType){
		deploy.Spec = deploycrd.Spec.RunningSpec
		//if types == waitingType{
		deploy.Annotations[DeployStateAnnotation] = string(elasticscalev1.WaitingState)
		//}else{ metadata:
		//	deploy.Annotations[DeployStateAnnotation] = string(elasticscalev1.ActiveState)
		//}
	}else{
		return -1,nil, errors.New("do not input an favorable type of deployment")
	}
	//deploy.
	deploy.Spec.Selector.MatchLabels[deployIDKey] = deployName
	deploy.Spec.Template.Labels[deployIDKey] = deployName

	switch strategy {
	case ScheduleOnSameHosts:
		log.V(1).Info("Strategy for this deploy is ScheduleOnSameHosts")
	case ScheduleRoundBin:
		log.V(1).Info("Strategy for this deploy is ScheduleRoundBin")
	case ScheduleOnSameHostsHard:
		log.V(1).Info("Strategy for this deploy is ScheduleOnSameHostsHard")
	case ScheduleRoundBinHard:
		log.V(1).Info("Strategy for this deploy is ScheduleRoundBinHard")
	default:
		strategy = ScheduleOnSameHosts

	}

	if strategy == ScheduleOnSameHosts || strategy == ScheduleRoundBin{
		nowweight := 1.0
		if strategy == ScheduleRoundBin {
			nowweight = 1.2
		}else {
			nowweight = 1.0
		}
		var nodeaffinity *corev1.NodeAffinity = nil
		var podantiaffinity *corev1.PodAntiAffinity = nil
		if hostLabels != nil && len(hostLabels) > 0{
			nodeaffinity = &corev1.NodeAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{},
			}
			podantiaffinity = &corev1.PodAntiAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
					{
						Weight: int32(float32(PodAntiAffinityWeight)*float32(nowweight)),
						PodAffinityTerm: corev1.PodAffinityTerm{
							LabelSelector: &metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key: deployIDKey,
										Values: []string{deployName},
										Operator: metav1.LabelSelectorOpIn,
									},
								},
							},
							Namespaces: []string{deployName},
							TopologyKey: "kubernetes.io/hostname",
						},
					},
				},
			}
			weighttohosts := make(map[int][]string)

			for hostkey,hostvalue := range hostLabels{
				//nodeaffinity.PreferredDuringSchedulingIgnoredDuringExecution = append(nodeaffinity.PreferredDuringSchedulingIgnoredDuringExecution,corev1.PreferredSchedulingTerm{
				//	We
				//})
				tmpscore := PreferrNodeWeight(hostvalue)

				if _,ok := weighttohosts[tmpscore];!ok{
					weighttohosts[tmpscore] = make([]string,0)
					weighttohosts[tmpscore] = append(weighttohosts[tmpscore],hostkey)
				}
			}

			for score,_ := range weighttohosts{
				nodeaffinity.PreferredDuringSchedulingIgnoredDuringExecution = append(nodeaffinity.PreferredDuringSchedulingIgnoredDuringExecution,corev1.PreferredSchedulingTerm{
					Weight: int32(score),
					Preference: corev1.NodeSelectorTerm{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key: "kubernetes.io/hostname",
								Operator: corev1.NodeSelectorOperator("In"),
								Values: weighttohosts[score],
							},
						},
					},
				})
			}

		} else{
			//nowweight := 1.0
			if strategy == ScheduleOnSameHosts {
				nowweight = 0.5
			}
			podantiaffinity = &corev1.PodAntiAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
					{
						Weight: int32(float32(PodAntiAffinityWeight)*float32(nowweight)),
						PodAffinityTerm: corev1.PodAffinityTerm{
							LabelSelector: &metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key: deployIDKey,
										Values: []string{deployName},
										Operator: metav1.LabelSelectorOpIn,
									},
								},
							},
							Namespaces: []string{deployName},
							TopologyKey: "kubernetes.io/hostname",
						},
					},
				},
			}
		}
		allocAffinity := new(corev1.Affinity)
		hasaffinity := false
		if nodeaffinity != nil {
			allocAffinity.NodeAffinity = nodeaffinity
			hasaffinity = true
		}
		if podantiaffinity != nil {
			allocAffinity.PodAntiAffinity = podantiaffinity
			hasaffinity = true
		}
		if hasaffinity {
			deploy.Spec.Template.Spec.Affinity = allocAffinity
		}

	}else if strategy == ScheduleOnSameHostsHard{
		var nodeaffinity *corev1.NodeAffinity = nil
		var podantiaffinity *corev1.PodAntiAffinity = nil
		if hostLabels != nil && len(hostLabels) > 0{
			nodeaffinity = &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{},
					},
				},
				PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{},
				//PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{},
			}
			podantiaffinity = &corev1.PodAntiAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
					{
						Weight: int32(PodAntiAffinityWeight/2),
						PodAffinityTerm: corev1.PodAffinityTerm{
							LabelSelector: &metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key: deployIDKey,
										Values: []string{deployName},
										Operator: metav1.LabelSelectorOpIn,
									},
								},
							},
							Namespaces: []string{deployName},
							TopologyKey: "kubernetes.io/hostname",
						},
					},
				},
			}
			weighttohosts := make(map[int][]string)
			for hostkey,hostvalue := range hostLabels{
				nodeaffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms = append(nodeaffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms,corev1.NodeSelectorTerm{
					MatchExpressions: []corev1.NodeSelectorRequirement{
						{
							Key: "kubernetes.io/hostname",
							Operator: corev1.NodeSelectorOperator("In"),
							Values: []string{hostkey},
						},
					},
				})
				tmpscore := PreferrNodeWeight(hostvalue)

				if _,ok := weighttohosts[tmpscore];!ok{
					weighttohosts[tmpscore] = make([]string,0)
					weighttohosts[tmpscore] = append(weighttohosts[tmpscore],hostkey)
				}
			}
			for score,_ := range weighttohosts{
				nodeaffinity.PreferredDuringSchedulingIgnoredDuringExecution = append(nodeaffinity.PreferredDuringSchedulingIgnoredDuringExecution,corev1.PreferredSchedulingTerm{
					Weight: int32(score),
					Preference: corev1.NodeSelectorTerm{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key: "kubernetes.io/hostname",
								Operator: corev1.NodeSelectorOperator("In"),
								Values: weighttohosts[score],
							},
						},
					},
				})
			}
		}
		allocAffinity := new(corev1.Affinity)
		hasaffinity := false
		if nodeaffinity != nil {
			allocAffinity.NodeAffinity = nodeaffinity
			hasaffinity = true
		}
		if podantiaffinity != nil {
			allocAffinity.PodAntiAffinity = podantiaffinity
			hasaffinity = true
		}
		if hasaffinity {
			deploy.Spec.Template.Spec.Affinity = allocAffinity
		}
	}else{
		nowweight := 1.5
		if strategy == ScheduleRoundBin {
			nowweight = 1.2
		}else {
			nowweight = 1.5
		}
		var nodeaffinity *corev1.NodeAffinity = nil
		var podantiaffinity *corev1.PodAntiAffinity = nil
		if hostLabels != nil && len(hostLabels) > 0{
			nodeaffinity = &corev1.NodeAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{},
			}
			podantiaffinity = &corev1.PodAntiAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
					{
						Weight: int32(float32(PodAntiAffinityWeight)*float32(nowweight)),
						PodAffinityTerm: corev1.PodAffinityTerm{
							LabelSelector: &metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key: deployIDKey,
										Values: []string{deployName},
										Operator: metav1.LabelSelectorOpIn,
									},
								},
							},
							Namespaces: []string{deployName},
							TopologyKey: "kubernetes.io/hostname",
						},
					},
				},
			}
			weighttohosts := make(map[int][]string)

			for hostkey,_ := range hostLabels{
				//nodeaffinity.PreferredDuringSchedulingIgnoredDuringExecution = append(nodeaffinity.PreferredDuringSchedulingIgnoredDuringExecution,corev1.PreferredSchedulingTerm{
				//	We
				//})
				tmpscore := PreferrNodeWeight(MaxSafeReplicasOnOneHost)

				if _,ok := weighttohosts[tmpscore];!ok{
					weighttohosts[tmpscore] = make([]string,0)
					weighttohosts[tmpscore] = append(weighttohosts[tmpscore],hostkey)
				}
			}

			for score,_ := range weighttohosts{
				nodeaffinity.PreferredDuringSchedulingIgnoredDuringExecution = append(nodeaffinity.PreferredDuringSchedulingIgnoredDuringExecution,corev1.PreferredSchedulingTerm{
					Weight: int32(score),
					Preference: corev1.NodeSelectorTerm{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							{
								Key: "kubernetes.io/hostname",
								Operator: corev1.NodeSelectorOperator("In"),
								Values: weighttohosts[score],
							},
						},
					},
				})
			}

		} else{
			//nowweight := 1.0
			//if strategy == ScheduleOnSameHosts {
			//	nowweight = 0.5
			//}
			podantiaffinity = &corev1.PodAntiAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
					{
						Weight: int32(float32(PodAntiAffinityWeight)*float32(nowweight)),
						PodAffinityTerm: corev1.PodAffinityTerm{
							LabelSelector: &metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key: deployIDKey,
										Values: []string{deployName},
										Operator: metav1.LabelSelectorOpIn,
									},
								},
							},
							Namespaces: []string{deployName},
							TopologyKey: "kubernetes.io/hostname",
						},
					},
				},
			}
		}
		allocAffinity := new(corev1.Affinity)
		hasaffinity := false
		if nodeaffinity != nil {
			allocAffinity.NodeAffinity = nodeaffinity
			hasaffinity = true
		}
		if podantiaffinity != nil {
			allocAffinity.PodAntiAffinity = podantiaffinity
			hasaffinity = true
		}
		if hasaffinity {
			deploy.Spec.Template.Spec.Affinity = allocAffinity
		}
	}
	//if strategy == ScheduleRoundBin{
	//	if hostLabels != nil && len(hostLabels) > 0{
	//
	//	}
	//}

	//if types == waitingType {
	//	//deploy.Spec = deploycrd.Spec.RunningSpec
	//	//deploy.Annotations[DeployStateAnnotation] = string(elasticscalev1.WaitingState)
	//	affinity := corev1.NodeAffinity{
	//		PreferredDuringSchedulingIgnoredDuringExecution: []corev1.PreferredSchedulingTerm{
	//			{
	//				Weight: 100,
	//				Preference: corev1.NodeSelectorTerm{
	//					MatchExpressions: []corev1.NodeSelectorRequirement{
	//						{
	//							Key:      "kubernetes.io/hostname",
	//							Operator: corev1.NodeSelectorOperator("In"),
	//							Values:   *hostLabels,
	//						},
	//					},
	//				},
	//			},
	//		},
	//	}
	//
	//	allocAffinity := new(corev1.Affinity)
	//	allocAffinity.NodeAffinity = &affinity
	//	allocAffinity.PodAntiAffinity
	//	deploy.Spec.Template.Spec.Affinity = allocAffinity
	//}
	//else if types == backupType {
	//	deploy.Spec = deploycrd.Spec.BackupSpec
	//	deploy.Annotations[DeployStateAnnotation] = string(elasticscalev1.BackupState)
	//
	//}
	deploy.Spec.Replicas = replicas
	deploy.Spec.Template.Labels[elasticscalev1.DeploymentName] = deploy.Name

	if err = ctrl.SetControllerReference(deploycrd, deploy, r.Scheme); err != nil {
		log.Error(err, "deploymentSetControllerReference error")
		return -1,nil, err
	}

	deployment, err := r.AppsV1().Deployments(deploy.Namespace).Create(deploy)
	if err != nil {
		log.Error(err, "unable to create deployment in function create")
		return -1,nil, err
	}
	//*deploycrd.Status.LastID = *deploycrd.Status.LastID + 1
	log.Info("create deployment success")
	//r.Update(ctx, deploycrd)
	return int(useID),deployment, nil
}

func findNodenames(ctx context.Context, r *BackupDeploymentReconciler, dp *appsv1.Deployment) map[string]int {
	log := r.Log.WithValues("fun", "findNodenames")
	options := metav1.ListOptions{
		LabelSelector: strings.Join([]string{elasticscalev1.DeploymentName, "=", dp.Name}, ""),
	}
	podList, err := r.CoreV1().Pods(dp.Namespace).List(options)
	if err != nil {
		log.Error(err, "findNodenames error")
		return nil
	}
	var hostNames map[string]int = make(map[string]int)
	for _, pods := range podList.Items {
		node, err := r.CoreV1().Nodes().Get(pods.Spec.NodeName, metav1.GetOptions{})
		if err != nil {
			log.Error(err, "find hostname error")
			return nil
		}
		tmpHost := node.Labels["kubernetes.io/hostname"]
		if _,ok := hostNames[tmpHost];!ok{
			hostNames[tmpHost] = 0
		}
		hostNames[tmpHost] = hostNames[tmpHost]+1
	}
	return hostNames
}

// +kubebuilder:docs-gen:collapse=findLabels

func (r *BackupDeploymentReconciler) ReadBackupLastID(deploycrd *elasticscalev1.BackupDeployment) (int64){
	r.StatusLock.RLock()
	defer r.StatusLock.RUnlock()
	//deploycrd.Status.
	if deploycrd.Status.LastID == nil{
		return int64(0)
	}else{
		return *deploycrd.Status.LastID
	}
}

func (r *BackupDeploymentReconciler) ReadBackupAction(deploycrd *elasticscalev1.BackupDeployment)(string){
	r.StatusLock.RLock()
	defer r.StatusLock.RUnlock()
	return deploycrd.Spec.Action
}

func (r *BackupDeploymentReconciler)ReadStrategyStrategy(deploycrd *elasticscalev1.BackupDeployment) (ScheduleStrategy,bool) {
	r.StatusLock.RLock()
	defer r.StatusLock.RUnlock()
	statusstrategy := ScheduleOnSameHosts
	specstrategy := ScheduleOnSameHosts
	if deploycrd.Status.ScheduleStrategy != nil{
		statusstrategy = ScheduleStrategy(int(*deploycrd.Status.ScheduleStrategy))
		//return ,nil
	}
	if deploycrd.Spec.ScheduleStrategy != nil{
		specstrategy = ScheduleStrategy(int(*deploycrd.Spec.ScheduleStrategy))
		//return ScheduleStrategy(*deploycrd.Spec.ScheduleStrategy),nil
	}
	if statusstrategy != specstrategy || deploycrd.Status.ScheduleStrategy== nil{
		return specstrategy,false
	}else{
		return specstrategy,true
	}
}

func (r *BackupDeploymentReconciler)ReadAllocateStrategy(deploycrd *elasticscalev1.BackupDeployment) (AllocateStrategy,bool) {
	r.StatusLock.RLock()
	defer r.StatusLock.RUnlock()
	statusstrategy := BinPacking
	specstrategy := BinPacking
	if deploycrd.Status.AllocateStrategy != nil{
		statusstrategy = AllocateStrategy(int(*deploycrd.Status.AllocateStrategy))
		//return ,nil
	}
	if deploycrd.Spec.AllocateStrategy != nil{
		specstrategy = AllocateStrategy(int(*deploycrd.Spec.AllocateStrategy))
		//return ScheduleStrategy(*deploycrd.Spec.ScheduleStrategy),nil
	}
	if statusstrategy != specstrategy || deploycrd.Status.AllocateStrategy== nil{
		return specstrategy,false
	}else{
		return specstrategy,true
	}
}

func (r *BackupDeploymentReconciler)ReadUnitReplicas(deploycrd *elasticscalev1.BackupDeployment) (int,bool) {
	r.StatusLock.RLock()
	defer r.StatusLock.RUnlock()
	statusstrategy := 0
	specstrategy := 0
	if deploycrd.Status.UnitReplicas != nil{
		statusstrategy = (int(*deploycrd.Status.UnitReplicas))
		//return ,nil
	}
	if deploycrd.Spec.UnitReplicas != nil{
		specstrategy = (int(*deploycrd.Spec.UnitReplicas))
		//return ScheduleStrategy(*deploycrd.Spec.ScheduleStrategy),nil
	}
	if statusstrategy != specstrategy || deploycrd.Status.AllocateStrategy== nil{
		return specstrategy,false
	}else{
		return specstrategy,true
	}
}

func (r *BackupDeploymentReconciler)ReadDeploySpecReplicas(deploy *appsv1.Deployment) (int){
	r.StatusLock.RLock()
	r.StatusLock.RUnlock()
	if deploy.Spec.Replicas == nil{
		return 0
	}else{
		return int(*deploy.Spec.Replicas)
	}
}

func (r *BackupDeploymentReconciler)UpdateUnitReplicas(ctx context.Context,deploycrd *elasticscalev1.BackupDeployment) (error) {
	r.StatusLock.Lock()
	defer r.StatusLock.Unlock()
	tmpSpec := int64(DefaultUnitReplicas)
	if deploycrd.Spec.UnitReplicas != nil {
		tmpSpec = int64(*deploycrd.Spec.UnitReplicas)
	}
	*deploycrd.Status.UnitReplicas = int64(tmpSpec)
	//if ScheduleStrategy(*deploycrd.Status.ScheduleStrategy) == ScheduleStrategy(*deploycrd.Spec.ScheduleStrategy)
	err := r.Status().Update(ctx,deploycrd)
	if err != nil{
		return err
	}else{
		return nil
	}
}

func (r *BackupDeploymentReconciler)UpdateScheduleStrategy(ctx context.Context,deploycrd *elasticscalev1.BackupDeployment) (error) {
	r.StatusLock.Lock()
	defer r.StatusLock.Unlock()
	tmpSpec := int64(ScheduleOnSameHosts)
	if deploycrd.Spec.ScheduleStrategy != nil {
		tmpSpec = int64(*deploycrd.Spec.ScheduleStrategy)
	}
	*deploycrd.Status.ScheduleStrategy = int64(tmpSpec)
	//if ScheduleStrategy(*deploycrd.Status.ScheduleStrategy) == ScheduleStrategy(*deploycrd.Spec.ScheduleStrategy)
	err := r.Status().Update(ctx,deploycrd)
	if err != nil{
		return err
	}else{
		return nil
	}
}

func (r *BackupDeploymentReconciler)UpdateAllocateStrategy(ctx context.Context,deploycrd *elasticscalev1.BackupDeployment) (error) {
	r.StatusLock.Lock()
	defer r.StatusLock.Unlock()
	tmpSpec := int64(BinPacking)
	if deploycrd.Spec.AllocateStrategy != nil {
		tmpSpec = int64(*deploycrd.Spec.AllocateStrategy)
	}
	*deploycrd.Status.AllocateStrategy = int64(tmpSpec)
	//if ScheduleStrategy(*deploycrd.Status.ScheduleStrategy) == ScheduleStrategy(*deploycrd.Spec.ScheduleStrategy)
	err := r.Status().Update(ctx,deploycrd)
	if err != nil{
		return err
	}else{
		return nil
	}
}

func (r *BackupDeploymentReconciler) ReadBackupStatus(deploycrd *elasticscalev1.BackupDeployment) (string){
	r.StatusLock.RLock()
	defer r.StatusLock.RUnlock()
	//deploycrd.Status.
	return deploycrd.Status.Status
}

func (r *BackupDeploymentReconciler) ReadBackupDeployList(deploycrd *elasticscalev1.BackupDeployment)([]corev1.ObjectReference,[]corev1.ObjectReference,[]corev1.ObjectReference)  {
	r.StatusLock.RLock()
	defer r.StatusLock.RUnlock()
	return deploycrd.Status.Back,deploycrd.Status.Wait,deploycrd.Status.Active
}

func (r *BackupDeploymentReconciler) SyncBackupDelpoyList(ctx context.Context,log logr.Logger,namespace string,name string,deploycrd *elasticscalev1.BackupDeployment)(map[int]*appsv1.Deployment,map[int]*appsv1.Deployment,map[int]*appsv1.Deployment,int,int){
	var childDeploys appsv1.DeploymentList
	//req.Namespace
	//req.Name
	if err := r.List(ctx, &childDeploys, client.InNamespace(namespace), client.MatchingFields{deployOwnerKey: name}); err != nil {
		log.V(1).Info("unable to List Deployments in current namespace")
	}

	activeDeploys := make(map[int]*appsv1.Deployment)
	backDeploys := make(map[int]*appsv1.Deployment)
	waitDeploys := make(map[int]*appsv1.Deployment)


	runningreplicas := 0
	backupreplicas := 0

	for i, deploy := range childDeploys.Items {
		_, ok2, deployStatus, idx := isDeployStatus(&deploy)
		switch deployStatus {
		case string(elasticscalev1.ActiveState):
			if !ok2 {
				//activeDeploys[int(*deploycrd.Status.LastID)] = &childDeploys.Items[i]
				//tmpvalue := *backdeploy.Status.LastID
				//*backdeploy.Status.LastID = tmpvalue + 1
				//tmpID := r.ReadBackupLastID(deploycrd)
				tmpID,newID,_ := r.UpdateBackupLastID(ctx,deploycrd,1)
				activeDeploys[int(tmpID)] = &childDeploys.Items[i]
				log.V(1).Info("add the lasid from %v to %v" ,tmpID,newID)

			} else {
				activeDeploys[idx] = &childDeploys.Items[i]
			}
			runningreplicas += int(*(&childDeploys.Items[i]).Spec.Replicas)
		case string(elasticscalev1.WaitingState):
			if !ok2 {
				tmpID,newID,_ := r.UpdateBackupLastID(ctx,deploycrd,1)
				//waitDeploys[int(*backdeploy.Status.LastID)] = &childDeploys.Items[i]
				//tmpvalue := *backdeploy.Status.LastID
				//*backdeploy.Status.LastID = (tmpvalue + 1)
				waitDeploys[int(tmpID)] = &childDeploys.Items[i]
				log.V(1).Info("add the lasid from %v to %v" ,tmpID,newID)
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
	return activeDeploys,backDeploys,waitDeploys,backupreplicas,runningreplicas
}

func (r *BackupDeploymentReconciler) UpdateBackupLastID(ctx context.Context,deploycrd *elasticscalev1.BackupDeployment,increment int64) (oldID,newID int64,err error){
	r.StatusLock.Lock()
	defer r.StatusLock.Unlock()
	var tmpID int64 = 0
	if deploycrd.Status.LastID == nil{
		tmpID = 0
	} else{
		tmpID = *deploycrd.Status.LastID
	}
	newID = tmpID + increment
	*deploycrd.Status.LastID = newID
	err = r.Status().Update(ctx,deploycrd)
	if err != nil{
		newID = tmpID
	}

	return tmpID,newID,err
}

func (r *BackupDeploymentReconciler) UpdateBackupStatus(ctx context.Context,deploycrd *elasticscalev1.BackupDeployment,newStatus string) (err error)  {
	r.StatusLock.Lock()
	defer r.StatusLock.Unlock()
	deploycrd.Status.Status = newStatus
	err = r.Status().Update(ctx,deploycrd)
	return err
}

//func (r *BackupDeploymentReconciler) UpdateBackupDeployList(ctx context.Context,deploycrd *elasticscalev1.BackupDeployment,newList string) (err error)()  {
//
//}

func (r *BackupDeploymentReconciler)UpdateBackDeployList(ctx context.Context,log logr.Logger,deploycrd *elasticscalev1.BackupDeployment,deploys map[elasticscalev1.DeployState]map[int]*appsv1.Deployment) (err error) {
	r.StatusLock.Lock()
	defer r.StatusLock.Unlock()
	activeDeploys,okactive := deploys[elasticscalev1.ActiveState]
	if okactive{
		tmpactive := make([]corev1.ObjectReference,0)
		for _, activeDeploy := range activeDeploys {
			deployRef, err := ref.GetReference(r.Scheme, activeDeploy)
			if err != nil {
				log.Error(err, "unable to make reference to the running deployment", "deployment", activeDeploy)
				continue
			}
			tmpactive = append(tmpactive,*deployRef)
			//deploycrd.Status.Active = append(deploycrd.Status.Active, *deployRef)
		}
		deploycrd.Status.Active = tmpactive
	}

	backDeploys,okbackup := deploys[elasticscalev1.BackupState]
	if okbackup{
		tmpbackup := make([]corev1.ObjectReference,0)
		for _,backDeploy := range backDeploys{
			deployRef, err := ref.GetReference(r.Scheme, backDeploy)
			if err != nil {
				log.Error(err, "unable to make reference to the running deployment", "deployment", backDeploy)
				continue
			}
			tmpbackup = append(tmpbackup, *deployRef)
		}
		deploycrd.Status.Back = tmpbackup
	}

	waitDeploys,okwait := deploys[elasticscalev1.WaitingState]
	if okwait{
		tmpwait := make([]corev1.ObjectReference,0)
		for _,waitDeploy := range waitDeploys{
			deployRef, err := ref.GetReference(r.Scheme, waitDeploy)
			if err != nil {
				log.Error(err, "unable to make reference to the running deployment", "deployment", waitDeploy)
				continue
			}
			tmpwait = append(tmpwait,*deployRef)
		}
		deploycrd.Status.Wait = tmpwait
	}
	err = r.Status().Update(ctx, deploycrd)
	if err != nil {
		return
	}
	return err


	//for _, backDeploy := range backDeploys {
	//
	//}

	//for _, waitDeploy := range waitDeploys {
	//
	//	backdeploy.Status.Wait = append(backdeploy.Status.Wait, *deployRef)
	//}

}

func PreferrNodeWeight(replicas int) (score int){
	if replicas > MaxSafeReplicasOnOneHost{
		return 10 + 10*MaxSafeReplicasOnOneHost
	}else if replicas <= 0{
		return 0
	}else {
		return replicas*10+10
	}
}

//func PreferrPodAntiWeight(replicas int)
func (r *BackupDeploymentReconciler)AppendDeployList(input map[int]*appsv1.Deployment,state elasticscalev1.DeployState,deploy *appsv1.Deployment)(id int,err error){
	r.StatusLock.Lock()
	defer r.StatusLock.Unlock()
	_,_,nowstate,id := isDeployStatus(deploy)
	if input == nil{
		//err = errors.New("The status of the deploy does not match the status of the list")
		//return -1,
		input = make(map[int]*appsv1.Deployment)
	}
	if string(state) != nowstate{
		err = errors.New("The status of the deploy does not match the status of the list")
		return -1,err
	}
	input[id] = deploy
	return id,nil
	//delete(input, id)
}
func (r *BackupDeploymentReconciler)DeleteDeployList(input map[int]*appsv1.Deployment,id int,deploy *appsv1.Deployment) (deleted *appsv1.Deployment,err error){
	r.StatusLock.Lock()
	defer r.StatusLock.Unlock()
	if input == nil{
		err = errors.New("The map is nil")
		//return -1,
		return nil, err
	}

	if id <= 0 && deploy == nil{
		err = errors.New("Do not specify the deploy to delete")
		return nil,err
	}
	id0 := id
	if id <= 0 && deploy != nil{
		_,_,_,id0 = isDeployStatus(deploy)
	}

	if _,ok := input[id0];!ok{
		err = errors.New("This deploy do not in the map")
		return nil, err
	}
	deleted = input[id0]
	delete(input,id0)
	return deleted, nil
}

func (r *BackupDeploymentReconciler)UpdateDeployList(input map[int]*appsv1.Deployment,id int,deploy *appsv1.Deployment) (err error){
	r.StatusLock.Lock()
	defer r.StatusLock.Unlock()
	if input == nil{
		//err = errors.New("The status of the deploy does not match the status of the list")
		//return -1,
		input = make(map[int]*appsv1.Deployment)
	}
	input[id] = deploy
	return nil

}

func (r *BackupDeploymentReconciler) DispatchQPSToPods(ctx context.Context,log logr.Logger,deploycrd *elasticscalev1.BackupDeployment,waitDeploys map[int]*appsv1.Deployment,activeDeploys map[int]*appsv1.Deployment,qpslabels map[string]string)(err error){
	//deploycrd.Status.Status = string(elasticscalev1.Keep)
	patchData := map[string]interface{}{
		"metadata": map[string]interface{}{
			"annotations": map[string]interface{}{
				DeployStateAnnotation: string(elasticscalev1.ActiveState),
			},
		},
	}
	for idx, deploy := range waitDeploys {
		annotationBytes, _ := json.Marshal(patchData)
		_, err = r.AppsV1().Deployments(deploy.Namespace).Patch(deploy.Name, types.StrategicMergePatchType, annotationBytes)
		if err != nil {
			log.Error(err, "unable to patch annotation")
			return err
		}
		//activeDeploys[idx] = deploy
		//acitveidx
		_, err = r.AppendDeployList(activeDeploys,elasticscalev1.ActiveState,deploy)
		if err != nil {
			return err
		}
		//list
		_, err = r.DeleteDeployList(waitDeploys,idx,nil)
		if err != nil {
			return err
		}
		//delete(waitDeploys, idx)
		options := metav1.ListOptions{
			//elasticscalev1.DeploymentName
			//deployIDKey
			LabelSelector: strings.Join([]string{elasticscalev1.DeploymentName, "=", deploy.Name}, ""),
		}
		podList, err2 := r.CoreV1().Pods(deploy.Namespace).List(options)
		err = err2
		if err != nil {
			//ctrl.Result{},
			log.Error(err, "unable to find pods from deployment: "+deploy.Name)
			return err
		}
		if podList != nil{
			for _, pods := range podList.Items {
				patchData := map[string]interface{}{
					"metadata": map[string]interface{}{
						"labels": qpslabels,
					},
				}
				loadData, _ := json.Marshal(patchData)
				_, err = r.CoreV1().Pods(pods.Namespace).Patch(pods.Name, types.StrategicMergePatchType, loadData)
				//ctrl.Result{},
				if err != nil {
					log.Error(err, "unable to patch qpslabel")
					return err
				}
			}
		}
	}
	err = r.UpdateBackupStatus(ctx, deploycrd, string(elasticscalev1.Keep))
	if err != nil {
		return err
	}
	return nil
}

//*backdeploy.Spec.RunningReplicas

func (r *BackupDeploymentReconciler)ReadRunningReplicas(deploycrd *elasticscalev1.BackupDeployment)(int){
	r.StatusLock.RLock()
	defer r.StatusLock.RUnlock()
	if deploycrd.Spec.RunningReplicas == nil{
		return 0
	}
	return int(*deploycrd.Spec.RunningReplicas)
}

func (r *BackupDeploymentReconciler) ReadDeployReplicas(deploy *appsv1.Deployment)(int){
	r.StatusLock.RLock()
	defer r.StatusLock.RUnlock()
	return int(deploy.Status.Replicas)
}

func (r *BackupDeploymentReconciler)ReadBackReplicas(deploycrd *elasticscalev1.BackupDeployment)(int){
	r.StatusLock.RLock()
	defer r.StatusLock.RUnlock()
	if deploycrd.Spec.BackupReplicas == nil{
		return 0
	}
	return int(*deploycrd.Spec.BackupReplicas)
}


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
	// update the watch things
	if err := mgr.GetFieldIndexer().IndexField(&appsv1.Deployment{}, deployOwnerKey, extractValue); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&elasticscalev1.BackupDeployment{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}


