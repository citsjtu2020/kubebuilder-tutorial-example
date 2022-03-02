package main

import (
	"flag"
	"fmt"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"path/filepath"
)

/*
关于DiscoveryClient
最后一种客户端：DiscoveryClient，之前学习的Clientset和dynamicClient都是面向资源对象的（例如创建deployment实例、查看pod实例），
而DiscoveryClient则不同，它聚焦的是资源，
例如查看当前kubernetes有哪些Group、Version、Resource，
下面是DiscoveryClient数据结构的字段和关联方法，
再次看到了熟悉的restClient字段，
还有一众方法皆是与Group、Version、Resource有关：
*/

/*
// DiscoveryClient implements the functions that discover server-supported API groups,
// versions and resources.
type DiscoveryClient struct {
	restClient restclient.Interface

	LegacyPrefix string
}
*/

/*
// ServerGroups returns the supported groups, with information like supported versions and the
// preferred version.
func (d *DiscoveryClient) ServerGroups() (apiGroupList *metav1.APIGroupList, err error) {
	// Get the groupVersions exposed at /api
	v := &metav1.APIVersions{}
	err = d.restClient.Get().AbsPath(d.LegacyPrefix).Do(context.TODO()).Into(v)
	apiGroup := metav1.APIGroup{}
	if err == nil && len(v.Versions) != 0 {
		apiGroup = apiVersionsToAPIGroup(v)
	}
	if err != nil && !errors.IsNotFound(err) && !errors.IsForbidden(err) {
		return nil, err
	}

	// Get the groupVersions exposed at /apis
	apiGroupList = &metav1.APIGroupList{}
	err = d.restClient.Get().AbsPath("/apis").Do(context.TODO()).Into(apiGroupList)
	if err != nil && !errors.IsNotFound(err) && !errors.IsForbidden(err) {
		return nil, err
	}
	// to be compatible with a v1.0 server, if it's a 403 or 404, ignore and return whatever we got from /api
	if err != nil && (errors.IsNotFound(err) || errors.IsForbidden(err)) {
		apiGroupList = &metav1.APIGroupList{}
	}

	// prepend the group retrieved from /api to the list if not empty
	if len(v.Versions) != 0 {
		apiGroupList.Groups = append([]metav1.APIGroup{apiGroup}, apiGroupList.Groups...)
	}
	return apiGroupList, nil
}

// ServerResourcesForGroupVersion returns the supported resources for a group and version.
func (d *DiscoveryClient) ServerResourcesForGroupVersion(groupVersion string) (resources *metav1.APIResourceList, err error) {
	url := url.URL{}
	if len(groupVersion) == 0 {
		return nil, fmt.Errorf("groupVersion shouldn't be empty")
	}
	if len(d.LegacyPrefix) > 0 && groupVersion == "v1" {
		url.Path = d.LegacyPrefix + "/" + groupVersion
	} else {
		url.Path = "/apis/" + groupVersion
	}
	resources = &metav1.APIResourceList{
		GroupVersion: groupVersion,
	}
	err = d.restClient.Get().AbsPath(url.String()).Do(context.TODO()).Into(resources)
	if err != nil {
		// ignore 403 or 404 error to be compatible with an v1.0 server.
		if groupVersion == "v1" && (errors.IsNotFound(err) || errors.IsForbidden(err)) {
			return resources, nil
		}
		return nil, err
	}
	return resources, nil
}

// ServerResources returns the supported resources for all groups and versions.
// Deprecated: use ServerGroupsAndResources instead.
func (d *DiscoveryClient) ServerResources() ([]*metav1.APIResourceList, error) {
	_, rs, err := d.ServerGroupsAndResources()
	return rs, err
}

// ServerGroupsAndResources returns the supported resources for all groups and versions.
func (d *DiscoveryClient) ServerGroupsAndResources() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
	return withRetries(defaultRetries, func() ([]*metav1.APIGroup, []*metav1.APIResourceList, error) {
		return ServerGroupsAndResources(d)
	})
}
*/


/*
DiscoveryClient数据结构有两个字段：restClient和LegacyPrefix，这个LegacyPrefix是啥呢？
去看看新建DiscoveryClient实例的方法，原来是个固定字符串/api，看起来像是url中的一部分：

/ NewDiscoveryClientForConfig creates a new DiscoveryClient for the given config. This client
// can be used to discover supported resources in the API server.
func NewDiscoveryClientForConfig(c *restclient.Config) (*DiscoveryClient, error) {
	config := *c
	if err := setDiscoveryDefaults(&config); err != nil {
		return nil, err
	}
	client, err := restclient.UnversionedRESTClientFor(&config)
	return &DiscoveryClient{restClient: client, LegacyPrefix: "/api"}, err
}
*/

