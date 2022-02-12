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
//还记得最开始我们说要回到 main.go 文件么? 让我们看看 main.go 新增了什么。

package main

import (
	"flag"
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	//比较核心的库： controller-runtime
	ctrl "sigs.k8s.io/controller-runtime"
	//default log package of controller-runtime
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	//首先要注意的是 kubebuilder 已将新 API group 的 package(batchv1）添加到我们的 scheme 中了。
	//这意味着我们可以在 controller 中使用这些对象了。
	batchv1 "kubebuilder-tutorial/api/v1"
	"kubebuilder-tutorial/controllers"
	// +kubebuilder:scaffold:imports
)

//每组 controller 都需要一个 Scheme， Scheme 会提供 Kinds 与 Go types 之间的映射关系(现在你只需要记住这一点)。
//在编写 API 定义时，我们将会进一步讨论 Kinds。
//首先要注意的是 kubebuilder 已将新 API group 的 package(batchv1）添加到我们的 scheme 中了。
//这意味着我们可以在 controller 中使用这些对象了。
//
//如果要使用任何其他 CRD，则必须以相同的方式添加其 scheme。
//诸如 Job 之类的内置类型的 scheme 是由 clientgoscheme 添加的。
var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)

	_ = batchv1.AddToScheme(scheme)
	// +kubebuilder:scaffold:scheme
}

