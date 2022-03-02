package main

/*
本文是《client-go实战》系列的第四篇，前文学习了Clientset客户端，
发现Clientset在deployment、service这些kubernetes内置资源的时候是很方便的，
每个资源都有其专属的方法，配合官方API文档和数据结构定义，开发起来比Restclient高效；
但如果要处理的不是kubernetes的内置资源呢？
比如CRD，Clientset的代码中可没有用户自定义的东西，显然就用不上Clientset了，
此时本篇的主角dynamicClient登场

*/

/*
在正式学习dynamicClient之前，
有两个重要的知识点需要了解：Object.runtime和Unstructured，
对于整个kubernetes来说它们都是非常重要的；
Object.runtime
聊Object.runtime之前先要明确两个概念：资源和资源对象，关于资源大家都很熟悉了，
pod、deployment这些不都是资源嘛，个人的理解是资源更像一个严格的定义，
当在kubernetes中创建了一个deployment之后，这个新建的deployment实例就是资源对象了；
在kubernetes的代码世界中，资源对象对应着具体的数据结构实例，这些数据结构都实现了同一个接口，
名为Object.runtime，源码位置是:
staging/src/k8s.io/apimachinery/pkg/runtime/interfaces.go

type Object interface {
	GetObjectKind() schema.ObjectKind
	DeepCopyObject() Object
}

DeepCopyObject方法顾名思义，就是深拷贝，也就是将内存中的对象克隆出一个新的对象；
至于GetObjectKind方法的作用：处理Object.runtime类型的变量时，只要调用其GetObjectKind方法就知道它的具体身份了（如deployment，service等）；
最后再次强调：资源对象都是Object.runtime的实现

*/

