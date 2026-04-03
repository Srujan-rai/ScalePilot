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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	autoscalingv1alpha1 "github.com/srujan-rai/scalepilot/api/v1alpha1"
	"github.com/srujan-rai/scalepilot/pkg/cloudcost"
)

var _ = Describe("ScalingBudget Controller", func() {
	Context("When reconciling without a CostQuerierFactory", func() {
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

		It("should reconcile and set status using current values", func() {
			controllerReconciler := &ScalingBudgetReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(5 * time.Minute))

			var updated autoscalingv1alpha1.ScalingBudget
			Expect(k8sClient.Get(ctx, typeNamespacedName, &updated)).To(Succeed())
			Expect(updated.Status.LastCheckedAt).NotTo(BeNil())
			Expect(updated.Status.Breached).To(BeFalse())
			Expect(updated.Status.UtilizationPercent).To(Equal(0))

			costCond := findCondition(updated.Status.Conditions, "CostFetched")
			Expect(costCond).NotTo(BeNil())
			Expect(costCond.Status).To(Equal(metav1.ConditionTrue))
		})
	})

	Context("When reconciling with a mock CostQuerierFactory showing breach", func() {
		const resourceName = "test-budget-breach"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := &autoscalingv1alpha1.ScalingBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: autoscalingv1alpha1.ScalingBudgetSpec{
					Namespace:           "staging",
					CeilingMillidollars: 50000,
					CloudCost: autoscalingv1alpha1.CloudCostConfig{
						Provider: autoscalingv1alpha1.CloudProviderAWS,
						CredentialsSecretRef: autoscalingv1alpha1.SecretReference{
							Name:      "aws-creds",
							Namespace: "default",
						},
					},
					BreachAction:            autoscalingv1alpha1.BreachActionBlock,
					WarningThresholdPercent: 80,
					PollIntervalMinutes:     1,
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &autoscalingv1alpha1.ScalingBudget{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should detect breach when spend exceeds ceiling", func() {
			mockFactory := func(_ autoscalingv1alpha1.CloudCostConfig) (cloudcost.CostQuerier, error) {
				return &mockCostQuerier{
					spend: 75000, // $75 > $50 ceiling
				}, nil
			}

			controllerReconciler := &ScalingBudgetReconciler{
				Client:             k8sClient,
				Scheme:             k8sClient.Scheme(),
				CostQuerierFactory: mockFactory,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			var updated autoscalingv1alpha1.ScalingBudget
			Expect(k8sClient.Get(ctx, typeNamespacedName, &updated)).To(Succeed())
			Expect(updated.Status.Breached).To(BeTrue())
			Expect(updated.Status.CurrentSpendMillidollars).To(Equal(int64(75000)))
			Expect(updated.Status.UtilizationPercent).To(Equal(150))

			breachCond := findCondition(updated.Status.Conditions, "Breached")
			Expect(breachCond).NotTo(BeNil())
			Expect(breachCond.Status).To(Equal(metav1.ConditionTrue))
			Expect(breachCond.Reason).To(Equal("BudgetExceeded"))
		})
	})

	Context("When reconciling below warning threshold", func() {
		const resourceName = "test-budget-ok"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := &autoscalingv1alpha1.ScalingBudget{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: autoscalingv1alpha1.ScalingBudgetSpec{
					Namespace:           "dev",
					CeilingMillidollars: 200000,
					CloudCost: autoscalingv1alpha1.CloudCostConfig{
						Provider: autoscalingv1alpha1.CloudProviderGCP,
						CredentialsSecretRef: autoscalingv1alpha1.SecretReference{
							Name:      "gcp-creds",
							Namespace: "default",
						},
					},
					BreachAction:            autoscalingv1alpha1.BreachActionDowngrade,
					WarningThresholdPercent: 80,
					PollIntervalMinutes:     10,
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &autoscalingv1alpha1.ScalingBudget{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should show healthy status when well under budget", func() {
			mockFactory := func(_ autoscalingv1alpha1.CloudCostConfig) (cloudcost.CostQuerier, error) {
				return &mockCostQuerier{
					spend: 50000, // $50 of $200 = 25%
				}, nil
			}

			controllerReconciler := &ScalingBudgetReconciler{
				Client:             k8sClient,
				Scheme:             k8sClient.Scheme(),
				CostQuerierFactory: mockFactory,
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			var updated autoscalingv1alpha1.ScalingBudget
			Expect(k8sClient.Get(ctx, typeNamespacedName, &updated)).To(Succeed())
			Expect(updated.Status.Breached).To(BeFalse())
			Expect(updated.Status.UtilizationPercent).To(Equal(25))

			breachCond := findCondition(updated.Status.Conditions, "Breached")
			Expect(breachCond).NotTo(BeNil())
			Expect(breachCond.Status).To(Equal(metav1.ConditionFalse))
			Expect(breachCond.Reason).To(Equal("WithinBudget"))
		})
	})
})

// mockCostQuerier implements cloudcost.CostQuerier for testing.
type mockCostQuerier struct {
	spend int64
}

func (m *mockCostQuerier) GetCurrentCost(_ context.Context, _ string) (*cloudcost.CostData, error) {
	return &cloudcost.CostData{
		CurrentSpendMillidollars: m.spend,
		FetchedAt:                time.Now(),
		Currency:                 "USD",
	}, nil
}

func (m *mockCostQuerier) Provider() string { return "mock" }
