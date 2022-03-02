package main

/*
RESTClient简介
RESTClient是client-go最基础的客户端，主要是对HTTP Reqeust进行了封装，对外提供RESTful风格的API，并且提供丰富的API用于各种设置，相比其他几种客户端虽然更复杂，但是也更为灵活；
使用RESTClient对kubernetes的资源进行增删改查的基本步骤如下：
1. 确定要操作的资源类型(例如查找deployment列表)，去官方API文档中找到对于的path、数据结构等信息，后面会用到；
2. 加载配置kubernetes配置文件（和kubectl使用的那种kubeconfig完全相同）；
3. 根据配置文件生成配置对象，并且通过API对配置对象就行设置（例如请求的path、Group、Version、序列化反序列化工具等）
4. 创建RESTClient实例，入参是配置对象；
5. 调用RESTClient实例的方法向kubernetes的API Server发起请求，
编码用fluent风格将各种参数传入(例如指定namespace、资源等)，
如果是查询类请求，还要传入数据结构实例的指针，改数据结构用于接受kubernetes返回的查询结果；

本次实战内容很简单：查询kube-system这个namespace下的所有pod，然后在控制台打印每个pod的几个关键字段；


*/

import (
	"context"
	"flag"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"path/filepath"
)


func main() {
	/*
	1. 查看官方文档，获取编码所需内容
浏览器打开官方API文档，地址：https://v1-19.docs.kubernetes.io/docs/reference/generated/kubernetes-api/v1.19/
	for list operation(function): GET /api/v1/namespaces/{namespace}/pods
	
	return: PodList v1 core
	掌握了请求和响应的详细信息，可以开始编码了
	*/
	
//	2. 加载配置kubernetes配置文件（和kubectl使用的那种kubeconfig完全相同）；
	var kubeconfig *string
	// home是home目录，如果能取得home目录的值，就可以用来做默认值
	if home := homedir.HomeDir();home != ""{
		// 如果输入了kubeconfig参数，该参数的值就是kubeconfig文件的绝对路径，
		// 如果没有输入kubeconfig参数，就用默认路径~/.kube/config
		kubeconfig = flag.String("kubeconfig",filepath.Join(home,".kube","config"),"(optional) absolute path to the kubeconfig file")
	}else {
		// 如果取不到当前用户的home目录，就没办法设置kubeconfig的默认目录了，只能从入参中取
		kubeconfig = flag.String("kubeconfig","","absolute path to the kubeconfig file")
	}
	flag.Parse()
	//3. 根据配置文件生成配置对象，并且通过API对配置对象就行设置
	//（例如请求的path、Group、Version、序列化反序列化工具等）
	
	// 从本机加载kubeconfig配置文件，因此第一个参数为空字符串
	config,err := clientcmd.BuildConfigFromFlags("",*kubeconfig)
	// kubeconfig加载失败就直接退出了
	if err != nil {
		panic(err.Error())
	}
	// 参考path : /api/v1/namespaces/{namespace}/pods
	config.APIPath = "api"
	// pod的group是空字符串 (the core api have the "" group name)
	config.GroupVersion = &corev1.SchemeGroupVersion
	// 指定序列化工具
	config.NegotiatedSerializer = scheme.Codecs

	//4. 创建RESTClient实例，入参是配置对象；
	// 根据配置信息构建restClient实例
	restClient,err := rest.RESTClientFor(config)
	
	if err!=nil {
		panic(err.Error())
	}
	//5. 调用RESTClient实例的方法向kubernetes的API Server发起请求，
	// 保存pod结果的数据结构实例
	result := &corev1.PodList{}
	
	//  指定namespace
	namespace := "kube-system"
	// 设置请求参数，然后发起请求
	// GET请求
	err = restClient.Get().
		//  指定namespace，参考path : /api/v1/namespaces/{namespace}/pods
		Namespace(namespace).
		//// 查找多个pod，参考path : /api/v1/namespaces/{namespace}/pods
		Resource("pods").
		// 指定大小限制和序列化工具
		VersionedParams(&metav1.ListOptions{Limit: 100},scheme.ParameterCodec).
		// 请求
		Do(context.TODO()).
		// 结果存入result
		Into(result)
	
	if err != nil{
		panic(err.Error())
	}
	
	// 表头
	fmt.Printf("namespace\t status\t\t name\n")
	// 每个pod都打印namespace、status.Phase、name三个字段
	for _,p := range result.Items{
		fmt.Printf("%v\t %v\t %v\n",p.Namespace,p.Status.Phase,p.Name)
	}

	//runtime.Scheme{}
	//runtime.Serializer()

}
/*
如何将收到的数据反序列化为PodList对象？
前面的代码比较简单，result是corev1.PodList类型的结构体指针，
restClient收到kubernetes返回的数据后，
如何知道要将数据反序列化成corev1.PodList类型呢（Into方法入参类型为runtime.Object）
i.e., Info(result)

之前的代码中有一行设置了编解码工具：config.NegotiatedSerializer = scheme.Codecs，
展开这个scheme.Codecs，可见设置的时候确定了序列化工具为runtime.Serializer：
var Codecs = serializer.NewCodecFactory(Scheme)

// newCodecFactory is a helper for testing that allows a different metafactory to be specified.
func newCodecFactory(scheme *runtime.Scheme, serializers []serializerType) CodecFactory {
	decoders := make([]runtime.Decoder, 0, len(serializers))
	var accepts []runtime.SerializerInfo
	alreadyAccepted := make(map[string]struct{})

//可见设置的时候确定了序列化工具为runtime.Serializer:
	var legacySerializer runtime.Serializer

	for _, d := range serializers {
		decoders = append(decoders, d.Serializer)
		for _, mediaType := range d.AcceptContentTypes {
			if _, ok := alreadyAccepted[mediaType]; ok {
				continue
			}
			alreadyAccepted[mediaType] = struct{}{}
			info := runtime.SerializerInfo{
				MediaType:        d.ContentType,
				EncodesAsText:    d.EncodesAsText,
				Serializer:       d.Serializer,
				PrettySerializer: d.PrettySerializer,
			}

			mediaType, _, err := mime.ParseMediaType(info.MediaType)
			if err != nil {
				panic(err)
			}
			parts := strings.SplitN(mediaType, "/", 2)
			info.MediaTypeType = parts[0]
			info.MediaTypeSubType = parts[1]

			if d.StreamSerializer != nil {
				info.StreamSerializer = &runtime.StreamSerializerInfo{
					Serializer:    d.StreamSerializer,
					EncodesAsText: d.EncodesAsText,
					Framer:        d.Framer,
				}
			}
			accepts = append(accepts, info)
			if mediaType == runtime.ContentTypeJSON {
				legacySerializer = d.Serializer
			}
		}
	}
	if legacySerializer == nil {
		legacySerializer = serializers[0].Serializer
	}

	return CodecFactory{
		scheme:      scheme,
		serializers: serializers,
		universal:   recognizer.NewDecoder(decoders...),

		accepts: accepts,

		legacySerializer: legacySerializer,
	}
}

//Serializer的typer字段类型是runtime.ObjectTyper，
这里实际上是runtime.Scheme，因此ObjectTyper.ObjectKinds方法，
实际上就是Scheme.ObjectKinds方法，在里面根据s.typeToGVK[t]拿到了GVK，也就是v1.PodList：
// ObjectKinds returns all possible group,version,kind of the go object, true if the
// object is considered unversioned, or an error if it's not a pointer or is unregistered.
func (s *Scheme) ObjectKinds(obj Object) ([]schema.GroupVersionKind, bool, error) {
	// Unstructured objects are always considered to have their declared GVK
	if _, ok := obj.(Unstructured); ok {
		// we require that the GVK be populated in order to recognize the object
		gvk := obj.GetObjectKind().GroupVersionKind()
		if len(gvk.Kind) == 0 {
			return nil, false, NewMissingKindErr("unstructured object has no kind")
		}
		if len(gvk.Version) == 0 {
			return nil, false, NewMissingVersionErr("unstructured object has no version")
		}
		return []schema.GroupVersionKind{gvk}, false, nil
	}
	// get the reflect object of the raw value object
	v, err := conversion.EnforcePtr(obj)
	if err != nil {
		return nil, false, err
	}
	// get the specific type of this object
	t := v.Type()
	
	// get the gvk (group version kind) of the object
	gvks, ok := s.typeToGVK[t]
	if !ok {
		return nil, false, NewNotRegisteredErrForType(s.schemeName, t)
	}
	_, unversionedType := s.unversionedTypes[t]

	return gvks, unversionedType, nil
}

GCK:
type GroupVersionKind struct {
	Group   string
	Version string
	Kind    string
}

//有了这个GVK就确定的返回数据的类型，最终调用caseSensitiveJSONIterator.Unmarshal(data, obj)完成byte数组到对象的反序列化操作：
 can find in k8s.io/apimachinery/pkg/runtime/serializer/json/json.go:

func CaseSensitiveJsonIterator() jsoniter.API {
	config := jsoniter.Config{
		EscapeHTML:             true,
		SortMapKeys:            true,
		ValidateJsonRawMessage: true,
		CaseSensitive:          true,
	}.Froze()
	// Force jsoniter to decode number to interface{} via int64/float64, if possible.
	config.RegisterExtension(&customNumberExtension{})
	return config
}

var caseSensitiveJsonIterator = CaseSensitiveJsonIterator()
func (s *Serializer) Decode(originalData []byte, gvk *schema.GroupVersionKind, into runtime.Object) (runtime.Object, *schema.GroupVersionKind, error) {
	data := originalData
	if s.options.Yaml {
		altered, err := yaml.YAMLToJSON(data)
		if err != nil {
			return nil, nil, err
		}
		data = altered
	}

	actual, err := s.meta.Interpret(data)
	if err != nil {
		return nil, nil, err
	}

	if gvk != nil {
		*actual = gvkWithDefaults(*actual, *gvk)
	}

	if unk, ok := into.(*runtime.Unknown); ok && unk != nil {
		unk.Raw = originalData
		unk.ContentType = runtime.ContentTypeJSON
		unk.GetObjectKind().SetGroupVersionKind(*actual)
		return unk, actual, nil
	}

	if into != nil {
		_, isUnstructured := into.(runtime.Unstructured)
		types, _, err := s.typer.ObjectKinds(into)
		switch {
		case runtime.IsNotRegisteredError(err), isUnstructured:
			//最终调用caseSensitiveJSONIterator.Unmarshal(data, obj)完成byte数组到对象的反序列化操作：
			if err := caseSensitiveJsonIterator.Unmarshal(data, into); err != nil {
				return nil, actual, err
			}
			return into, actual, nil
		case err != nil:
			return nil, actual, err
		default:
			*actual = gvkWithDefaults(*actual, types[0])
		}
	}

	if len(actual.Kind) == 0 {
		return nil, actual, runtime.NewMissingKindErr(string(originalData))
	}
	if len(actual.Version) == 0 {
		return nil, actual, runtime.NewMissingVersionErr(string(originalData))
	}

	// use the target if necessary
	obj, err := runtime.UseOrCreateObject(s.typer, s.creater, *actual, into)
	if err != nil {
		return nil, actual, err
	}
	//最终调用caseSensitiveJSONIterator.Unmarshal(data, obj)完成byte数组到对象的反序列化操作：
	if err := caseSensitiveJsonIterator.Unmarshal(data, obj); err != nil {
		return nil, actual, err
	}

	// If the deserializer is non-strict, return successfully here.
	if !s.options.Strict {
		return obj, actual, nil
	}

	// In strict mode pass the data trough the YAMLToJSONStrict converter.
	// This is done to catch duplicate fields regardless of encoding (JSON or YAML). For JSON data,
	// the output would equal the input, unless there is a parsing error such as duplicate fields.
	// As we know this was successful in the non-strict case, the only error that may be returned here
	// is because of the newly-added strictness. hence we know we can return the typed strictDecoderError
	// the actual error is that the object contains duplicate fields.
	altered, err := yaml.YAMLToJSONStrict(originalData)
	if err != nil {
		return nil, actual, runtime.NewStrictDecodingError(err.Error(), string(originalData))
	}
	// As performance is not an issue for now for the strict deserializer (one has regardless to do
	// the unmarshal twice), we take the sanitized, altered data that is guaranteed to have no duplicated
	// fields, and unmarshal this into a copy of the already-populated obj. Any error that occurs here is
	// due to that a matching field doesn't exist in the object. hence we can return a typed strictDecoderError,
	// the actual error is that the object contains unknown field.
	strictObj := obj.DeepCopyObject()
	if err := strictCaseSensitiveJsonIterator.Unmarshal(altered, strictObj); err != nil {
		return nil, actual, runtime.NewStrictDecodingError(err.Error(), string(originalData))
	}
	// Always return the same object as the non-strict serializer to avoid any deviations.
	return obj, actual, nil
}

call this decode function, then we can find that:
(see: k8s.io/apimachinery/pkg/runtime/serializer/versioning/versioning.go)
func (c *codec) Decode(data []byte, defaultGVK *schema.GroupVersionKind, into runtime.Object) (runtime.Object, *schema.GroupVersionKind, error) {
	// If the into object is unstructured and expresses an opinion about its group/version,
	// create a new instance of the type so we always exercise the conversion path (skips short-circuiting on `into == obj`)
	decodeInto := into
	if into != nil {
		if _, ok := into.(runtime.Unstructured); ok && !into.GetObjectKind().GroupVersionKind().GroupVersion().Empty() {
			decodeInto = reflect.New(reflect.TypeOf(into).Elem()).Interface().(runtime.Object)
		}
	}

	obj, gvk, err := c.decoder.Decode(data, defaultGVK, decodeInto)
	if err != nil {
		return nil, gvk, err
	}

	if d, ok := obj.(runtime.NestedObjectDecoder); ok {
		if err := d.DecodeNestedObjects(runtime.WithoutVersionDecoder{c.decoder}); err != nil {
			return nil, gvk, err
		}
	}

	// if we specify a target, use generic conversion.
	if into != nil {
		// perform defaulting if requested
		if c.defaulter != nil {
			c.defaulter.Default(obj)
		}

		// Short-circuit conversion if the into object is same
// store the unmarshal data into a temparory object.
		if into == obj {
			return into, gvk, nil
		}
//最后还有一行关键代码，将data的内容写到最外层的Into方法的入参中：
		if err := c.convertor.Convert(obj, into, c.decodeVersion); err != nil {
			return nil, gvk, err
		}

		return into, gvk, nil
	}

	// perform defaulting if requested
	if c.defaulter != nil {
		c.defaulter.Default(obj)

	//最后还有一行关键代码，将data的内容写到最外层的Into方法的入参中：}
	out, err := c.convertor.ConvertToVersion(obj, c.decodeVersion)
	if err != nil {
		return nil, gvk, err
	}
	return out, gvk, nil
}


*/