/*
Unstructured
在聊Unstructured之前，先看一个简单的JSON字符串：
{
	"id": 101,
	"name": "Tom"
}

上述JSON的字段名称和字段值类型都是固定的，因此可以针对性编写一个数据结构来处理它：
type Person struct {
	ID int
	Name String
}

对于上面的JSON字符串就是结构化数据（Structured Data），这个应该好理解；
与结构化数据相对的就是非结构化数据了（Unstructured Data），
在实际的kubernetes环境中，可能会遇到一些无法预知结构的数据，
例如前面的JSON字符串中还有第三个字段，字段值的具体内容和类型在编码时并不知晓，
而是在真正运行的时候才知道，那么在编码时如何处理呢？go用interface{}来表示，
实际上client-go也是这么做的，来看Unstructured数据结构的源码，
路径是staging/src/k8s.io/apimachinery/pkg/apis/meta/v1/unstructured/unstructured.go：
type Unstructured struct {
	// Object is a JSON compatible map with string, float, int, bool, []interface{}, or
	// map[string]interface{}
	// children.
	Object map[string]interface{}
}

显然，上述数据结构定义并不能发挥什么作用，真正重要的是关联的方法，
client-go已经为Unstructured准备了丰富的方法，借助这些方法可以灵活的处理非结构化数据：
func (obj *Unstructured) GetObjectKind() schema.ObjectKind { return obj }

func (obj *Unstructured) IsList() bool {
	field, ok := obj.Object["items"]
	if !ok {
		return false
	}
	_, ok = field.([]interface{})
	return ok
}
func (obj *Unstructured) ToList() (*UnstructuredList, error) {
	if !obj.IsList() {
		// return an empty list back
		return &UnstructuredList{Object: obj.Object}, nil
	}

	ret := &UnstructuredList{}
	ret.Object = obj.Object

	err := obj.EachListItem(func(item runtime.Object) error {
		castItem := item.(*Unstructured)
		ret.Items = append(ret.Items, *castItem)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return ret, nil
}

func (obj *Unstructured) EachListItem(fn func(runtime.Object) error) error {
	field, ok := obj.Object["items"]
	if !ok {
		return errors.New("content is not a list")
	}
	items, ok := field.([]interface{})
	if !ok {
		return fmt.Errorf("content is not a list: %T", field)
	}
	for _, item := range items {
		child, ok := item.(map[string]interface{})
		if !ok {
			return fmt.Errorf("items member is not an object: %T", child)
		}
		if err := fn(&Unstructured{Object: child}); err != nil {
			return err
		}
	}
	return nil
}

func (obj *Unstructured) UnstructuredContent() map[string]interface{} {
	if obj.Object == nil {
		return make(map[string]interface{})
	}
	return obj.Object
}

func (obj *Unstructured) SetUnstructuredContent(content map[string]interface{}) {
	obj.Object = content
}

// MarshalJSON ensures that the unstructured object produces proper
// JSON when passed to Go's standard JSON library.
func (u *Unstructured) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	err := UnstructuredJSONScheme.Encode(u, &buf)
	return buf.Bytes(), err
}

// UnmarshalJSON ensures that the unstructured object properly decodes
// JSON when passed to Go's standard JSON library.
func (u *Unstructured) UnmarshalJSON(b []byte) error {
	_, _, err := UnstructuredJSONScheme.Decode(b, nil, u)
	return err
}

// NewEmptyInstance returns a new instance of the concrete type containing only kind/apiVersion and no other data.
// This should be called instead of reflect.New() for unstructured types because the go type alone does not preserve kind/apiVersion info.
func (in *Unstructured) NewEmptyInstance() runtime.Unstructured {
	out := new(Unstructured)
	if in != nil {
		out.GetObjectKind().SetGroupVersionKind(in.GetObjectKind().GroupVersionKind())
	}
	return out
}

func (in *Unstructured) DeepCopy() *Unstructured {
	if in == nil {
		return nil
	}
	out := new(Unstructured)
	*out = *in
	out.Object = runtime.DeepCopyJSON(in.Object)
	return out
}

func (u *Unstructured) setNestedField(value interface{}, fields ...string) {
	if u.Object == nil {
		u.Object = make(map[string]interface{})
	}
	SetNestedField(u.Object, value, fields...)
}

func (u *Unstructured) setNestedStringSlice(value []string, fields ...string) {
	if u.Object == nil {
		u.Object = make(map[string]interface{})
	}
	SetNestedStringSlice(u.Object, value, fields...)
}

func (u *Unstructured) setNestedSlice(value []interface{}, fields ...string) {
	if u.Object == nil {
		u.Object = make(map[string]interface{})
	}
	SetNestedSlice(u.Object, value, fields...)
}

func (u *Unstructured) setNestedMap(value map[string]string, fields ...string) {
	if u.Object == nil {
		u.Object = make(map[string]interface{})
	}
	SetNestedStringMap(u.Object, value, fields...)
}

func (u *Unstructured) GetOwnerReferences() []metav1.OwnerReference {
	field, found, err := NestedFieldNoCopy(u.Object, "metadata", "ownerReferences")
	if !found || err != nil {
		return nil
	}
	original, ok := field.([]interface{})
	if !ok {
		return nil
	}
	ret := make([]metav1.OwnerReference, 0, len(original))
	for _, obj := range original {
		o, ok := obj.(map[string]interface{})
		if !ok {
			// expected map[string]interface{}, got something else
			return nil
		}
		ret = append(ret, extractOwnerReference(o))
	}
	return ret
}

func (u *Unstructured) SetOwnerReferences(references []metav1.OwnerReference) {
	if references == nil {
		RemoveNestedField(u.Object, "metadata", "ownerReferences")
		return
	}

	newReferences := make([]interface{}, 0, len(references))
	for _, reference := range references {
		out, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&reference)
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("unable to convert Owner Reference: %v", err))
			continue
		}
		newReferences = append(newReferences, out)
	}
	u.setNestedField(newReferences, "metadata", "ownerReferences")
}

func (u *Unstructured) GetAPIVersion() string {
	return getNestedString(u.Object, "apiVersion")
}

func (u *Unstructured) SetAPIVersion(version string) {
	u.setNestedField(version, "apiVersion")
}

func (u *Unstructured) GetKind() string {
	return getNestedString(u.Object, "kind")
}

func (u *Unstructured) SetKind(kind string) {
	u.setNestedField(kind, "kind")
}

func (u *Unstructured) GetNamespace() string {
	return getNestedString(u.Object, "metadata", "namespace")
}

func (u *Unstructured) SetNamespace(namespace string) {
	if len(namespace) == 0 {
		RemoveNestedField(u.Object, "metadata", "namespace")
		return
	}
	u.setNestedField(namespace, "metadata", "namespace")
}

func (u *Unstructured) GetName() string {
	return getNestedString(u.Object, "metadata", "name")
}

func (u *Unstructured) SetName(name string) {
	if len(name) == 0 {
		RemoveNestedField(u.Object, "metadata", "name")
		return
	}
	u.setNestedField(name, "metadata", "name")
}

func (u *Unstructured) GetGenerateName() string {
	return getNestedString(u.Object, "metadata", "generateName")
}

func (u *Unstructured) SetGenerateName(generateName string) {
	if len(generateName) == 0 {
		RemoveNestedField(u.Object, "metadata", "generateName")
		return
	}
	u.setNestedField(generateName, "metadata", "generateName")
}

func (u *Unstructured) GetUID() types.UID {
	return types.UID(getNestedString(u.Object, "metadata", "uid"))
}

func (u *Unstructured) SetUID(uid types.UID) {
	if len(string(uid)) == 0 {
		RemoveNestedField(u.Object, "metadata", "uid")
		return
	}
	u.setNestedField(string(uid), "metadata", "uid")
}

func (u *Unstructured) GetResourceVersion() string {
	return getNestedString(u.Object, "metadata", "resourceVersion")
}

func (u *Unstructured) SetResourceVersion(resourceVersion string) {
	if len(resourceVersion) == 0 {
		RemoveNestedField(u.Object, "metadata", "resourceVersion")
		return
	}
	u.setNestedField(resourceVersion, "metadata", "resourceVersion")
}

func (u *Unstructured) GetGeneration() int64 {
	val, found, err := NestedInt64(u.Object, "metadata", "generation")
	if !found || err != nil {
		return 0
	}
	return val
}

func (u *Unstructured) SetGeneration(generation int64) {
	if generation == 0 {
		RemoveNestedField(u.Object, "metadata", "generation")
		return
	}
	u.setNestedField(generation, "metadata", "generation")
}

func (u *Unstructured) GetSelfLink() string {
	return getNestedString(u.Object, "metadata", "selfLink")
}

func (u *Unstructured) SetSelfLink(selfLink string) {
	if len(selfLink) == 0 {
		RemoveNestedField(u.Object, "metadata", "selfLink")
		return
	}
	u.setNestedField(selfLink, "metadata", "selfLink")
}

func (u *Unstructured) GetContinue() string {
	return getNestedString(u.Object, "metadata", "continue")
}

func (u *Unstructured) SetContinue(c string) {
	if len(c) == 0 {
		RemoveNestedField(u.Object, "metadata", "continue")
		return
	}
	u.setNestedField(c, "metadata", "continue")
}

func (u *Unstructured) GetRemainingItemCount() *int64 {
	return getNestedInt64Pointer(u.Object, "metadata", "remainingItemCount")
}

func (u *Unstructured) SetRemainingItemCount(c *int64) {
	if c == nil {
		RemoveNestedField(u.Object, "metadata", "remainingItemCount")
	} else {
		u.setNestedField(*c, "metadata", "remainingItemCount")
	}
}

func (u *Unstructured) GetCreationTimestamp() metav1.Time {
	var timestamp metav1.Time
	timestamp.UnmarshalQueryParameter(getNestedString(u.Object, "metadata", "creationTimestamp"))
	return timestamp
}

func (u *Unstructured) SetCreationTimestamp(timestamp metav1.Time) {
	ts, _ := timestamp.MarshalQueryParameter()
	if len(ts) == 0 || timestamp.Time.IsZero() {
		RemoveNestedField(u.Object, "metadata", "creationTimestamp")
		return
	}
	u.setNestedField(ts, "metadata", "creationTimestamp")
}

func (u *Unstructured) GetDeletionTimestamp() *metav1.Time {
	var timestamp metav1.Time
	timestamp.UnmarshalQueryParameter(getNestedString(u.Object, "metadata", "deletionTimestamp"))
	if timestamp.IsZero() {
		return nil
	}
	return &timestamp
}

func (u *Unstructured) SetDeletionTimestamp(timestamp *metav1.Time) {
	if timestamp == nil {
		RemoveNestedField(u.Object, "metadata", "deletionTimestamp")
		return
	}
	ts, _ := timestamp.MarshalQueryParameter()
	u.setNestedField(ts, "metadata", "deletionTimestamp")
}

func (u *Unstructured) GetDeletionGracePeriodSeconds() *int64 {
	val, found, err := NestedInt64(u.Object, "metadata", "deletionGracePeriodSeconds")
	if !found || err != nil {
		return nil
	}
	return &val
}

func (u *Unstructured) SetDeletionGracePeriodSeconds(deletionGracePeriodSeconds *int64) {
	if deletionGracePeriodSeconds == nil {
		RemoveNestedField(u.Object, "metadata", "deletionGracePeriodSeconds")
		return
	}
	u.setNestedField(*deletionGracePeriodSeconds, "metadata", "deletionGracePeriodSeconds")
}

func (u *Unstructured) GetLabels() map[string]string {
	m, _, _ := NestedStringMap(u.Object, "metadata", "labels")
	return m
}

func (u *Unstructured) SetLabels(labels map[string]string) {
	if labels == nil {
		RemoveNestedField(u.Object, "metadata", "labels")
		return
	}
	u.setNestedMap(labels, "metadata", "labels")
}

func (u *Unstructured) GetAnnotations() map[string]string {
	m, _, _ := NestedStringMap(u.Object, "metadata", "annotations")
	return m
}

func (u *Unstructured) SetAnnotations(annotations map[string]string) {
	if annotations == nil {
		RemoveNestedField(u.Object, "metadata", "annotations")
		return
	}
	u.setNestedMap(annotations, "metadata", "annotations")
}

func (u *Unstructured) SetGroupVersionKind(gvk schema.GroupVersionKind) {
	u.SetAPIVersion(gvk.GroupVersion().String())
	u.SetKind(gvk.Kind)
}

func (u *Unstructured) GroupVersionKind() schema.GroupVersionKind {
	gv, err := schema.ParseGroupVersion(u.GetAPIVersion())
	if err != nil {
		return schema.GroupVersionKind{}
	}
	gvk := gv.WithKind(u.GetKind())
	return gvk
}

func (u *Unstructured) GetFinalizers() []string {
	val, _, _ := NestedStringSlice(u.Object, "metadata", "finalizers")
	return val
}

func (u *Unstructured) SetFinalizers(finalizers []string) {
	if finalizers == nil {
		RemoveNestedField(u.Object, "metadata", "finalizers")
		return
	}
	u.setNestedStringSlice(finalizers, "metadata", "finalizers")
}

func (u *Unstructured) GetClusterName() string {
	return getNestedString(u.Object, "metadata", "clusterName")
}

func (u *Unstructured) SetClusterName(clusterName string) {
	if len(clusterName) == 0 {
		RemoveNestedField(u.Object, "metadata", "clusterName")
		return
	}
	u.setNestedField(clusterName, "metadata", "clusterName")
}

func (u *Unstructured) GetManagedFields() []metav1.ManagedFieldsEntry {
	items, found, err := NestedSlice(u.Object, "metadata", "managedFields")
	if !found || err != nil {
		return nil
	}
	managedFields := []metav1.ManagedFieldsEntry{}
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			utilruntime.HandleError(fmt.Errorf("unable to retrieve managedFields for object, item %v is not a map", item))
			return nil
		}
		out := metav1.ManagedFieldsEntry{}
		if err := runtime.DefaultUnstructuredConverter.FromUnstructured(m, &out); err != nil {
			utilruntime.HandleError(fmt.Errorf("unable to retrieve managedFields for object: %v", err))
			return nil
		}
		managedFields = append(managedFields, out)
	}
	return managedFields
}

func (u *Unstructured) SetManagedFields(managedFields []metav1.ManagedFieldsEntry) {
	if managedFields == nil {
		RemoveNestedField(u.Object, "metadata", "managedFields")
		return
	}
	items := []interface{}{}
	for _, managedFieldsEntry := range managedFields {
		out, err := runtime.DefaultUnstructuredConverter.ToUnstructured(&managedFieldsEntry)
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("unable to set managedFields for object: %v", err))
			return
		}
		items = append(items, out)
	}
	u.setNestedSlice(items, "metadata", "managedFields")
}
*/