//此时，我们 main.go 的功能相对来说比较简单：
func main() {
	//
	var metricsAddr string
	var enableLeaderElection bool
	//1.为 metrics 绑定一些基本的 flags。
	flag.StringVar(&metricsAddr, "metrics-addr", ":8080", "The address the metric endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "enable-leader-election", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseDevMode(true)))
	//2.实例化一个 manager，用于跟踪我们运行的所有 controllers，
	//并设置 shared caches 和可以连接到 API server 的 k8s clients 实例: ctrl.GetConfigOrDie()
	//并将 Scheme 配置传入 manager: ctrl.Options
	//请注意：下面代码如果指定了 Namespace 字段, controllers
	//仍然可以 watch cluster 级别的资源(例如Node),
	//但是对于 namespace 级别的资源，cache 将仅可以缓存指定 namespace 中的资源。
	//mgr, err = ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
	//        Scheme:             scheme,
	//        Namespace:          namespace,
	//        MetricsBindAddress: metricsAddr,
	//    })
	//上面的示例将 operator 应用的获取资源范围限制在了单个 namespace。
	//在这种情况下，建议将默认的 ClusterRole 和 ClusterRoleBinding
	//分别替换为 Role 和 RoleBinding 来将授权限制于此名称空间。
	//有关更多信息，请参见如何使用 RBAC Authorization。
	//除此之外，你也可以使用 MultiNamespacedCacheBuilder 来监视一组特定的 namespaces：
	//namspaces := []string{"default","kube-system"}
	//mgr0,err0 := ctrl.NewManager(ctrl.GetConfigOrDie(),ctrl.Options{
	//	Scheme: scheme,
	//	NewCache: cache.MultiNamespacedCacheBuilder(namspaces),
	//	MetricsBindAddress: metricsAddr,
	//})
	//有关更多信息，请参见 MultiNamespacedCacheBuilder
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:             scheme,
		MetricsBindAddress: metricsAddr,
		Port:               9443,
		LeaderElection:     enableLeaderElection,
		LeaderElectionID:   "5bc24d40.tutorial.kubebuilder.io",
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}
	//虽然目前我们还没有什么可以运行，但是请记住 +kubebuilder:scaffold:builder 注释的位置
	//-- 很快那里就会变得有趣。
	//另一处更改是 kubebuilder 添加了一个块代码，
	//该代码调用了我们的 CronJob controller 的 SetupWithManager 方法。
	if err = (&controllers.CronJobReconciler{
		Client: mgr.GetClient(),
		Log:    ctrl.Log.WithName("controllers").WithName("CronJob"),
		Scheme: mgr.GetScheme(),
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to create controller", "controller", "CronJob")
		os.Exit(1)
	}

	//
	//如不需要启动 webhook，只需要设置 ENABLE_WEBHOOKS = false 即可。
	//if os.Getenv("ENABLE_WEBHOOKS") != "false"{
	//	if err = (&batchv1.CronJob{}).SetupWebhookWithManager(mgr); err != nil{
	//
	//	}
	//}
	//我们还会为我们的 type 设置 webhook。 我们只需要将它们添加到 manager 中就行。
	//由于我们可能想单独运行 webhook，或者在本地测试 controller 不运行它们，
	//因此我们将其是否启动放在环境变量里面。
	//如不需要启动 webhook，只需要设置 ENABLE_WEBHOOKS = false 即可。

	if os.Getenv("ENABLE_WEBHOOKS") != "false" {
		if err = (&batchv1.CronJob{}).SetupWebhookWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create webhook", "webhook", "CronJob")
			os.Exit(1)
		}
	}

	// +kubebuilder:scaffold:builder

	setupLog.Info("starting manager")
	//3.运行我们的 manager, 而 manager 又运行所有的 controllers 和 webhook。
	//manager 会一直处于运行状态，直到收到正常关闭信号为止。(<- chan struct{})
	//这样，当我们的 operator 运行在 Kubernetes 上时，我们可以通过优雅的方式终止这个 Pod。
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

//在开始编写 API 之前，让我们讨论一些关键术语
//
//当描述 Kubernetes 中的 API 时，我们经常用到的四个术语是：groups、 versions、kinds 和 resources

//Groups 和 Versions
//Kubernetes 中的 API Group 只是相关功能的集合,
//每个 group 包含一个或者多个 versions, 这样的关系可以让我们随着时间的推移，
//通过创建不同的 versions 来更改 API 的工作方式。

//Kinds 和 Resources
// Kinds
//每个 API group-version 包含一个或者多个 API 类型, 我们称之为 Kinds.
//虽然同类型 Kind 在不同的 version 之间的表现形式可能不同，
//但是同类型 Kind 必须能够存储其他 Kind 的全部数据，
//也就是说同类型 Kind 之间必须是互相兼容的(我们可以把数据存到 fields 或者 annotations)，
//这样当你使用老版本的 API group-version 时不会造成丢失或损坏,
//有关更多信息，请参见Kubernetes API指南。

//ps: 自己的理解
//通常部署到 k8s 集群中的 YAML 文件前几行格式如下, 其中extensions/v1beta1 就是 API group-version、extensions 是 Group、 v1beta1 是 version、 Deployment 是 Kind。
//
//apiVersion: extensions/v1beta1
//kind: Deployment
//metadata:

//Resources
//你应该听别人提到过 Resources， Resource 是 Kind 在 API 中的标识，
//通常情况下 Kind 和 Resource 是一一对应的, 但是有时候相同的 Kind 可能对应多个 Resources,
//比如 Scale Kind 可能对应很多 Resources：deployments/scale 或者 replicasets/scale,
//但是在 CRD 中，每个 Kind 只会对应一种 Resource。

//请注意，Resource 始终是小写形式，并且通常情况下是 Kind 的小写形式。 具体对应关系可以查看 resource type。

//ps: 自己的理解
//当我们使用 kubectl 操作 API 时，操作的就是 Resource，比如 kubectl get pods,
//这里的 pods 就是指 Resource。
//而我们在编写 YAML 文件时，会编写类似 Kind: Pod 这样的内容，这里 Pod 就是 Kind

//那么以上类型在框架中是如何定义的呢?
//当我们在一个特定的 group-version中使用 Kind 时，我们称它为 GroupVersionKind, 简称 GVK,
//同样的 resources 我们称它为 GVR。 稍后我们将看到每个 GVK
//都对应一个 root Go type (比如：Deployment
//就关联着 K8s 源码里面 k8s.io/api/apps/v1 package 中的 Deployment struct)
//现在我们已经熟悉了一些关键术语，那么我们可以开始创建我们的 API 了！

//额, 但是 Scheme 是什么东东?
//Scheme 提供了 GVK 与对应 Go types(struct) 之间的映射（请不要和 godocs 中的 Scheme 混淆）
//
//也就是说给定 Go type 就可以获取到它的 GVK，给定 GVK 可以获取到它的 Go type
//
//例如，让我们假设 tutorial.kubebuilder.io/api/v1/cronjob_types.go 中的 CronJob 结构体
//在 batch.tutorial.kubebuilder.io/v1 API group 中
//（也就是说，假设这个 API group 有一种 Kind：CronJob，(GVK)
//并且已经注册到 Api Server 中）

//那么我们可以从 Api Server 获取到数据并反序列化至 &CronJob{}中，那么结果会是如下格式:
//{
//    "kind": "CronJob",
//    "apiVersion": "batch.tutorial.kubebuilder.io/v1",
//    ...
//}

// registered logical GVK (url-like) mapping to the base struct of go type
//ps：这里翻译的不好，请继续向后看，慢慢就理解了 ：）
