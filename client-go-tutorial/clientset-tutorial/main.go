package main

/*前文学习了最基础的客户端Restclient，尽管咱们实战的需求很简单（获取指定namespace下所有pod的信息），
但还是写了不少代码，各种设置太麻烦，例如api的path、Group、Version、返回的数据结构、编解码工具

如果业务代码中，需要操作kubernetes资源的代码都写成restclient-tutorial的样子，
是难以忍受的，应该会做一些封装以简化代码，
不过client-go已经给出了简化版客户端，就省去了自己动手的麻烦，也就是本文的主题：Clientset

源码速读，快速搞清楚Clientset到底是啥，然后确认需求，最后快速编码和验证
*/

/*
源码速读:(k8s.io/client-go/kubernetes/clientset.go)
之所以是速读而非精读，是因为Clientset内容简单容易理解，快速掌握其原理即可用于实战；
Clientset源码阅读的切入点就是其名字中的set，这是个集合，里面有很多东西，看一下Clientset数据结构的源码（限于篇幅只展示了一部分）：
*/

/*
// Clientset contains the clients for groups. Each group has exactly one
// version included in a Clientset.
type Clientset struct {
	*discovery.DiscoveryClient
	admissionregistrationV1      *admissionregistrationv1.AdmissionregistrationV1Client
	admissionregistrationV1beta1 *admissionregistrationv1beta1.AdmissionregistrationV1beta1Client
	appsV1                       *appsv1.AppsV1Client
	appsV1beta1                  *appsv1beta1.AppsV1beta1Client
	appsV1beta2                  *appsv1beta2.AppsV1beta2Client
	auditregistrationV1alpha1    *auditregistrationv1alpha1.AuditregistrationV1alpha1Client
	authenticationV1             *authenticationv1.AuthenticationV1Client
	authenticationV1beta1        *authenticationv1beta1.AuthenticationV1beta1Client
	authorizationV1              *authorizationv1.AuthorizationV1Client
	authorizationV1beta1         *authorizationv1beta1.AuthorizationV1beta1Client
	autoscalingV1                *autoscalingv1.AutoscalingV1Client
	autoscalingV2beta1           *autoscalingv2beta1.AutoscalingV2beta1Client
	autoscalingV2beta2           *autoscalingv2beta2.AutoscalingV2beta2Client
	batchV1                      *batchv1.BatchV1Client
	batchV1beta1                 *batchv1beta1.BatchV1beta1Client
	batchV2alpha1                *batchv2alpha1.BatchV2alpha1Client
	certificatesV1beta1          *certificatesv1beta1.CertificatesV1beta1Client
	coordinationV1beta1          *coordinationv1beta1.CoordinationV1beta1Client
	coordinationV1               *coordinationv1.CoordinationV1Client
	coreV1                       *corev1.CoreV1Client
	discoveryV1alpha1            *discoveryv1alpha1.DiscoveryV1alpha1Client
	discoveryV1beta1             *discoveryv1beta1.DiscoveryV1beta1Client
	eventsV1beta1                *eventsv1beta1.EventsV1beta1Client
	extensionsV1beta1            *extensionsv1beta1.ExtensionsV1beta1Client
	flowcontrolV1alpha1          *flowcontrolv1alpha1.FlowcontrolV1alpha1Client
	networkingV1                 *networkingv1.NetworkingV1Client
	networkingV1beta1            *networkingv1beta1.NetworkingV1beta1Client
	nodeV1alpha1                 *nodev1alpha1.NodeV1alpha1Client
	nodeV1beta1                  *nodev1beta1.NodeV1beta1Client
	policyV1beta1                *policyv1beta1.PolicyV1beta1Client
	rbacV1                       *rbacv1.RbacV1Client
	rbacV1beta1                  *rbacv1beta1.RbacV1beta1Client
	rbacV1alpha1                 *rbacv1alpha1.RbacV1alpha1Client
	schedulingV1alpha1           *schedulingv1alpha1.SchedulingV1alpha1Client
	schedulingV1beta1            *schedulingv1beta1.SchedulingV1beta1Client
	schedulingV1                 *schedulingv1.SchedulingV1Client
	settingsV1alpha1             *settingsv1alpha1.SettingsV1alpha1Client
	storageV1beta1               *storagev1beta1.StorageV1beta1Client
	storageV1                    *storagev1.StorageV1Client
	storageV1alpha1              *storagev1alpha1.StorageV1alpha1Client
}
//kubernetes的Group和Version的每个组合，都对应Clientset数据结构的一个字段
//Clientset是所有Group和Version组合对象的集合，
不过Group和Version组合对象到底是啥呢？以appsV1字段为例，
去看看其类型appsv1.AppsV1Client，
AppsV1Client只有一字段，就是restClient，所以RESTClient是Clientset的基础，这话没毛病，
另外注意Deployments方法，返回的是DeploymentInterface接口实现：
*/

