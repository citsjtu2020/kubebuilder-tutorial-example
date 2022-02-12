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
	"github.com/robfig/cron"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	validationutils "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// +kubebuilder:docs-gen:collapse=Go imports

// log is for logging in this package.

/*下一步，为我们的 webhooks 创建一个 logger。
 */
var cronjoblog = logf.Log.WithName("cronjob-resource")

/*然后, 我们通过 manager 构建 webhook。
 */

func (r *CronJob) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

/*
注意，下面我们使用 kubebuilder 的 marker 语句
生成 webhook manifests(配置 webhook 的 yaml 文件)，
这个 marker 会生成一个 mutating Webhook 的 manifests。

关于每个参数的解释都可以在这里找到。
*/
// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate-batch-tutorial-kubebuilder-io-v1-cronjob,mutating=true,failurePolicy=fail,groups=batch.tutorial.kubebuilder.io,resources=cronjobs,verbs=create;update,versions=v1,name=mcronjob.kb.io

var _ webhook.Defaulter = &CronJob{}

/*
我们使用 webhook.Defaulter 接口将 webhook 的默认值设置为我们的 CRD，
这将自动生成一个 Webhook(defaulting webhooks)，并调用它的 Default 方法。

Default 方法用来改变接受到的内容，并设置默认值。
Default implements webhook.Defaulter so a webhook will be registered for the type
*/

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *CronJob) Default() {
	cronjoblog.Info("default", "name", r.Name)

	// TODO(user): fill in your defaulting logic.
	if r.Spec.ConcurrencyPolicy == "" {
		r.Spec.ConcurrencyPolicy = AllowConcurrent
	}

	if r.Spec.Suspend == nil {
		r.Spec.Suspend = new(bool)
	}

	if r.Spec.SuccessfulJobHistoryLimit == nil {
		r.Spec.SuccessfulJobHistoryLimit = new(int32)
		*r.Spec.SuccessfulJobHistoryLimit = 3
	}
	if r.Spec.FailedJobsHistoryLimit == nil {
		r.Spec.FailedJobsHistoryLimit = new(int32)
		*r.Spec.FailedJobsHistoryLimit = 1
	}
}

/*
这个 marker 负责生成一个 validating webhook manifest。
*/

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// +kubebuilder:webhook:verbs=create;update,path=/validate-batch-tutorial-kubebuilder-io-v1-cronjob,mutating=false,failurePolicy=fail,groups=batch.tutorial.kubebuilder.io,resources=cronjobs,versions=v1,name=vcronjob.kb.io

var _ webhook.Validator = &CronJob{}

/*
我们可以通过声明式验证(declarative validation)来验证我们的 CRD，
通常情况下，声明式验证就足够了，但是有时更高级的用例需要进行复杂的验证。

例如，我们将在后面看到我们使验证(declarative validation)
来验证 cron schedule(是否是 * * * * * 这种格式) 的格式是否正确，
而不是编写复杂的正则表达式来验证。

如果实现了 webhook.Validator 接口，将会自动生成一个 Webhook(validating webhooks)
来调用我们验证方法。
*/

/*
ValidateCreate、ValidateUpdate 和 ValidateDelete 方法分别在创建，
更新和删除 resrouces 时验证它们收到的信息。
我们将 ValidateCreate 与 ValidateUpdate 方法分开，因为某些字段可能是固定不变的，
他们只能在 ValidateCreate 方法中被调用，这样会提高一些安全性，
ValidateDelete 和 ValidateUpdate 方法也被分开，以便在进行删除操作时进行单独的验证。
但是在这里，我们在 ValidateDelete 中什么也没有做，
只是对 ValidateCreate 和 ValidateUpdate 使用同一个方法进行了验证，
因为我们不需要在删除时验证任何内容
*/

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *CronJob) ValidateCreate() error {
	cronjoblog.Info("validate create", "name", r.Name)

	// TODO(user): fill in your validation logic upon object creation.
	return nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *CronJob) ValidateUpdate(old runtime.Object) error {
	cronjoblog.Info("validate update", "name", r.Name)

	// TODO(user): fill in your validation logic upon object update.
	return nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *CronJob) ValidateDelete() error {
	cronjoblog.Info("validate delete", "name", r.Name)

	// TODO(user): fill in your validation logic upon object deletion.
	return nil
}

/*验证 CronJob 的 name 和 spec 字段
 */

/*
We validate the name and the spec of the CronJob.
*/

