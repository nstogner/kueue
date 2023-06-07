/*
Copyright 2022 The Kubernetes Authors.

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

package job

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"sigs.k8s.io/kueue/pkg/controller/jobframework"
)

type PodWebhook struct {
	manageJobsWithoutQueueName bool
}

// SetupWebhook configures the webhook for Pod.
func SetupWebhook(mgr ctrl.Manager, opts ...jobframework.Option) error {
	options := jobframework.DefaultOptions
	for _, opt := range opts {
		opt(&options)
	}
	wh := &PodWebhook{
		manageJobsWithoutQueueName: options.ManageJobsWithoutQueueName,
	}
	return ctrl.NewWebhookManagedBy(mgr).
		For(&corev1.Pod{}).
		WithDefaulter(wh).
		WithValidator(wh).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-core-v1-pod,mutating=true,failurePolicy=fail,sideEffects=None,groups="",resources=pods,verbs=create,versions=v1,name=mpod.kb.io,admissionReviewVersions=v1

var _ webhook.CustomDefaulter = &PodWebhook{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the type
func (w *PodWebhook) Default(ctx context.Context, obj runtime.Object) error {
	pod := obj.(*corev1.Pod)
	log := ctrl.LoggerFrom(ctx).WithName("pod-webhook")
	log.V(5).Info("Applying defaults", "pod", klog.KObj(pod))

	jobframework.ApplyDefaultForSuspend(&Pod{pod}, w.manageJobsWithoutQueueName)
	return nil
}

// +kubebuilder:webhook:path=/validate-core-v1-pod,mutating=false,failurePolicy=fail,sideEffects=None,groups="",resources=pods,verbs=update,versions=v1,name=vpod.kb.io,admissionReviewVersions=v1

var _ webhook.CustomValidator = &PodWebhook{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type
func (w *PodWebhook) ValidateCreate(ctx context.Context, obj runtime.Object) error {
	pod := obj.(*corev1.Pod)
	log := ctrl.LoggerFrom(ctx).WithName("pod-webhook")
	log.V(5).Info("Validating create", "pod", klog.KObj(pod))
	return validateCreate(&Pod{pod}).ToAggregate()
}

func validateCreate(job jobframework.GenericJob) field.ErrorList {
	var allErrs field.ErrorList
	allErrs = append(allErrs, jobframework.ValidateAnnotationAsCRDName(job, jobframework.ParentWorkloadAnnotation)...)
	allErrs = append(allErrs, jobframework.ValidateCreateForQueueName(job)...)
	return allErrs
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type
func (w *PodWebhook) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) error {
	oldPod := oldObj.(*corev1.Pod)
	newPod := newObj.(*corev1.Pod)
	log := ctrl.LoggerFrom(ctx).WithName("pod-webhook")
	log.V(5).Info("Validating update", "pod", klog.KObj(newPod))
	return validateUpdate(&Pod{oldPod}, &Pod{newPod}).ToAggregate()
}

func validateUpdate(oldJob, newJob jobframework.GenericJob) field.ErrorList {
	allErrs := validateCreate(newJob)
	allErrs = append(allErrs, jobframework.ValidateUpdateForParentWorkload(oldJob, newJob)...)
	allErrs = append(allErrs, jobframework.ValidateUpdateForOriginalNodeSelectors(oldJob, newJob)...)
	allErrs = append(allErrs, jobframework.ValidateUpdateForQueueName(oldJob, newJob)...)
	return allErrs
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type
func (w *PodWebhook) ValidateDelete(ctx context.Context, obj runtime.Object) error {
	return nil
}