//(see: k8s.io/client-go/kubernetes/typed/ for diverse sub client 
//(e.g., appsV1 in: k8s.io/client-go/kubernetes/typed/apps/v1/apps_client.go))
/*
// AppsV1Client is used to interact with features provided by the apps group.
type AppsV1Client struct {
	restClient rest.Interface
}

func (c *AppsV1Client) ControllerRevisions(namespace string) ControllerRevisionInterface {
	return newControllerRevisions(c, namespace)
}

func (c *AppsV1Client) DaemonSets(namespace string) DaemonSetInterface {
	return newDaemonSets(c, namespace)
}

func (c *AppsV1Client) Deployments(namespace string) DeploymentInterface {
	return newDeployments(c, namespace)
}

func (c *AppsV1Client) ReplicaSets(namespace string) ReplicaSetInterface {
	return newReplicaSets(c, namespace)
}

func (c *AppsV1Client) StatefulSets(namespace string) StatefulSetInterface {
	return newStatefulSets(c, namespace)
}

// NewForConfig creates a new AppsV1Client for the given config.
func NewForConfig(c *rest.Config) (*AppsV1Client, error) {
	config := *c
	if err := setConfigDefaults(&config); err != nil {
		return nil, err
	}
	client, err := rest.RESTClientFor(&config)
	if err != nil {
		return nil, err
	}
	return &AppsV1Client{client}, nil
}

// NewForConfigOrDie creates a new AppsV1Client for the given config and
// panics if there is an error in the config.
func NewForConfigOrDie(c *rest.Config) *AppsV1Client {
	client, err := NewForConfig(c)
	if err != nil {
		panic(err)
	}
	return client
}

// New creates a new AppsV1Client for the given RESTClient.
func New(c rest.Interface) *AppsV1Client {
	return &AppsV1Client{c}
}

func setConfigDefaults(config *rest.Config) error {
	gv := v1.SchemeGroupVersion
	config.GroupVersion = &gv
	config.APIPath = "/apis"
	config.NegotiatedSerializer = scheme.Codecs.WithoutConversion()

	if config.UserAgent == "" {
		config.UserAgent = rest.DefaultKubernetesUserAgent()
	}

	return nil
}

// RESTClient returns a RESTClient that is used to communicate
// with API server by this client implementation.
func (c *AppsV1Client) RESTClient() rest.Interface {
	if c == nil {
		return nil
	}
	return c.restClient
}

*/

/*
去看DeploymentInterface，打开deployment.go文件后真相大白，接口定义的和实现一目了然：
(see: k8s.io/client-go/kubernetes/typed/apps/v1/deployment.go)
type DeploymentsGetter interface {
	Deployments(namespace string) DeploymentInterface
}

// DeploymentInterface has methods to work with Deployment resources.
type DeploymentInterface interface {
	Create(ctx context.Context, deployment *v1.Deployment, opts metav1.CreateOptions) (*v1.Deployment, error)
	Update(ctx context.Context, deployment *v1.Deployment, opts metav1.UpdateOptions) (*v1.Deployment, error)
	UpdateStatus(ctx context.Context, deployment *v1.Deployment, opts metav1.UpdateOptions) (*v1.Deployment, error)
	Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error
	DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOpts metav1.ListOptions) error
	Get(ctx context.Context, name string, opts metav1.GetOptions) (*v1.Deployment, error)
	List(ctx context.Context, opts metav1.ListOptions) (*v1.DeploymentList, error)
	Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error)
	Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (result *v1.Deployment, err error)
	GetScale(ctx context.Context, deploymentName string, options metav1.GetOptions) (*autoscalingv1.Scale, error)
	UpdateScale(ctx context.Context, deploymentName string, scale *autoscalingv1.Scale, opts metav1.UpdateOptions) (*autoscalingv1.Scale, error)

	DeploymentExpansion
}

// deployments implements DeploymentInterface
type deployments struct {
	client rest.Interface
	ns     string
}

// newDeployments returns a Deployments
func newDeployments(c *AppsV1Client, namespace string) *deployments {
	return &deployments{
		client: c.RESTClient(),
		ns:     namespace,
	}
}
*/

/*
挑一个接口实现的代码看看，就选新建deployment的方法吧，如下，和我们使用RESTClient编码差不多：
// Create takes the representation of a deployment and creates it.  Returns the server's representation of the deployment, and an error, if there is any.
func (c *deployments) Create(ctx context.Context, deployment *v1.Deployment, opts metav1.CreateOptions) (result *v1.Deployment, err error) {
	result = &v1.Deployment{}
	err = c.client.Post().
		Namespace(c.ns).
		Resource("deployments").
		VersionedParams(&opts, scheme.ParameterCodec).
		Body(deployment).
		Do(ctx).
		Into(result)
	return
}
*/

