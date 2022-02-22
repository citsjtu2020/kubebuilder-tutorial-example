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

package v1

import (
	"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reflect"
	"strconv"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.
// 期望状态
// ElasticWebSpec defines the desired state of ElasticWeb
type ElasticWebSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of ElasticWeb. Edit ElasticWeb_types.go to remove/update
	//Foo string `json:"foo,omitempty"`
	// 业务服务对应的镜像，包括名称:tag

	// +kubebuilder:validation:MinLength=0
	Image string `json:"image"`

	// service占用的宿主机端口，外部请求通过此端口访问pod的服务

	// +kubebuilder:validation:Minimum=0
	Port *int32 `json:"port"`

	// +kubebuilder:validation:Minimum=0
	ContainerPort *int32 `json:"containerPort"`

	// +optional
	Resource corev1.ResourceRequirements `json:"resource,omitempty"`

	//// +optional
	//Limit corev1.ResourceList `json:"limit,omitempty"`

	// 单个pod的QPS上限

	// +kubebuilder:validation:Minimum=0
	SinglePodQPS *int32 `json:"singlePodQPS"`

	// 当前整个业务的总QPS

	// +kubebuilder:validation:Minimum=0
	TotalQPS *int32 `json:"totalQPS"`
}

// 实际状态，该数据结构中的值都是业务代码计算出来的
// ElasticWebStatus defines the observed state of ElasticWeb
type ElasticWebStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// 当前kubernetes中实际支持的总QPS

	// +kubebuilder:validation:Minimum=0
	// +optional
	RealTotalQPS *int32 `json:"realQPS,omitempty"`
	RealAvailQPS *int32 `json:"realAvailQPS,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// ElasticWeb is the Schema for the elasticwebs API
type ElasticWeb struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ElasticWebSpec   `json:"spec,omitempty"`
	Status ElasticWebStatus `json:"status,omitempty"`
}

//func (in *ElasticWeb) Default() {
//	panic("implement me")
//}

// +kubebuilder:object:root=true

// ElasticWebList contains a list of ElasticWeb
type ElasticWebList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ElasticWeb `json:"items"`
}

func (in *ElasticWeb) String() string {
	var realQPS string
	var realAvailQPS string

	if nil == in.Status.RealTotalQPS {
		realQPS = "nil"
	} else {
		realQPS = strconv.Itoa(int((*in.Status.RealTotalQPS)))
	}

	if nil == in.Status.RealAvailQPS {
		realAvailQPS = "nil"
	} else {
		realAvailQPS = strconv.Itoa(int((*in.Status.RealAvailQPS)))
	}

	return fmt.Sprintf("Image [%s], Port [%d], SinglePodQPS [%d], TotalQPS [%d], RealQPS [%s], RealAvailQPS [%s]",
		in.Spec.Image,
		*(in.Spec.Port),
		*(in.Spec.SinglePodQPS),
		*(in.Spec.TotalQPS),
		realQPS,
		realAvailQPS)

}

func init() {
	SchemeBuilder.Register(&ElasticWeb{}, &ElasticWebList{})
}

//+kubebuilder:docs-gen:collapse=Root Object Definitions

func IsResourceEmpty(s corev1.ResourceRequirements) bool {
	return reflect.DeepEqual(s, corev1.ResourceRequirements{})
}
