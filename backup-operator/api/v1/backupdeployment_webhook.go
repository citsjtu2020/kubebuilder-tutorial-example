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
	"errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	validationutils "k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// log is for logging in this package.
var backupdeploymentlog = logf.Log.WithName("backupdeployment-resource")

func (r *BackupDeployment) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate-elasticscale-com-sjtu-cit-com-sjtu-cit-v1-backupdeployment,mutating=true,failurePolicy=fail,groups=elasticscale.com.sjtu.cit.com.sjtu.cit,resources=backupdeployments,verbs=create;update,versions=v1,name=mbackupdeployment.kb.io

var _ webhook.Defaulter = &BackupDeployment{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *BackupDeployment) Default() {
	backupdeploymentlog.Info("default", "name", r.Name)

	// TODO(user): fill in your defaulting logic.
	if r.Spec.AllocateStrategy == nil {
		tmpallocate := int64(BinPacking)
		r.Spec.AllocateStrategy = new(int64)
		*r.Spec.AllocateStrategy = tmpallocate
	}

	if r.Spec.ScheduleStrategy == nil {
		tmpschedule := int64(ScheduleOnSameHosts)
		r.Spec.ScheduleStrategy = new(int64)
		*r.Spec.ScheduleStrategy = tmpschedule
	}

	if r.Spec.UnitReplicas == nil {
		tmpreplicas := int64(0)
		r.Spec.ScheduleStrategy = new(int64)
		*r.Spec.UnitReplicas = tmpreplicas
	}
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// +kubebuilder:webhook:verbs=create;update,path=/validate-elasticscale-com-sjtu-cit-com-sjtu-cit-v1-backupdeployment,mutating=false,failurePolicy=fail,groups=elasticscale.com.sjtu.cit.com.sjtu.cit,resources=backupdeployments,versions=v1,name=vbackupdeployment.kb.io

var _ webhook.Validator = &BackupDeployment{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *BackupDeployment) ValidateCreate() error {
	backupdeploymentlog.Info("validate create", "name", r.Name)

	// TODO(user): fill in your validation logic upon object creation.
	//nil
	return r.validateBackupDeployment()
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *BackupDeployment) ValidateUpdate(old runtime.Object) error {
	backupdeploymentlog.Info("validate update", "name", r.Name)

	// TODO(user): fill in your validation logic upon object update.
	// nil
	return r.validateBackupDeployment()
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *BackupDeployment) ValidateDelete() error {
	backupdeploymentlog.Info("validate delete", "name", r.Name)

	// TODO(user): fill in your validation logic upon object deletion.
	// nil
	// nil
	return nil
}

func (r *BackupDeployment) validateBackupDeploymentName() *field.Error {
	if len(r.ObjectMeta.Name) > validationutils.DNS1035LabelMaxLength-10 {
		// The job name length is 63 character like all Kubernetes objects
		// (which must fit in a DNS subdomain). The cronjob controller appends
		// a 11-character suffix to the cronjob (`-$TIMESTAMP`) when creating
		// a job. The job name length limit is 63 characters. Therefore cronjob
		// names must have length <= 63-11=52. If we don't validate this here,
		// then job creation will fail later.
		return field.Invalid(field.NewPath("metadata").Child("name"), r.Name, "must be no more than 53 characters")
	}
	return nil
}

func (r *BackupDeployment) validateBackupDeployment() error {
	var allErrs field.ErrorList
	if err := r.validateBackupDeploymentName(); err != nil {
		allErrs = append(allErrs, err)
	}
	//if err := r.validateCronJobSpec(); err != nil {
	//	allErrs = append(allErrs, err)
	//}
	specerr := r.validateBackupDeploymentSpec()
	if specerr != nil && len(specerr) > 0 {
		for _, err0 := range specerr {
			allErrs = append(allErrs, err0)
		}
	}

	if len(allErrs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(schema.GroupKind{Group: "elasticscale.com.sjtu.cit", Kind: "BackupDeployment"}, r.Name, allErrs)
}

func (r *BackupDeployment) validateBackupDeploymentSpec() field.ErrorList {
	// The field helpers from the kubernetes API machinery help us return nicely
	// structured validation errors.
	//return validateScheduleStrtegyFormat(
	//	r.Spec.ScheduleStrategy,
	//	field.NewPath("spec").Child("schedule"))
	var allErrs field.ErrorList
	//var allerr error = nil
	if err := validateScheduleStrtegyFormat(r.Spec.ScheduleStrategy, field.NewPath("spec").Child("scheduleStrategy")); err != nil {
		allErrs = append(allErrs, err)
	}
	if err := validateAllocateStrtegyFormat(r.Spec.AllocateStrategy, field.NewPath("spec").Child("allocateStrategy")); err != nil {
		allErrs = append(allErrs, err)
	}
	if len(allErrs) == 0 {
		return nil
	}

	return allErrs
	//allerr := allErrs.ToAggregate()
	//if allerr != nil && len(a)
	//apierrors.NewInvalid()
	//return apierrors.NewInvalid(schema.GroupKind{Group: "elasticscale.com.sjtu.cit", Kind: "BackupDeployment"}, r.Name, allErrs)
}

/*
We'll need to validate the [cron](https://en.wikipedia.org/wiki/Cron) schedule
is well-formatted.
*/

func validateScheduleStrtegyFormat(strategy *int64, fldPath *field.Path) *field.Error {
	var err error = nil
	if strategy == nil {
		return nil
	} else {
		tmpstrategy := ScheduleStrategy(int(*strategy))
		switch tmpstrategy {
		case ScheduleOnSameHosts:
			err = nil
			//return nil
		case ScheduleOnSameHostsHard:
			err = nil
			//return nil
		case ScheduleRoundBin:
			err = nil
			//return nil
		case ScheduleRoundBinHard:
			err = nil
			//return nil
		default:
			err = errors.New("Invalid ScheduleStrategy")
		}
	}
	if err != nil {
		return field.Invalid(fldPath, *strategy, err.Error())
	}
	return nil
}

func validateAllocateStrtegyFormat(strategy *int64, fldPath *field.Path) *field.Error {
	var err error = nil
	if strategy == nil {
		return nil
	} else {
		tmpstrategy := AllocateStrategy(int(*strategy))
		switch tmpstrategy {
		case BinPacking:
			err = nil
			//return nil
		case LeastUsage:
			err = nil
		default:
			err = errors.New("Invalid ScheduleStrategy")
		}
	}
	if err != nil {
		return field.Invalid(fldPath, *strategy, err.Error())
	}
	return nil
}
