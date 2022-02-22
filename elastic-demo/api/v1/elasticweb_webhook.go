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
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	validationutils "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

const (
// tomcat容器的端口号
	CONTAINER_PORT = 8080
	// 单个POD的CPU资源申请
	//CPU_REQUEST = "100m"
	// 单个POD的CPU资源上限
	CPU_LIMIT = "100m"
	// 单个POD的内存资源申请
	//MEM_REQUEST = "512Mi"
	// 单个POD的内存资源上限
	MEM_LIMIT = "512Mi"
)

// log is for logging in this package.
var elasticweblog = logf.Log.WithName("elasticweb-resource")

func (r *ElasticWeb) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate-elasticweb-elastic-demo-io-v1-elasticweb,mutating=true,failurePolicy=fail,groups=elasticweb.elastic.demo.io,resources=elasticwebs,verbs=create;update,versions=v1,name=melasticweb.kb.io

var _ webhook.Defaulter = &ElasticWeb{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *ElasticWeb) Default() {
	elasticweblog.Info("default", "name", r.Name)
	if r.Spec.ContainerPort == nil{
		r.Spec.ContainerPort = new(int32)
	}
	*r.Spec.ContainerPort = CONTAINER_PORT

	if IsResourceEmpty(r.Spec.Resource){
		r.Spec.Resource = corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									"cpu":    resource.MustParse(CPU_LIMIT),
									"memory": resource.MustParse(MEM_LIMIT),
								},
							}
	}

	// TODO(user): fill in your defaulting logic.
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// +kubebuilder:webhook:verbs=create;update,path=/validate-elasticweb-elastic-demo-io-v1-elasticweb,mutating=false,failurePolicy=fail,groups=elasticweb.elastic.demo.io,resources=elasticwebs,versions=v1,name=velasticweb.kb.io

var _ webhook.Validator = &ElasticWeb{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *ElasticWeb) ValidateCreate() error {
	elasticweblog.Info("validate create", "name", r.Name)

	// TODO(user): fill in your validation logic upon object creation.
	return r.validateElasticWeb()
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *ElasticWeb) ValidateUpdate(old runtime.Object) error {
	elasticweblog.Info("validate update", "name", r.Name)

	// TODO(user): fill in your validation logic upon object update.
	return r.validateElasticWeb()
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *ElasticWeb) ValidateDelete() error {
	elasticweblog.Info("validate delete", "name", r.Name)

	// TODO(user): fill in your validation logic upon object deletion.
	return nil
}

func (r *ElasticWeb) validateElasticWeb() error {
	var allErrs field.ErrorList
	if err := r.validateCronJobName(); err != nil {
		allErrs = append(allErrs, err)
	}
	//if err := r.validateCronJobSpec(); err != nil {
	//	allErrs = append(allErrs, err)
	//}
	if len(allErrs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(schema.GroupKind{Group: "batch.tutorial.kubebuilder.io", Kind: "CronJob"}, r.Name, allErrs)
}

func (r *ElasticWeb) validateCronJobName() *field.Error {
	if len(r.ObjectMeta.Name) > validationutils.DNS1035LabelMaxLength {
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

