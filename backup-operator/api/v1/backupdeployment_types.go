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

package v1

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// BackupDeploymentSpec defines the desired state of BackupDeployment
type BackupDeploymentSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Foo is an example field of BackupDeployment. Edit BackupDeployment_types.go to remove/update
	//Foo string `json:"foo,omitempty"`

	// +optional
	ServiceName []string `json:"serviceName"`

	// +kubebuilder:validation:Minimum=0
	// +optional
	InitalWait *int64 `json:"initalWait"` //default: ms

	// +kubebuilder:validation:MinLength=0
	Action string `json:"action"` // control the backup to waiting or running
	// 调度到同一台/均衡
	// +kubebuilder:validation:Minimum=0
	ScheduleStrategy *int64 `json:"scheduleStrategy"`
	// 选择机器，选择资源最多的、最少的
	// +kubebuilder:validation:Minimum=0
	AllocateStrategy *int64 `json:"allocateStrategy"`

	// +kubebuilder:validation:Minimum=0
	BackupReplicas *int64 `json:"backupReplicas"`

	// +kubebuilder:validation:Minimum=1
	RunningReplicas *int64 `json:"runningReplicas"` // at last 1  replica need to be running
	// 一个deploy最大的副本数
	UnitReplicas *int64 `json:"unitReplicas"`

	BackupSpec appsv1.DeploymentSpec `json:"backupSpec"`

	RunningSpec appsv1.DeploymentSpec `json:"runningSpec"`
}

// BackupDeploymentStatus defines the observed state of BackupDeployment
type BackupDeploymentStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// +optional
	Active []corev1.ObjectReference `json:"active,omitempty"`

	// +optional
	Back []corev1.ObjectReference `json:"back,omitempty"`

	// +optional
	Wait []corev1.ObjectReference `json:"wait,omitempty"`

	// +optional
	LastID *int64 `json:"last_id,omitempty"`

	// +optional
	Status string `json:"status,omitempty"`

	// +optional
	ScheduleStrategy *int64 `json:"scheduleStrategy,omitempty"`

	// +optional
	AllocateStrategy *int64 `json:"allocateStrategy,omitempty"`

	// +optionl
	UnitReplicas *int64 `json:"unitReplicas,omitempty"`

	// Information when was the last time the job was successfully scheduled.
	// +optional
	LastScaleTime *metav1.Time `json:"lastScaleTime,omitempty"`
}

type DeployState string

var ActiveState DeployState = "active"
var WaitingState DeployState = "waiting"
var BackupState DeployState = "backup"

type ActionState string

var Running ActionState = "running"
var Waiting ActionState = "waiting"

type ScaleState string

var Scale ScaleState = "scale"
var Keep ScaleState = "keep"
var DeploymentName = "deployment"

// +kubebuilder:subresource:status
// +kubebuilder:object:root=true

// BackupDeployment is the Schema for the backupdeployments API
type BackupDeployment struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   BackupDeploymentSpec   `json:"spec,omitempty"`
	Status BackupDeploymentStatus `json:"status,omitempty"`
}

type ScheduleStrategy int

var ScheduleOnSameHosts ScheduleStrategy = 1
var ScheduleOnSameHostsHard ScheduleStrategy = 2
var ScheduleRoundBin ScheduleStrategy = 3
var ScheduleRoundBinHard ScheduleStrategy = 4

type AllocateStrategy int

var BinPacking AllocateStrategy = 1
var LeastUsage AllocateStrategy = 2

// +kubebuilder:object:root=true

// BackupDeploymentList contains a list of BackupDeployment
type BackupDeploymentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []BackupDeployment `json:"items"`
}

func init() {
	SchemeBuilder.Register(&BackupDeployment{}, &BackupDeploymentList{})
}
