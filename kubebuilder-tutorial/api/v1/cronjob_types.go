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

/*
Apache License
创建一个新的 Kind (你还记得上一章的内容, 对吗?)和相应的 controller，
我们可以使用 kubebuilder create api 命令:
kubebuilder create api --group batch --version v1 --kind CronJob

当我们第一次执行这个命令时，它会为创建一个新的 group-version 目录
在这里，api/v1/ 这个目录会被创建,
对应的 group-version 是 batch.tutorial.kubebuilder.io/v1
(还记得我们一开始设置的--domain setting) 参数么)

它也会创建 api/v1/cronjob_types.go 文件并添加 CronJob Kind,
每次我们运行这个命令但是指定不同的 Kind 时, 他都会为我们创建一个 xxx_types.go 文件。

先让我们看看我们得到了什么开箱即用的东西，然后我们就开始填写它。
0.Apache License (as the beginning of the file)

*/
package v1

import (
	/*1. import some basic package
	最开始我们导入了 meta/v1 库，该库通常不会直接使用，
	但是它包含了 Kubernetes 所有 kind 的 metadata 结构体类型。
	*/
	batchv1beta1 "k8s.io/api/batch/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

/*
2. define the Spec and Status for the API resource/kind
下一步，我们为我们的 Kind 定义 Spec(CronJobSpec) 和 Status(CronJobStatus) 的类型。
Kubernetes 的功能是将期望状态(Spec)与集群实际状态(其他对象的Status)和外部状态进行协调。
然后记录下观察到的状态（Status）。
因此，每个具有功能的对象都包含 Spec 和 Status 。
但是 ConfigMap 之类的一些类型不遵循此模式，因为它们不会维护一种状态，但是大多数类型都需要。

*/

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// +kubebuilder:docs-gen:collapse=Imports

// CronJobSpec defines the desired state of CronJob
type CronJobSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	/*
		Foo is an example field of CronJob. Edit CronJob_types.go to remove/update
		Foo string `json:"foo,omitempty"`
			首先，让我们看一下 spec，如我们之前说的，spec 表明期望的状态，
			所以有关 controller 的任何 “inputs” 都在这里声明。
			一个基本的 CronJob 需要以下功能：

		   一个 schedule(调度器) -- (CronJob 中的 “Cron”)
		   用来运行 Job 的模板 -- (CronJob 中的 “Job”)

			为了让用户体现更好，我们也需要一些额外的功能：

		   一个截止时间（StartingDeadlineSeconds）, 如果错过了这个截止时间，Job 将会等到下一个调度时间点再被调度。
		   如果多个 Job 同时启动要怎么做（ConcurrencyPolicy）（等待？停掉最老的一个？还是同时运行？）
		   一个暂停(Suspend)功能，以防止 Job 在运行过程中出现什么错误。
		   限制历史 Job 的数量（SuccessfulJobsHistoryLimit）
			请记住，因为 job 不会读取自己的状态，所以我们需要用一些方式去跟踪一个 CronJob 是否已经运行过 Job， 我们可以用至少一个 old job 来完成这个功能。

		我们会用几个标记 (//+comment) 去定义一些额外的数据，这些标记在 controller-tools 生成 CRD manifest 时会被使用。 稍后我们还将看到，controller-tools 还会使用 GoDoc 来为字段生成描述信息。

	*/

	// +kubebuilder:validation:MinLength=0

	Schedule string `json:"schedule"` // The schedule in Cron format, see https://en.wikipedia.org/wiki/Cron.

	// +kubebuilder:validation:Minimum=0

	// +optional
	StartingDeadlineSeconds *int64 `json:"startingDeadlineSeconds,omitempty"`
	/*
			Optional deadline in seconds for starting the job if it misses scheduled
		   time for any reason.  Missed jobs executions will be counted as failed ones.

	*/

	/*
			Specifies how to treat concurrent executions of a Job.
		   Valid values are:
		- "Allow" (default): allows CronJobs to run concurrently;
		- "Forbid": forbids concurrent runs, skipping next run if previous run hasn't finished yet;
		- "Replace": cancels currently running job and replaces it with a new one;
	*/
	// +optional
	ConcurrencyPolicy ConcurrencyPolicy `json:"concurrencyPolicy,omitempty"`

	/*
		This flag tells the controller to suspend subsequent executions, it does
		not apply to already started executions.  Defaults to false.
	*/

	// +optional
	Suspend *bool `json:"suspend"`

	JobTemplate batchv1beta1.JobTemplateSpec `json:"jobTemplate"` // Specifies the job that will be created when executing a CronJob.

	/*
		The number of successful finished jobs to retain.
		   This is a pointer to distinguish between explicit zero and not specified.

	*/

	// +kubebuilder:validation:Minimum=0

	// +optional

	SuccessfulJobHistoryLimit *int32 `json:"successfulJobHistoryLimit,omitempty"`

	/*
		The number of failed finished jobs to retain.
		This is a pointer to distinguish between explicit zero and not specified.

	*/

	// +kubebuilder:validation:Minimum=0

	// +optional
	FailedJobsHistoryLimit *int32 `json:"failedJobsHistoryLimit,omitempty"`
}

/*
我们自定义了一个类型（ConcurrencyPolicy）来保存我们的并发策略。
它实际上只是一个字符串，但是这个类型名称提供了额外的说明，
我们还将验证附加到类型上，而不是字段上，从而使验证更易于重用。
ConcurrencyPolicy describes how the job will be handled.
Only one of the following concurrent policies may be specified.
If none of the following policies is specified, the default one
is AllowConcurrent.
*/
/*验证附加到类型上，而不是字段上，从而使验证更易于重用*/

