/*
Copyright 2026.

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

package controller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	autoscalingv1alpha1 "github.com/srujan-rai/scalepilot/api/v1alpha1"
)

var _ = Describe("ClusterScaleProfile Controller", func() {
	Context("When reconciling a basic profile", func() {
		const resourceName = "test-profile"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name: resourceName,
		}
		clusterscaleprofile := &autoscalingv1alpha1.ClusterScaleProfile{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind ClusterScaleProfile")
			err := k8sClient.Get(ctx, typeNamespacedName, clusterscaleprofile)
			if err != nil && errors.IsNotFound(err) {
				resource := &autoscalingv1alpha1.ClusterScaleProfile{
					ObjectMeta: metav1.ObjectMeta{
						Name: resourceName,
					},
					Spec: autoscalingv1alpha1.ClusterScaleProfileSpec{
						MaxSurgePercent:        25,
						DefaultCooldownSeconds: 60,
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &autoscalingv1alpha1.ClusterScaleProfile{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance ClusterScaleProfile")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should set status fields correctly with no blackout windows", func() {
			controllerReconciler := &ClusterScaleProfileReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(30 * time.Second))

			var updated autoscalingv1alpha1.ClusterScaleProfile
			Expect(k8sClient.Get(ctx, typeNamespacedName, &updated)).To(Succeed())

			Expect(updated.Status.ActiveBlackout).To(BeFalse())
			Expect(updated.Status.ActiveBlackoutName).To(BeEmpty())
			Expect(updated.Status.TeamsConfigured).To(Equal(int32(0)))
			Expect(updated.Status.LastReconcileTime).NotTo(BeNil())
			Expect(updated.Status.Conditions).NotTo(BeEmpty())

			readyCond := findCondition(updated.Status.Conditions, "Ready")
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(readyCond.Reason).To(Equal("Reconciled"))
		})
	})

	Context("When reconciling a profile with team overrides", func() {
		const resourceName = "test-profile-teams"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name: resourceName,
		}

		BeforeEach(func() {
			resource := &autoscalingv1alpha1.ClusterScaleProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name: resourceName,
				},
				Spec: autoscalingv1alpha1.ClusterScaleProfileSpec{
					MaxSurgePercent:        25,
					DefaultCooldownSeconds: 60,
					TeamOverrides: []autoscalingv1alpha1.TeamOverride{
						{TeamName: "platform", Namespaces: []string{"platform-ns"}},
						{TeamName: "data-eng", Namespaces: []string{"data-ns", "batch-ns"}},
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &autoscalingv1alpha1.ClusterScaleProfile{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should count teams correctly", func() {
			controllerReconciler := &ClusterScaleProfileReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			var updated autoscalingv1alpha1.ClusterScaleProfile
			Expect(k8sClient.Get(ctx, typeNamespacedName, &updated)).To(Succeed())
			Expect(updated.Status.TeamsConfigured).To(Equal(int32(2)))
		})
	})

	Context("When reconciling a profile with global dry-run", func() {
		const resourceName = "test-profile-dryrun"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name: resourceName,
		}

		BeforeEach(func() {
			resource := &autoscalingv1alpha1.ClusterScaleProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name: resourceName,
				},
				Spec: autoscalingv1alpha1.ClusterScaleProfileSpec{
					MaxSurgePercent:        25,
					DefaultCooldownSeconds: 60,
					EnableGlobalDryRun:     true,
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &autoscalingv1alpha1.ClusterScaleProfile{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should reconcile successfully with dry-run enabled", func() {
			controllerReconciler := &ClusterScaleProfileReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			var updated autoscalingv1alpha1.ClusterScaleProfile
			Expect(k8sClient.Get(ctx, typeNamespacedName, &updated)).To(Succeed())
			Expect(updated.Status.ActiveBlackout).To(BeFalse())
		})
	})
})

func findCondition(conditions []metav1.Condition, condType string) *metav1.Condition {
	for i := range conditions {
		if conditions[i].Type == condType {
			return &conditions[i]
		}
	}
	return nil
}
