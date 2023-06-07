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
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/record"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kueue "sigs.k8s.io/kueue/apis/kueue/v1beta1"
	"sigs.k8s.io/kueue/pkg/controller/jobframework"
)

var (
	gvk = corev1.SchemeGroupVersion.WithKind("Pod")

	FrameworkName = "core/pod"
)

func init() {
	utilruntime.Must(jobframework.RegisterIntegration(FrameworkName, jobframework.IntegrationCallbacks{
		SetupIndexes:  SetupIndexes,
		NewReconciler: NewReconciler,
		SetupWebhook:  SetupWebhook,
		JobType:       &corev1.Pod{},
	}))
}

// PodReconciler reconciles a Pod object.
type PodReconciler jobframework.JobReconciler

func NewReconciler(
	scheme *runtime.Scheme,
	client client.Client,
	record record.EventRecorder,
	opts ...jobframework.Option) jobframework.JobReconcilerInterface {
	return (*PodReconciler)(jobframework.NewReconciler(scheme,
		client,
		record,
		opts...,
	))
}

type Pod struct {
	*corev1.Pod
}

var _ jobframework.GenericJob = &Pod{}

func (j *Pod) Object() client.Object {
	return j.Pod
}

func (j *Pod) IsSuspended() bool {
	for _, t := range j.Spec.Tolerations {
		if t.Key == taintKey && t.Value == taintAdmittedVal {
			return false
		}
	}
	return true
}

func (j *Pod) IsActive() bool {
	return j.Status.Phase == corev1.PodRunning
}

const taintKey = "kueue"
const taintAdmittedVal = "admitted"
const taintNotAdmittedVal = "not-admitted"

func (j *Pod) Suspend() {
	for i := range j.Spec.Tolerations {
		if j.Spec.Tolerations[i].Key == taintKey {
			j.Spec.Tolerations[i].Value = taintNotAdmittedVal
		}
	}
}

func (j *Pod) ResetStatus() bool {
	// Reset start time so we can update the scheduling directives later when unsuspending.
	if j.Status.StartTime == nil {
		return false
	}
	j.Status.StartTime = nil
	return true
}

func (j *Pod) GetGVK() schema.GroupVersionKind {
	return gvk
}

func (j *Pod) ReclaimablePods() []kueue.ReclaimablePod {
	return nil
}

func (j *Pod) PodSets() []kueue.PodSet {
	return []kueue.PodSet{
		{
			Name: kueue.DefaultPodSetName,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: *j.ObjectMeta.DeepCopy(),
				Spec:       *j.Spec.DeepCopy(),
			},
			Count: 1,
		},
	}
}

func (j *Pod) RestorePodSetsInfo(podSetsInfo []jobframework.PodSetInfo) {
	// TODO
	return
}

func (j *Pod) run() {
	for i := range j.Spec.Tolerations {
		if j.Spec.Tolerations[i].Key == taintKey {
			j.Spec.Tolerations[i].Value = taintAdmittedVal
			return
		}
	}

	j.Spec.Tolerations = append(j.Spec.Tolerations, corev1.Toleration{
		Key:      taintKey,
		Value:    taintAdmittedVal,
		Operator: corev1.TolerationOpEqual,
		Effect:   corev1.TaintEffectNoSchedule,
	})
}

func (j *Pod) RunWithPodSetsInfo(podSetsInfo []jobframework.PodSetInfo) {
	j.run()
	// TODO
}

func (j *Pod) Finished() (metav1.Condition, bool) {
	condition := metav1.Condition{
		Type:   kueue.WorkloadFinished,
		Status: metav1.ConditionTrue,
		Reason: "JobFinished",
	}

	var finished bool
	switch j.Status.Phase {
	case corev1.PodSucceeded:
		finished = true
		condition.Message = "Pod finished successfully"
	case corev1.PodFailed:
		finished = true
		condition.Message = "Pod failed"
	}

	return condition, finished
}

func (j *Pod) EquivalentToWorkload(wl kueue.Workload) bool {
	if len(wl.Spec.PodSets) != 1 {
		return false
	}

	// nodeSelector may change, hence we are not checking for
	// equality of the whole Spec
	if !equality.Semantic.DeepEqual(j.Spec.InitContainers,
		wl.Spec.PodSets[0].Template.Spec.InitContainers) {
		return false
	}
	return equality.Semantic.DeepEqual(j.Spec.Containers,
		wl.Spec.PodSets[0].Template.Spec.Containers)
}

func (j *Pod) PriorityClass() string {
	return j.Spec.PriorityClassName
}

func (j *Pod) PodsReady() bool {
	for _, c := range j.Status.Conditions {
		if c.Type == corev1.PodReady {
			if c.Status == corev1.ConditionTrue {
				return true
			} else {
				return false
			}
		}
	}
	return false
}

func (j *Pod) podsCount() int32 {
	return 1
}

// SetupWithManager sets up the controller with the Manager. It indexes workloads
// based on the owning jobs.
func (r *PodReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		Owns(&kueue.Workload{}).
		Complete(r)
}

func SetupIndexes(ctx context.Context, indexer client.FieldIndexer) error {
	return jobframework.SetupWorkloadOwnerIndex(ctx, indexer, gvk)
}

//+kubebuilder:rbac:groups=scheduling.k8s.io,resources=priorityclasses,verbs=list;get;watch
//+kubebuilder:rbac:groups="",resources=events,verbs=create;watch;update;patch
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups="",resources=pods/status,verbs=get;update
//+kubebuilder:rbac:groups="",resources=pods/finalizers,verbs=get;update;patch
//+kubebuilder:rbac:groups=kueue.x-k8s.io,resources=workloads,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=kueue.x-k8s.io,resources=workloads/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=kueue.x-k8s.io,resources=workloads/finalizers,verbs=update
//+kubebuilder:rbac:groups=kueue.x-k8s.io,resources=resourceflavors,verbs=get;list;watch

func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	fjr := (*jobframework.JobReconciler)(r)
	return fjr.ReconcileGenericJob(ctx, req, &Pod{&corev1.Pod{}})
}

func GetWorkloadNameForJob(jobName string) string {
	return jobframework.GetWorkloadNameForOwnerWithGVK(jobName, gvk)
}