/*
至此，对Clientset应该心里有数了：
其实就是把我们使用RESTClient操作资源的代码按照Group和Version分类再封装而已，
这不像技术活，更像体力活—所以，这种体力活是谁做的呢？
源码中已经注明这些代码是工具client-gen自动生成的：
// Code generated by client-gen. DO NOT EDIT.
*/

/*
本次编码实战的需求如下：
1.写一段代码，检查用户输入的operate参数，该参数默认是create，也可以接受clean；
2.如果operate参数等于create，就执行以下操作：
新建名为test-clientset的namespace
新建一个deployment，namespace为test-clientset，镜像用tomcat，副本数为2
新建一个service，namespace为test-clientset，类型是NodePort
3.如果operate参数等于clean，就删除create操作中创建的service、deployment、namespace等资源：
以上需求使用Clientset客户端实现，完成后咱们用浏览器访问来验证tomcat是否正常；
*/

import (
	"context"
	"flag"
	"fmt"
	appsv1 "k8s.io/api/apps/v1"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"k8s.io/utils/pointer"
	"path/filepath"
)

const (
	NAMESPACE = "test-clientset"
	DEPLOYMENT_NAME = "client-test-deployment"
	SERVICE_NAME = "client-test-service"
)

func main() {
	var kubeconfig *string
	// home是home目录，如果能取得home目录的值，就可以用来做默认值
	if home := homedir.HomeDir();home != ""{
		// 如果输入了kubeconfig参数，该参数的值就是kubeconfig文件的绝对路径，
		// 如果没有输入kubeconfig参数，就用默认路径~/.kube/config
		kubeconfig = flag.String("kubeconfig",filepath.Join(home,".kube","config"),"(optional) absolute path to the kubeconfig file")
	}else {
		// 如果取不到当前用户的家目录，就没办法设置kubeconfig的默认目录了，只能从入参中取
		kubeconfig = flag.String("kubeconfig","","absolute path to the kubeconfig file")
	}
	// 获取用户输入的操作类型，默认是create，还可以输入clean，用于清理所有资源
	operate := flag.String("operate", "create", "operate type : create or clean")

	flag.Parse()
	config,err := clientcmd.BuildConfigFromFlags("",*kubeconfig)

	// kubeconfig加载失败就直接退出了
	if err != nil {
		panic(err.Error())
	}

	clientset,err := kubernetes.NewForConfig(config)
	if err!= nil {
		panic(err.Error())
	}

	fmt.Printf("operation is %v\n", *operate)
	// 如果要执行清理操作
	if "clean"==*operate {
		clean(clientset)
	} else {
		// 创建namespace
		createNamespace(clientset)

		// 创建deployment
		createDeployment(clientset)

		// 创建service
		createService(clientset)
	}

}