/*
挑一个DiscoveryClient的关联方法看看，果然，LegacyPrefix就是url中的一部分：
// ServerGroups returns the supported groups, with information like supported versions and the
// preferred version.
func (d *DiscoveryClient) ServerGroups() (apiGroupList *metav1.APIGroupList, err error) {
	// Get the groupVersions exposed at /api
	v := &metav1.APIVersions{}
	//LegacyPrefix就是url中的一部分：
	err = d.restClient.Get().AbsPath(d.LegacyPrefix).Do(context.TODO()).Into(v)
	apiGroup := metav1.APIGroup{}
	if err == nil && len(v.Versions) != 0 {
		apiGroup = apiVersionsToAPIGroup(v)
	}
	if err != nil && !errors.IsNotFound(err) && !errors.IsForbidden(err) {
		return nil, err
	}

	// Get the groupVersions exposed at /apis
	apiGroupList = &metav1.APIGroupList{}
	err = d.restClient.Get().AbsPath("/apis").Do(context.TODO()).Into(apiGroupList)
	if err != nil && !errors.IsNotFound(err) && !errors.IsForbidden(err) {
		return nil, err
	}
	// to be compatible with a v1.0 server, if it's a 403 or 404, ignore and return whatever we got from /api
	if err != nil && (errors.IsNotFound(err) || errors.IsForbidden(err)) {
		apiGroupList = &metav1.APIGroupList{}
	}

	// prepend the group retrieved from /api to the list if not empty
	if len(v.Versions) != 0 {
		apiGroupList.Groups = append([]metav1.APIGroup{apiGroup}, apiGroupList.Groups...)
	}
	return apiGroupList, nil
}
*/

func main() {
	//本次实战的需求很简单：
	//从kubernetes查询所有的Group、Version、Resource信息，在控制台打印出来；
	/*
	要重点关注的是ServerGroupsAndResources方法的第二个返回值，
	它的数据结构中有切片(mulit group-versions)，
	切片的每个元素里面又有切片(multi kinds/resources)，这才是每个资源的信息：
	*/
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
	//operate := flag.String("operate", "create", "operate type : create or clean")

	flag.Parse()

	config,err := clientcmd.BuildConfigFromFlags("",*kubeconfig)

	// kubeconfig加载失败就直接退出了
	if err != nil {
		panic(err.Error())
	}
	// 新建discoveryClient实例
	discoveryClient,err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	// 获取所有分组和资源数据
	APIGroup,APIResourceListSlice,err := discoveryClient.ServerGroupsAndResources()

	if err != nil {
		panic(err.Error())
	}

	// 先看Group信息
	fmt.Printf("APIGroup :\n\n %v\n\n\n\n",APIGroup)

	// APIResourceListSlice是个切片，里面的每个元素代表一个GroupVersion及其资源
	for _,singleAPIResourceList := range APIResourceListSlice{
		// GroupVersion是个字符串，例如"apps/v1"
		groupVersionStr := singleAPIResourceList.GroupVersion

		// ParseGroupVersion方法将字符串转成数据结构
		gv,err := schema.ParseGroupVersion(groupVersionStr)

		if err != nil {
			panic(err.Error())
		}

		fmt.Println("*****************************************************************")
		fmt.Printf("GV string [%v]\nGV struct [%#v]\nresources :\n\n", groupVersionStr, gv)

		// APIResources字段是个切片，里面是当前GroupVersion下的所有资源
		for _,singleAPIResource := range singleAPIResourceList.APIResources{
			fmt.Printf("Name: %v Kind:%v\n",singleAPIResource.Name,singleAPIResource.Kind)
		}
	}
}

/*
kubectl中如何使用DiscoveryClient
kubectl api-versions命令，大家应该不陌生吧，
可以返回当前kubernetes环境的所有Group+Version的组合

通过查看kubectl源码可见，上述命令的背后就是使用了DiscoveryClient来实现的:
see:
kubernetes-1.17.3/vendor/k8s.io/kubectl/pkg/cmd/apiresources/apiversions.go
or
/kubernetes-1.17.3/staging/src/k8s.io/kubectl/pkg/cmd/apiresources/apiversions.go
// NewCmdAPIVersions creates the `api-versions` command

func NewCmdAPIVersions(f cmdutil.Factory, ioStreams genericclioptions.IOStreams) *cobra.Command {
	o := NewAPIVersionsOptions(ioStreams)
	cmd := &cobra.Command{
		Use:     "api-versions", // kubectl api-version
		Short:   "Print the supported API versions on the server, in the form of \"group/version\"",
		Long:    "Print the supported API versions on the server, in the form of \"group/version\"",
		Example: apiversionsExample,
		Run: func(cmd *cobra.Command, args []string) {
			cmdutil.CheckErr(o.Complete(f, cmd, args))
			cmdutil.CheckErr(o.RunAPIVersions()) //call the RunAPIVersions() function
		},
	}
	return cmd
}


// RunAPIVersions does the work
func (o *APIVersionsOptions) RunAPIVersions() error {
	// Always request fresh data from the server
	o.discoveryClient.Invalidate()
	// call the discoveryClient to get group and versions
	groupList, err := o.discoveryClient.ServerGroups()
	if err != nil {
		return fmt.Errorf("couldn't get available api versions from server: %v", err)
	}
	apiVersions := metav1.ExtractGroupVersions(groupList)
	sort.Strings(apiVersions)
	for _, v := range apiVersions {
		fmt.Fprintln(o.Out, v)
	}
	return nil
}

还有一处没有明确：o.discoveryClient究竟是不是DiscoveryClient呢？
虽然名字很像，但还是瞅一眼才放心，
结果这一瞅有了新发现，如下所示，discoveryClient的数据结构是:
// APIVersionsOptions have the data required for API versions
type APIVersionsOptions struct {
	discoveryClient discovery.CachedDiscoveryInterface

	genericclioptions.IOStreams
}

从名称CachedDiscoveryInterface来看，kubectl对GVR数据是做了本地缓存的，
GVR不经常变化，没必要每次都去API Server拉取，关于缓存的细节请参考：
staging/src/k8s.io/client-go/discovery/cached/disk/cached_discovery.go，
这里就不展开了；
至此，client-go的四种客户端工具实战以及相关源码的浅层次分析就全部完成了.
*/