func (r *CronJob) validateCronJob() error {
	var allErrs field.ErrorList
	if err := r.validateCronJobName(); err != nil {
		allErrs = append(allErrs, err)
	}
	if err := r.validateCronJobSpec(); err != nil {
		allErrs = append(allErrs, err)
	}
	if len(allErrs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(schema.GroupKind{Group: "batch.tutorial.kubebuilder.io", Kind: "CronJob"}, r.Name, allErrs)
}

/*
Some fields are declaratively validated by OpenAPI schema.
You can find kubebuilder validation markers (prefixed
with `// +kubebuilder:validation`) in the
[Designing an API](api-design.md) section.
You can find all of the kubebuilder supported markers for
declaring validation by running `controller-gen crd -w`,
or [here](/reference/markers/crd-validation.md).
*/

/*
一些字段是用 OpenAPI schema 方式进行验证，
你可以在 设计API 中找到有关 kubebuilder 的验证 markers(注释前缀为//+kubebuilder:validation)。
你也可以通过运行 controller-gen crd -w 或 在这里 找到所有关于使用 markers 验证的格式信息。
*/

func (r *CronJob) validateCronJobSpec() *field.Error {
	// The field helpers from the kubernetes API machinery help us return nicely
	// structured validation errors.
	return validateScheduleFormat(r.Spec.Schedule, field.NewPath("spec").Child("schedule"))
}

/*
We'll need to validate the [cron](https://en.wikipedia.org/wiki/Cron) schedule
is well-formatted.
*/

func validateScheduleFormat(schedule string, fldPath *field.Path) *field.Error {
	if _, err := cron.ParseStandard(schedule); err != nil {
		return field.Invalid(fldPath, schedule, err.Error())
	}
	return nil
}

/*
Validating the length of a string field can be done declaratively by
the validation schema.
But the `ObjectMeta.Name` field is defined in a shared package under
the apimachinery repo, so we can't declaratively validate it using
the validation schema.
*/

func (r *CronJob) validateCronJobName() *field.Error {
	if len(r.ObjectMeta.Name) > validationutils.DNS1035LabelMaxLength-11 {
		// The job name length is 63 character like all Kubernetes objects
		// (which must fit in a DNS subdomain). The cronjob controller appends
		// a 11-character suffix to the cronjob (`-$TIMESTAMP`) when creating
		// a job. The job name length limit is 63 characters. Therefore cronjob
		// names must have length <= 63-11=52. If we don't validate this here,
		// then job creation will fail later.
		return field.Invalid(field.NewPath("metadata").Child("name"), r.Name, "must be no more than 52 characters")
	}
	return nil
}

// +kubebuilder:docs-gen:collapse=Validate object name

/*
如果你想为你的 CRD 实现 admission webhooks，你只需要实现 Defaulter 和 (或) Validator 接口即可。

其余的东西 Kubebuilder 会为你实现，比如：

    1.创建一个 webhook server
    2.确保这个 server 被添加到 manager 中
    3.为你的 webhooks 创建一个 handlers
    4.将每个 handler 以 path 形式注册到你的 server 中
首先，让我们为 CRD（CronJob）创建 webhooks，
我们需要用到 --defaulting 和 --programmatic-validation 参数
(因为我们的测试项目将使用 defaulting webhooks 和 validating webhooks)：
kubebuilder create webhook --group batch --version v1 --kind CronJob --defaulting --programmatic-validation

这将创建 Webhook 功能相关的方法，并在 main.go 中注册 Webhook (server)到你的 manager 中。

*/

/*
Admission Webhooks:
Admission webhooks are HTTP callbacks that receive admission requests,
process them and return admission responses.

Kubernetes provides the following types of admission webhooks:
1. Mutating Admission Webhook:These can mutate the object
while it’s being created or updated, before it gets stored.
It can be used to default fields in a resource requests,
e.g. fields in Deployment that are not specified by the user.
It can be used to inject sidecar containers.

2. Validating Admission Webhook:These can validate the object
while it’s being created or updated, before it gets stored.
It allows more complex validation than pure schema-based validation.
e.g. cross-field validation and pod image whitelisting.

The apiserver by default doesn’t authenticate itself to the webhooks.
However, if you want to authenticate the clients,
you can configure the apiserver to use basic auth, bearer token,
or a cert to authenticate itself to the webhooks.
You can find detailed steps here.
*/

/*
Kubernetes 对 API 访问提供了三种安全访问控制措施：认证、授权和 Admission Control。
认证解决用户是谁的问题，授权解决用户能做什么的问题，
Admission Control 则是资源管理方面的作用。通过合理的权限管理，能够保证系统的安全可靠。

本文主要讲讲Admission中ValidatingAdmissionWebhook和MutatingAdmissionWebhook。
AdmissionWebhook

我们知道k8s在各个方面都具备可扩展性，比如通过cni实现多种网络模型，
通过csi实现多种存储引擎，通过cri实现多种容器运行时等等。
而AdmissionWebhook就是另外一种可扩展的手段。 除了已编译的Admission插件外，
可以开发自己的Admission插件作为扩展，并在运行时配置为webhook。

Admission webhooks是HTTP回调，它接收Admission请求并对它们做一些事情。
可以定义两种类型的Admission webhook，
ValidatingAdmissionWebhook和MutatingAdmissionWebhook。

如果启用了MutatingAdmission，当开始创建一种k8s资源对象的时候，
创建请求会发到你所编写的controller中，然后我们就可以做一系列的操作。
比如我们的场景中，我们会统一做一些功能性增强，当业务开发创建了新的deployment，
我们会执行一些注入的操作，
比如敏感信息aksk，或是一些优化的init脚本,sidecar etc。

而与此类似，只不过ValidatingAdmissionWebhook 是按照你自定义的逻辑是否允许资源的创建。
比如，我们在实际生产k8s集群中，处于稳定性考虑，
我们要求创建的deployment 必须设置request和limit。

总结

最后我们来总结下 webhook Admission 的优势：

webhook 可动态扩展 Admission 能力，满足自定义客户的需求
不需要重启 API Server，可通过创建 webhook configuration 热加载 webhook admission
*/