import (
	"context"
	"flag"
	"fmt"
	//appsv1 "k8s.io/api/apps/v1"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"path/filepath"
)

func main() {
	/*
	重要知识点：Unstructured与资源对象的相互转换
另外还有一个非常重要的知识点：可以用Unstructured实例生成资源对象，
	也可以用资源对象生成Unstructured实例，
	这个神奇的能力是unstructuredConverter的
	FromUnstructured和ToUnstructured方法分别实现的，
	下面的代码片段展示了如何将Unstructured实例转为PodList实例：
	*/
	// 实例化一个PodList数据结构，用于接收从unstructObj转换后的结果
	//podList := &apiv1.PodList{}
	//
	//// unstructObj
	//err := runtime.DefaultUnstructuredConverter.FromUnstructured(unstructObj.UnstructuredContent(), podList)

//	您可能会好奇上述FromUnstructured方法究竟是如何实现转换的，
	//	咱们去看下此方法的内部实现，
	//	其实也没啥悬念了，通过反射可以得到podList的字段信息:
	/*
	// FromUnstructured converts an object from map[string]interface{} representation into a concrete type.
// It uses encoding/json/Unmarshaler if object implements it or reflection if not.
func (c *unstructuredConverter) FromUnstructured(u map[string]interface{}, obj interface{}) error {

	// use reflect machnism to get the type and value of object
	t := reflect.TypeOf(obj)
	value := reflect.ValueOf(obj)

	if t.Kind() != reflect.Ptr || value.IsNil() {
		return fmt.Errorf("FromUnstructured requires a non-nil pointer to an object, got %v", t)
	}

	// detail function
	err := fromUnstructured(reflect.ValueOf(u), value.Elem())
	if c.mismatchDetection {
		newObj := reflect.New(t.Elem()).Interface()
		newErr := fromUnstructuredViaJSON(u, newObj)
		if (err != nil) != (newErr != nil) {
			klog.Fatalf("FromUnstructured unexpected error for %v: error: %v", u, err)
		}
		if err == nil && !c.comparison.DeepEqual(obj, newObj) {
			klog.Fatalf("FromUnstructured mismatch\nobj1: %#v\nobj2: %#v", obj, newObj)
		}
	}
	return err
}
	*/

	/*
	至此，Unstructured的分析就结束了吗？
	没有，强烈推荐您进入fromUnstructured方法去看细节，
	这里面是非常精彩的，以podList为例，这是个数据结构，
	而fromUnstructured只处理原始类型:
	func fromUnstructuredViaJSON(u map[string]interface{}, obj interface{}) error {
	data, err := json.Marshal(u)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, obj)
}

func fromUnstructured(sv, dv reflect.Value) error {
	sv = unwrapInterface(sv)
	if !sv.IsValid() {
		dv.Set(reflect.Zero(dv.Type()))
		return nil
	}
	st, dt := sv.Type(), dv.Type()

	switch dt.Kind() {
	case reflect.Map, reflect.Slice, reflect.Ptr, reflect.Struct, reflect.Interface:
		// Those require non-trivial conversion.
	default:
		// This should handle all simple types.
		if st.AssignableTo(dt) {
			dv.Set(sv)
			return nil
		}
		// We cannot simply use "ConvertibleTo", as JSON doesn't support conversions
		// between those four groups: bools, integers, floats and string. We need to
		// do the same.
		if st.ConvertibleTo(dt) {
			switch st.Kind() {
			case reflect.String:
				switch dt.Kind() {
				case reflect.String:
					dv.Set(sv.Convert(dt))
					return nil
				}
			case reflect.Bool:
				switch dt.Kind() {
				case reflect.Bool:
					dv.Set(sv.Convert(dt))
					return nil
				}
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
				reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				switch dt.Kind() {
				case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
					reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
					dv.Set(sv.Convert(dt))
					return nil
				}
			case reflect.Float32, reflect.Float64:
				switch dt.Kind() {
				case reflect.Float32, reflect.Float64:
					dv.Set(sv.Convert(dt))
					return nil
				}
				if sv.Float() == math.Trunc(sv.Float()) {
					dv.Set(sv.Convert(dt))
					return nil
				}
			}
			return fmt.Errorf("cannot convert %s to %s", st.String(), dt.String())
		}
	}

	// Check if the object has a custom JSON marshaller/unmarshaller.
	entry := value.TypeReflectEntryOf(dv.Type())
	if entry.CanConvertFromUnstructured() {
		return entry.FromUnstructured(sv, dv)
	}

	switch dt.Kind() {
	case reflect.Map:
		return mapFromUnstructured(sv, dv)
	case reflect.Slice:
		return sliceFromUnstructured(sv, dv)
	case reflect.Ptr:
		return pointerFromUnstructured(sv, dv)
	case reflect.Struct:
	//对于数据结构会调用structFromUnstructured方法处理，
	//在structFromUnstructured方法中:
		return structFromUnstructured(sv, dv)
	case reflect.Interface:
		return interfaceFromUnstructured(sv, dv)
	default:
		return fmt.Errorf("unrecognized type: %v", dt.Kind())
	}
}
	//在structFromUnstructured方法中:
	func structFromUnstructured(sv, dv reflect.Value) error {
	st, dt := sv.Type(), dv.Type()
	if st.Kind() != reflect.Map {
		return fmt.Errorf("cannot restore struct from: %v", st.Kind())
	}

	for i := 0; i < dt.NumField(); i++ {
		fieldInfo := fieldInfoFromField(dt, i)
		fv := dv.Field(i)

		if len(fieldInfo.name) == 0 {
			// This field is inlined.
			if err := fromUnstructured(sv, fv); err != nil {
				return err
			}
		} else {
			value := unwrapInterface(sv.MapIndex(fieldInfo.nameValue))
			if value.IsValid() {
				if err := fromUnstructured(value, fv); err != nil {
					return err
				}
			} else {
				fv.Set(reflect.Zero(fv.Type()))
			}
		}
	}
	return nil
}
	
	*/

	/*
	处理数据结构的每个字段，又会调用fromUnstructured，
	这是相互迭代的过程，最终，不论podList中有多少数据结构的嵌套都会被处理掉，
	篇幅所限就不展开相信分析了，下图是一部分关键代码：
	func structFromUnstructured(sv, dv reflect.Value) error {
	st, dt := sv.Type(), dv.Type()
	if st.Kind() != reflect.Map {
		return fmt.Errorf("cannot restore struct from: %v", st.Kind())
	}

	for i := 0; i < dt.NumField(); i++ {
		fieldInfo := fieldInfoFromField(dt, i)
		fv := dv.Field(i)

		if len(fieldInfo.name) == 0 {
			// This field is inlined.
			if err := fromUnstructured(sv, fv); err != nil {
				return err
			}
		} else {
			value := unwrapInterface(sv.MapIndex(fieldInfo.nameValue))
			if value.IsValid() {
				if err := fromUnstructured(value, fv); err != nil {
					return err
				}
			} else {
				fv.Set(reflect.Zero(fv.Type()))
			}
		}
	}
	return nil
}
	*/

//	小结：Unstructured转为资源对象的套路并不神秘，无非是用反射取得资源对象的字段类型，然后按照字段名去Unstructured的map中取得原始数据，再用反射设置到资源对象的字段中即可；
	//做完了准备工作，接下来该回到本篇文章的主题了：dynamicClient客户端

	/*
	关于dynamicClient
deployment、pod这些资源，其数据结构是明确的固定的，可以精确对应到Clientset中的数据结构和方法，但是对于CRD（用户自定义资源），Clientset客户端就无能为力了，此时需要有一种数据结构来承载资源对象的数据，也要有对应的方法来处理这些数据；
此刻，前面提到的Unstructured可以登场了，没错，把Clientset不支持的资源对象交给Unstructured来承载，接下来看看dynamicClient和Unstructured的关系：
先看数据结构定义，和clientset没啥区别，只有个restClient字段：

	type dynamicClient struct {
		client *rest.RESTClient
	}
	这个数据结构只有一个关联方法Resource，
	入参为GVR，返回的是另一个数据结构dynamicResourceClient：
	func (c *dynamicClient) Resource(resource schema.GroupVersionResource) NamespaceableResourceInterface {
	return &dynamicResourceClient{client: c, resource: resource}
}
	*/

	/*
	通过上述代码可知，dynamicClient的关键是数据结构dynamicResourceClient及其关联方法，
	来看看这个dynamicResourceClient，果然，dynamicClient所有和资源相关的操作
	都是dynamicResourceClient在做（代理模式？）:
	type dynamicResourceClient struct {
		client    *dynamicClient
		namespace string
		resource  schema.GroupVersionResource
	}

	func (c *dynamicResourceClient) Namespace(ns string) ResourceInterface {
	ret := *c
	ret.namespace = ns
	return &ret
}

func (c *dynamicResourceClient) Create(ctx context.Context, obj *unstructured.Unstructured, opts metav1.CreateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	outBytes, err := runtime.Encode(unstructured.UnstructuredJSONScheme, obj)
	if err != nil {
		return nil, err
	}
	name := ""
	if len(subresources) > 0 {
		accessor, err := meta.Accessor(obj)
		if err != nil {
			return nil, err
		}
		name = accessor.GetName()
		if len(name) == 0 {
			return nil, fmt.Errorf("name is required")
		}
	}

	result := c.client.client.
		Post().
		AbsPath(append(c.makeURLSegments(name), subresources...)...).
		Body(outBytes).
		SpecificallyVersionedParams(&opts, dynamicParameterCodec, versionV1).
		Do(ctx)
	if err := result.Error(); err != nil {
		return nil, err
	}

	retBytes, err := result.Raw()
	if err != nil {
		return nil, err
	}
	uncastObj, err := runtime.Decode(unstructured.UnstructuredJSONScheme, retBytes)
	if err != nil {
		return nil, err
	}
	return uncastObj.(*unstructured.Unstructured), nil
}

func (c *dynamicResourceClient) Update(ctx context.Context, obj *unstructured.Unstructured, opts metav1.UpdateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return nil, err
	}
	name := accessor.GetName()
	if len(name) == 0 {
		return nil, fmt.Errorf("name is required")
	}
	outBytes, err := runtime.Encode(unstructured.UnstructuredJSONScheme, obj)
	if err != nil {
		return nil, err
	}

	result := c.client.client.
		Put().
		AbsPath(append(c.makeURLSegments(name), subresources...)...).
		Body(outBytes).
		SpecificallyVersionedParams(&opts, dynamicParameterCodec, versionV1).
		Do(ctx)
	if err := result.Error(); err != nil {
		return nil, err
	}

	retBytes, err := result.Raw()
	if err != nil {
		return nil, err
	}
	uncastObj, err := runtime.Decode(unstructured.UnstructuredJSONScheme, retBytes)
	if err != nil {
		return nil, err
	}
	return uncastObj.(*unstructured.Unstructured), nil
}

func (c *dynamicResourceClient) UpdateStatus(ctx context.Context, obj *unstructured.Unstructured, opts metav1.UpdateOptions) (*unstructured.Unstructured, error) {
	accessor, err := meta.Accessor(obj)
	if err != nil {
		return nil, err
	}
	name := accessor.GetName()
	if len(name) == 0 {
		return nil, fmt.Errorf("name is required")
	}

	outBytes, err := runtime.Encode(unstructured.UnstructuredJSONScheme, obj)
	if err != nil {
		return nil, err
	}

	result := c.client.client.
		Put().
		AbsPath(append(c.makeURLSegments(name), "status")...).
		Body(outBytes).
		SpecificallyVersionedParams(&opts, dynamicParameterCodec, versionV1).
		Do(ctx)
	if err := result.Error(); err != nil {
		return nil, err
	}

	retBytes, err := result.Raw()
	if err != nil {
		return nil, err
	}
	uncastObj, err := runtime.Decode(unstructured.UnstructuredJSONScheme, retBytes)
	if err != nil {
		return nil, err
	}
	return uncastObj.(*unstructured.Unstructured), nil
}

func (c *dynamicResourceClient) Delete(ctx context.Context, name string, opts metav1.DeleteOptions, subresources ...string) error {
	if len(name) == 0 {
		return fmt.Errorf("name is required")
	}
	deleteOptionsByte, err := runtime.Encode(deleteOptionsCodec.LegacyCodec(schema.GroupVersion{Version: "v1"}), &opts)
	if err != nil {
		return err
	}

	result := c.client.client.
		Delete().
		AbsPath(append(c.makeURLSegments(name), subresources...)...).
		Body(deleteOptionsByte).
		Do(ctx)
	return result.Error()
}

func (c *dynamicResourceClient) DeleteCollection(ctx context.Context, opts metav1.DeleteOptions, listOptions metav1.ListOptions) error {
	deleteOptionsByte, err := runtime.Encode(deleteOptionsCodec.LegacyCodec(schema.GroupVersion{Version: "v1"}), &opts)
	if err != nil {
		return err
	}

	result := c.client.client.
		Delete().
		AbsPath(c.makeURLSegments("")...).
		Body(deleteOptionsByte).
		SpecificallyVersionedParams(&listOptions, dynamicParameterCodec, versionV1).
		Do(ctx)
	return result.Error()
}

func (c *dynamicResourceClient) Get(ctx context.Context, name string, opts metav1.GetOptions, subresources ...string) (*unstructured.Unstructured, error) {
	if len(name) == 0 {
		return nil, fmt.Errorf("name is required")
	}
	result := c.client.client.Get().AbsPath(append(c.makeURLSegments(name), subresources...)...).SpecificallyVersionedParams(&opts, dynamicParameterCodec, versionV1).Do(ctx)
	if err := result.Error(); err != nil {
		return nil, err
	}
	retBytes, err := result.Raw()
	if err != nil {
		return nil, err
	}
	uncastObj, err := runtime.Decode(unstructured.UnstructuredJSONScheme, retBytes)
	if err != nil {
		return nil, err
	}
	return uncastObj.(*unstructured.Unstructured), nil
}

func (c *dynamicResourceClient) List(ctx context.Context, opts metav1.ListOptions) (*unstructured.UnstructuredList, error) {
	result := c.client.client.Get().AbsPath(c.makeURLSegments("")...).SpecificallyVersionedParams(&opts, dynamicParameterCodec, versionV1).Do(ctx)
	if err := result.Error(); err != nil {
		return nil, err
	}
	retBytes, err := result.Raw()
	if err != nil {
		return nil, err
	}
	uncastObj, err := runtime.Decode(unstructured.UnstructuredJSONScheme, retBytes)
	if err != nil {
		return nil, err
	}
	if list, ok := uncastObj.(*unstructured.UnstructuredList); ok {
		return list, nil
	}

	list, err := uncastObj.(*unstructured.Unstructured).ToList()
	if err != nil {
		return nil, err
	}
	return list, nil
}

func (c *dynamicResourceClient) Watch(ctx context.Context, opts metav1.ListOptions) (watch.Interface, error) {
	opts.Watch = true
	return c.client.client.Get().AbsPath(c.makeURLSegments("")...).
		SpecificallyVersionedParams(&opts, dynamicParameterCodec, versionV1).
		Watch(ctx)
}

func (c *dynamicResourceClient) Patch(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (*unstructured.Unstructured, error) {
	if len(name) == 0 {
		return nil, fmt.Errorf("name is required")
	}
	result := c.client.client.
		Patch(pt).
		AbsPath(append(c.makeURLSegments(name), subresources...)...).
		Body(data).
		SpecificallyVersionedParams(&opts, dynamicParameterCodec, versionV1).
		Do(ctx)
	if err := result.Error(); err != nil {
		return nil, err
	}
	retBytes, err := result.Raw()
	if err != nil {
		return nil, err
	}
	uncastObj, err := runtime.Decode(unstructured.UnstructuredJSONScheme, retBytes)
	if err != nil {
		return nil, err
	}
	return uncastObj.(*unstructured.Unstructured), nil
}

func (c *dynamicResourceClient) makeURLSegments(name string) []string {
	url := []string{}
	if len(c.resource.Group) == 0 {
		url = append(url, "api")
	} else {
		url = append(url, "apis", c.resource.Group)
	}
	url = append(url, c.resource.Version)

	if len(c.namespace) > 0 {
		url = append(url, "namespaces", c.namespace)
	}
	url = append(url, c.resource.Resource)

	if len(name) > 0 {
		url = append(url, name)
	}

	return url
}

	选了create方法细看，序列化和反序列化都交给unstructured的UnstructuredJSONScheme，与kubernetes的交互交给Restclient：
	*/

	/**
	func (c *dynamicResourceClient) Create(ctx context.Context, obj *unstructured.Unstructured, opts metav1.CreateOptions, subresources ...string) (*unstructured.Unstructured, error) {
	//序列化和反序列化都交给unstructured的UnstructuredJSONScheme
	outBytes, err := runtime.Encode(unstructured.UnstructuredJSONScheme, obj)
	if err != nil {
		return nil, err
	}
	name := ""
	if len(subresources) > 0 {
		accessor, err := meta.Accessor(obj)
		if err != nil {
			return nil, err
		}
		name = accessor.GetName()
		if len(name) == 0 {
			return nil, fmt.Errorf("name is required")
		}
	}
	//与kubernetes的交互交给Restclient
	result := c.client.client.
		Post().
		AbsPath(append(c.makeURLSegments(name), subresources...)...).
		Body(outBytes).
		SpecificallyVersionedParams(&opts, dynamicParameterCodec, versionV1).
		Do(ctx)
	if err := result.Error(); err != nil {
		return nil, err
	}

	retBytes, err := result.Raw()
	if err != nil {
		return nil, err
	}
	序列化(Encode)和反序列化(Decode)都交给unstructured的UnstructuredJSONScheme
	uncastObj, err := runtime.Decode(unstructured.UnstructuredJSONScheme, retBytes)
	if err != nil {
		return nil, err
	}
	return uncastObj.(*unstructured.Unstructured), nil
}
	**/

	/*
	小结：
	与Clientset不同，dynamicClient为各种类型的资源都提供统一的操作API，资源需要包装为Unstructured数据结构；
	内部使用了Restclient与kubernetes交互；
	对dynamicClient的介绍分析就这些吧，可以开始实战了；
	*/

	/*
	需求确认
本次编码实战的需求很简单：查询指定namespace下的所有pod，然后在控制台打印出来，
	要求用dynamicClient实现；
pod是kubernetes的内置资源，更适合Clientset来操作，
	而dynamicClient更适合处理CRD不是么？
	这里用pod是因为折腾CRD太麻烦了，定义好了还要在kubernetes上发布，
	于是干脆用pod来代替CRD，反正dynamicClient都能处理，
	咱们通过实战掌握dynamicClient的用法就行了，以后遇到各种资源都能处理之；
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

	dynamicClient,err := dynamic.NewForConfig(config)

	if err != nil{
		panic(err.Error())
	}
	// dynamicClient的唯一关联方法所需的入参
	gvr := schema.GroupVersionResource{
		Version: "v1",
		Resource: "pods",
	}

	unstructObj,err := dynamicClient.
		Resource(gvr).
		Namespace("kube-system").List(context.TODO(),metav1.ListOptions{Limit: 100})

	if err != nil{
		panic(err.Error())
	}

	// 实例化一个PodList数据结构，用于接收从unstructObj转换后的结果
	podList := &apiv1.PodList{}

	// 转换
	err = runtime.DefaultUnstructuredConverter.FromUnstructured(unstructObj.UnstructuredContent(),podList)
	// UnstructedContent using for list results (have Items field)
	/*
	// UnstructuredContent returns a map contain an overlay of the Items field onto
// the Object field. Items always overwrites overlay.
func (u *UnstructuredList) UnstructuredContent() map[string]interface{} {
	out := make(map[string]interface{}, len(u.Object)+1)

	// shallow copy every property
	for k, v := range u.Object {
		out[k] = v
	}

	items := make([]interface{}, len(u.Items))
	for i, item := range u.Items {
		items[i] = item.UnstructuredContent()
	}
	out["items"] = items
	return out
}
	*/

	// 表头
	fmt.Printf("namespace\t status\t\t name\n")

	// 每个pod都打印namespace、status.Phase、name三个字段
	for _, d := range podList.Items {
		fmt.Printf("%v\t %v\t %v\n",
			d.Namespace,
			d.Status.Phase,
			d.Name)
	}

	/*
	上述代码中有三处重点需要注意：
	1.Resource方法指定了本次操作的资源类型；
	2.List方法向kubernetes发起请求；
	3.FromUnstructured将Unstructured数据结构转成PodList，其原理前面已经分析过；

	至此，dynamicClient的学习和实战就完成了，
	它是名副其实的动态客户端工具，用一套API处理所有资源，
	除了突破Clientset的内置资源限制，还让我们的业务代码有了更大的灵活性.

	*/
}