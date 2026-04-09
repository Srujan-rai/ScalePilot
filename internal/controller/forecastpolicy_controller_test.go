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
)

var _ = Describe("ForecastPolicy Controller", func() {
	Context("When reconciling without a trained model", func() {
		const resourceName = "test-forecast"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		forecastpolicy := &autoscalingv1alpha1.ForecastPolicy{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind ForecastPolicy")
			err := k8sClient.Get(ctx, typeNamespacedName, forecastpolicy)
			if err != nil && errors.IsNotFound(err) {
				resource := &autoscalingv1alpha1.ForecastPolicy{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: autoscalingv1alpha1.ForecastPolicySpec{
						TargetDeployment: autoscalingv1alpha1.TargetDeploymentRef{Name: "web-app"},
						TargetHPA:        autoscalingv1alpha1.TargetHPARef{Name: "web-app-hpa"},
						MetricSource: autoscalingv1alpha1.PrometheusMetricSource{
							Address:         "http://prometheus:9090",
							Query:           "rate(http_requests_total[5m])",
							HistoryDuration: "7d",
						},
						Algorithm:              autoscalingv1alpha1.ForecastAlgorithmARIMA,
						LeadTimeMinutes:        5,
						RetrainIntervalMinutes: 30,
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &autoscalingv1alpha1.ForecastPolicy{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance ForecastPolicy")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should set ModelNotReady condition when no ConfigMap exists", func() {
			controllerReconciler := &ForecastPolicyReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(30 * time.Second))

			Eventually(func(g Gomega) {
				var updated autoscalingv1alpha1.ForecastPolicy
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, &updated)).To(Succeed())
				errCond := findCondition(updated.Status.Conditions, string(autoscalingv1alpha1.ForecastConditionError))
				g.Expect(errCond).NotTo(BeNil())
				g.Expect(errCond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(errCond.Reason).To(Or(
					Equal("ModelNotReady"),
					Equal(reasonTrainingFailed),
				))
			}).WithTimeout(5 * time.Second).WithPolling(100 * time.Millisecond).Should(Succeed())
		})
	})

	Context("When reconciling a dry-run ForecastPolicy", func() {
		const resourceName = "test-forecast-dryrun"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := &autoscalingv1alpha1.ForecastPolicy{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: autoscalingv1alpha1.ForecastPolicySpec{
					TargetDeployment: autoscalingv1alpha1.TargetDeploymentRef{Name: "web-dry"},
					TargetHPA:        autoscalingv1alpha1.TargetHPARef{Name: "web-dry-hpa"},
					MetricSource: autoscalingv1alpha1.PrometheusMetricSource{
						Address:         "http://prometheus:9090",
						Query:           "rate(http_requests_total[5m])",
						HistoryDuration: "7d",
					},
					Algorithm:              autoscalingv1alpha1.ForecastAlgorithmHoltWinters,
					LeadTimeMinutes:        5,
					RetrainIntervalMinutes: 30,
					DryRun:                 true,
					HoltWintersParams: &autoscalingv1alpha1.HoltWintersParams{
						Alpha:           "0.3",
						Beta:            "0.1",
						Gamma:           "0.2",
						SeasonalPeriods: 24,
					},
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &autoscalingv1alpha1.ForecastPolicy{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should not error on reconcile and surface an error until a model exists", func() {
			controllerReconciler := &ForecastPolicyReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			// Async training runs without MetricQuerierFactory and reports TrainingFailed;
			// or the reconcile path may briefly set ModelNotReady first.
			Eventually(func(g Gomega) {
				var updated autoscalingv1alpha1.ForecastPolicy
				g.Expect(k8sClient.Get(ctx, typeNamespacedName, &updated)).To(Succeed())
				errCond := findCondition(updated.Status.Conditions, string(autoscalingv1alpha1.ForecastConditionError))
				g.Expect(errCond).NotTo(BeNil())
				g.Expect(errCond.Status).To(Equal(metav1.ConditionTrue))
				g.Expect(errCond.Reason).To(Or(
					Equal("ModelNotReady"),
					Equal(reasonTrainingFailed),
				))
			}).WithTimeout(5 * time.Second).WithPolling(100 * time.Millisecond).Should(Succeed())
		})
	})
})