// +kubebuilder:validation:Enum=Allow;Forbid;Replace
type ConcurrencyPolicy string

const (
	// AllowConcurrent allows CronJobs to run concurrently.
	AllowConcurrent ConcurrencyPolicy = "Allow"

	// ForbidConcurrent forbids concurrent runs, skipping next run if previous
	// hasn't finished yet.
	ForbidConcurrent ConcurrencyPolicy = "Forbid"

	// ReplaceConcurrent cancels currently running job and replaces it with a new one.
	ReplaceConcurrent ConcurrencyPolicy = "Replace"
)

// CronJobStatus defines the observed state of CronJob
type CronJobStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	/*
			下面，让我们设计 status,它用来存储观察到的状态。
			它包含我们希望用户或其他 controllers 能够获取的所有信息

		我们将保留一份正在运行的 Job 列表，以及最后一次成功运行 Job 的时间。
		注意，如上所述，我们使用 metav1.Time 而不是 time.Time 来获得稳定的序列化。

		A list of pointers to currently running jobs.

	*/
	// +optional
	Active []corev1.ObjectReference `json:"active,omitempty"`

	// Information when was the last time the job was successfully scheduled.
	// +optional
	LastScheduleTime *metav1.Time `json:"lastScheduleTime,omitempty"`
}

/*
3. define the go type (struct) for the GVK
然后，我们定义了 Kinds 对应的结构体类型，CronJob 和 CronJobList。
CronJob 是我们的 root type，用来描述 CronJob Kind。
和所有 Kubernetes 对象一样， 它包含 TypeMeta (用来定义 API version 和 Kind)
和 ObjectMeta (用来定义 name、namespace 和 labels等一些字段)

CronJobList 包含了一个 CronJob 的切片，它是用来批量操作 Kind 的，比如 LIST 操作
通常，我们不会修改它们 -- 所有的修改都是在 Spec 和 Status 上进行的。

CronJob is the Schema for the cronjobs API
//CronJob 是我们的 root type，用来描述 CronJob Kind。

*/

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

type CronJob struct {
	metav1.TypeMeta   `json:",inline"`            //TypeMeta (用来定义 API version 和 Kind)
	metav1.ObjectMeta `json:"metadata,omitempty"` //ObjectMeta (用来定义 name、namespace 和 labels等一些字段)
	Spec              CronJobSpec                 `json:"spec,omitempty"`
	Status            CronJobStatus               `json:"status,omitempty"`
}

/*
this 注释称为标记(marker)。 稍后我们还会看到更多它们，它们提供了一些元数据，
来告诉 controller-tools(我们的代码和 YAML 生成器) 一些额外的信息。
这个注释告诉 object 这是一种 root type Kind。
然后，object 生成器会为我们生成 runtime.Object 接口的实现，
这是所有 Kinds 必须实现的接口。

最后我们只剩下了 CronJob 结构体，如之前说的，我们不需要修改该结构体的任何内容， 但是我们希望操作改 Kind 像操作 Kubernetes 内置资源一样，
所以我们需要增加一个 mark +kubebuilder:subresource:status, 来声明一个status subresource, 关于 subresource 的更多信息可以参考 k8s 文档

*/

// +kubebuilder:object:root=true

// CronJobList contains a list of CronJob
type CronJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CronJob `json:"items"`
}

/*
4. register the kind in a logical mode
we can register  the kind struct type to the API group (API server can recognize it)
registering in the API group means that this  GVK has the logical register location
we can use this GVK through the url-like functions from the API server.
meanwhile, this GVK is bounded to a go type struct (root type object)'
this means we add the GVK to scheme,
the scheme realize the mapping between root type object to the GVK

最后，我们将 Kinds 注册到 API group 中。
这使我们可以将此 API group 中的 Kind 添加到任何 Scheme 中。

*/
func init() {
	SchemeBuilder.Register(&CronJob{}, &CronJobList{})
}

//+kubebuilder:docs-gen:collapse=Root Object Definitions

/*
设计一个 API

1. 在 Kubernetes 中，我们有一些设计 API 的规范。 比如：所有可序列化字段必须是camelCase(驼峰) 格式，
所以我们使用 JSON 的 tags 去指定它们，当一个字段可以为空时，我们会用 omitempty tag 去标记它。

2. 字段类型大多是原始数据类型，Numbers 类型比较特殊： 出于兼容性的考虑，numbers 类型只接受三种数据类型：整形只能使用 int32 和 int64 声明,
小数只能使用 resource.Quantity 声明

ps: Quantity 是十进制数字的一种特殊表示法，具有明确固定的表示形式，
使它们在计算机之间更易于移植。在Kubernetes中指定资源请求和Pod的限制时，您可能已经注意到它们。
从概念上讲，它们的工作方式类似于浮点数：它们具有有效位数，基数和指数。
它们的可序列化和人类可读格式使用整数和后缀来指定值，这与我们描述计算机存储的方式非常相似。

例如，该值2m表示0.002十进制表示法。 2Ki 表示2048十进制，而2K表示2000十进制。
如果要指定分数，请切换到一个specified 后缀，该后缀使我们可以使用整数 to represent the 分数：
e.g., 2.5 can be represented as 2500m。
支持两个基数：10和2（分别称为十进制和二进制）。 十进制基数用“常规” SI后缀（例如 M和K）表示，
而二进制基数用“ mebi”表示法（例如Mi 和Ki）指定。想想megabytes vs mebibytes。
我们还使用了另一种特殊类型：metav1.Time。
它除了格式在 Kubernetes 中比较通用外，功能与 time.Time 完全相同。
现在，我们已经有了一个 API，然后我们需要编写一个 controller 来实现具体功能。

*/
