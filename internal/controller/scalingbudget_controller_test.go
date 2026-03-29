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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	autoscalingv1alpha1 "github.com/srujan-rai/scalepilot/api/v1alpha1"
)

var _ = Describe("ScalingBudget Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-budget"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		scalingbudget := &autoscalingv1alpha1.ScalingBudget{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind ScalingBudget")
			err := k8sClient.Get(ctx, typeNamespacedName, scalingbudget)
			if err != nil && errors.IsNotFound(err) {
				resource := &autoscalingv1alpha1.ScalingBudget{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: autoscalingv1alpha1.ScalingBudgetSpec{
						Namespace:           "production",
						CeilingMillidollars: 100000,
						CloudCost: autoscalingv1alpha1.CloudCostConfig{
							Provider: autoscalingv1alpha1.CloudProviderAWS,
							CredentialsSecretRef: autoscalingv1alpha1.SecretReference{
								Name:      "aws-creds",
								Namespace: "default",
							},
						},
						BreachAction:            autoscalingv1alpha1.BreachActionDelay,
						WarningThresholdPercent: 80,
						PollIntervalMinutes:     5,
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &autoscalingv1alpha1.ScalingBudget{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance ScalingBudget")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &ScalingBudgetReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			var updated autoscalingv1alpha1.ScalingBudget
			Expect(k8sClient.Get(ctx, typeNamespacedName, &updated)).To(Succeed())
			Expect(updated.Status.LastCheckedAt).NotTo(BeNil())
		})
	})
})