func clean(clientset *kubernetes.Clientset)  {
	//emptyDeleteOptions := metav1.DeleteOptions{}
	//kubernetes 删除资源对象的策略
	//具体源码在$GOPATH/src/k8s.io/kubernetes/vendor/k8s.io/apimachinery/pkg/apis/meta/v1/types.go文件
	/*
	type DeletionPropagation string

const (
   // Orphans the dependents.
   DeletePropagationOrphan DeletionPropagation = "Orphan"
   // Deletes the object from the key-value store, the garbage collector will
   // delete the dependents in the background.
   DeletePropagationBackground DeletionPropagation = "Background"
   // The object exists in the key-value store until the garbage collector
   // deletes all the dependents whose ownerReference.blockOwnerDeletion=true
   // from the key-value store.  API sever will put the "foregroundDeletion"
   // finalizer on the object, and sets its deletionTimestamp.  This policy is
   // cascading, i.e., the dependents will be deleted with Foreground.
   DeletePropagationForeground DeletionPropagation = "Foreground"
)

	// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// DeleteOptions may be provided when deleting an API object.
type DeleteOptions struct {
   TypeMeta `json:",inline"`

   // The duration in seconds before the object should be deleted. Value must be non-negative integer.
   // The value zero indicates delete immediately. If this value is nil, the default grace period for the
   // specified type will be used.
   // Defaults to a per object value if not specified. zero means delete immediately.
   // +optional
   GracePeriodSeconds *int64 `json:"gracePeriodSeconds,omitempty" protobuf:"varint,1,opt,name=gracePeriodSeconds"`

   // Must be fulfilled before a deletion is carried out. If not possible, a 409 Conflict status will be
   // returned.
   // +optional
   Preconditions *Preconditions `json:"preconditions,omitempty" protobuf:"bytes,2,opt,name=preconditions"`

   // Deprecated: please use the PropagationPolicy, this field will be deprecated in 1.7.
   // Should the dependent objects be orphaned. If true/false, the "orphan"
   // finalizer will be added to/removed from the object's finalizers list.
   // Either this field or PropagationPolicy may be set, but not both.
   // +optional
	OrphanDependents *bool `json:"orphanDependents,omitempty" protobuf:"varint,3,opt,name=orphanDependents"`

   // Whether and how garbage collection will be performed.
   // Either this field or OrphanDependents may be set, but not both.
   // The default policy is decided by the existing finalizer set in the
   // metadata.finalizers and the resource-specific default policy.
   // Acceptable values are: 'Orphan' - orphan the dependents; 'Background' -
   // allow the garbage collector to delete the dependents in the background;
   // 'Foreground' - a cascading policy that deletes all dependents in the
   // foreground.
   // +optional
   PropagationPolicy *DeletionPropagation `json:"propagationPolicy,omitempty" protobuf:"varint,4,opt,name=propagationPolicy"`

	*/
	/*
	从以上源码内容可以看出，有三种删除策略
Foreground:删除控制器之前，所管理的资源对象必须先删除
Background：删除控制器后，所管理的资源对象由GC删除
Orphan：只删除控制器对象,不删除控制器所管理的资源对象
	*/

	// 删除service
	service_delete_policy := metav1.DeletePropagationBackground
	if err := clientset.CoreV1().Services(NAMESPACE).Delete(context.TODO(),SERVICE_NAME,metav1.DeleteOptions{
		PropagationPolicy: &service_delete_policy,
	});err != nil{
		panic(err.Error())
	}
	deploy_delete_policy := metav1.DeletePropagationForeground
	if err := clientset.AppsV1().Deployments(NAMESPACE).Delete(context.TODO(),DEPLOYMENT_NAME,metav1.DeleteOptions{
		PropagationPolicy: &deploy_delete_policy,
	});err != nil{
		panic(err.Error())
	}
	name_delete_policy := metav1.DeletePropagationForeground
	
	if err := clientset.CoreV1().Namespaces().Delete(context.TODO(),NAMESPACE,metav1.DeleteOptions{
		PropagationPolicy: &name_delete_policy,
	});err != nil{
		panic(err.Error())
	}
//	deletePolicy := metav1.DeletePropagationForeground
	//	if err := depolymentClient.Delete(context.TODO(),"demo-deployment",metav1.DeleteOptions{
	//		PropagationPolicy: &deletePolicy,
	//	});err != nil{
	//		panic(err)
	//	}
	//	fmt.Println("Deleted deployment.")
	//metav1.DeletePropagationBackground
}

// 新建namespace
func createNamespace(clientset *kubernetes.Clientset) {
	namespaceClient := clientset.CoreV1().Namespaces()
	
	namespace := &apiv1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: NAMESPACE,
		},
	}
	
	result,err := namespaceClient.Create(context.TODO(),namespace,metav1.CreateOptions{})
	
	if err != nil{
		panic(err.Error())
	}
	
	fmt.Printf("Create namespace %s \n",result.GetName())
	
}

// 新建service
func createService(clientset *kubernetes.Clientset) {
	// 得到service的客户端
	serviceClient := clientset.CoreV1().Services(NAMESPACE)
	// 实例化一个数据结构
	service := &apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: SERVICE_NAME,
		},
		Spec: apiv1.ServiceSpec{
			Ports: []apiv1.ServicePort{
				{
					Name: "http",
					Port: 8080,
					NodePort: 30080,
				},
		},
		Selector: map[string]string{
				"app": "tomcat",
		},
		Type: apiv1.ServiceTypeNodePort,
	},
	}
	result,err := serviceClient.Create(context.TODO(),service,metav1.CreateOptions{})
	if err!=nil {
		panic(err.Error())
	}

	fmt.Printf("Create service %s \n", result.GetName())
}

// 新建deployment
func createDeployment(clientset *kubernetes.Clientset) {
	// 得到deployment的客户端
	deploymentClient := clientset.AppsV1().Deployments(NAMESPACE)
	// 实例化一个数据结构
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: DEPLOYMENT_NAME,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: pointer.Int32Ptr(2),
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app" : "tomcat",
				},
			},
			
			Template: apiv1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "tomcat",
					},
				},
				Spec: apiv1.PodSpec{
					Containers: []apiv1.Container{
						{
							Name: "tomcat",
							Image: "tomcat:8.0.18-jre8",
							ImagePullPolicy: "IfNotPresent",
							Ports: []apiv1.ContainerPort{
								{
									Name: "http",
									Protocol: apiv1.ProtocolTCP,
									ContainerPort: 8080,
									},
							},
						},
					},
				},
			},
		},
	}
	result, err := deploymentClient.Create(context.TODO(), deployment, metav1.CreateOptions{})

	if err!=nil {
		panic(err.Error())
	}
	fmt.Printf("Create deployment %s \n", result.GetName())
}
