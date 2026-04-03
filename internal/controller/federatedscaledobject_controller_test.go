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

func int32Ptr(v int32) *int32 { return &v }

var _ = Describe("FederatedScaledObject Controller", func() {
	Context("When reconciling without a metric querier", func() {
		const resourceName = "test-federation"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		federatedscaledobject := &autoscalingv1alpha1.FederatedScaledObject{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind FederatedScaledObject")
			err := k8sClient.Get(ctx, typeNamespacedName, federatedscaledobject)
			if err != nil && errors.IsNotFound(err) {
				resource := &autoscalingv1alpha1.FederatedScaledObject{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: autoscalingv1alpha1.FederatedScaledObjectSpec{
						PrimaryCluster: autoscalingv1alpha1.ClusterRef{
							Name: "primary",
							SecretRef: autoscalingv1alpha1.SecretReference{
								Name:      "primary-kubeconfig",
								Namespace: "default",
							},
						},
						OverflowClusters: []autoscalingv1alpha1.ClusterRef{
							{
								Name: "overflow-1",
								SecretRef: autoscalingv1alpha1.SecretReference{
									Name:      "overflow-kubeconfig",
									Namespace: "default",
								},
								MaxCapacity: int32Ptr(10),
								Priority:    1,
							},
						},
						Metric: autoscalingv1alpha1.SpilloverMetric{
							Query:             "kube_deployment_status_replicas",
							PrometheusAddress: "http://prometheus:9090",
							ThresholdValue:    "50",
						},
						Workload: autoscalingv1alpha1.WorkloadTemplate{
							DeploymentName: "worker",
							Namespace:      "default",
						},
						CooldownSeconds: 60,
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &autoscalingv1alpha1.FederatedScaledObject{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance FederatedScaledObject")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should reconcile with metric=0, no spillover triggered", func() {
			controllerReconciler := &FederatedScaledObjectReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			result, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result.RequeueAfter).To(Equal(30 * time.Second))

			var updated autoscalingv1alpha1.FederatedScaledObject
			Expect(k8sClient.Get(ctx, typeNamespacedName, &updated)).To(Succeed())

			Expect(updated.Status.SpilloverActive).To(BeFalse())
			Expect(updated.Status.CurrentMetricValue).To(Equal("0.00"))
			Expect(updated.Status.TotalReplicas).To(Equal(int32(0)))

			readyCond := findCondition(updated.Status.Conditions, "Ready")
			Expect(readyCond).NotTo(BeNil())
			Expect(readyCond.Status).To(Equal(metav1.ConditionTrue))
		})
	})

	Context("When reconciling with multiple overflow clusters", func() {
		const resourceName = "test-federation-multi"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			resource := &autoscalingv1alpha1.FederatedScaledObject{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: autoscalingv1alpha1.FederatedScaledObjectSpec{
					PrimaryCluster: autoscalingv1alpha1.ClusterRef{
						Name: "primary-cluster",
						SecretRef: autoscalingv1alpha1.SecretReference{
							Name:      "primary-kubeconfig",
							Namespace: "default",
						},
					},
					OverflowClusters: []autoscalingv1alpha1.ClusterRef{
						{
							Name: "overflow-us-east",
							SecretRef: autoscalingv1alpha1.SecretReference{
								Name:      "us-east-kubeconfig",
								Namespace: "default",
							},
							MaxCapacity: int32Ptr(20),
							Priority:    1,
						},
						{
							Name: "overflow-eu-west",
							SecretRef: autoscalingv1alpha1.SecretReference{
								Name:      "eu-west-kubeconfig",
								Namespace: "default",
							},
							MaxCapacity: int32Ptr(15),
							Priority:    2,
						},
					},
					Metric: autoscalingv1alpha1.SpilloverMetric{
						Query:             "queue_depth",
						PrometheusAddress: "http://prometheus:9090",
						ThresholdValue:    "100",
					},
					Workload: autoscalingv1alpha1.WorkloadTemplate{
						DeploymentName: "processor",
						Namespace:      "default",
					},
					CooldownSeconds:  120,
					MaxTotalReplicas: int32Ptr(50),
				},
			}
			Expect(k8sClient.Create(ctx, resource)).To(Succeed())
		})

		AfterEach(func() {
			resource := &autoscalingv1alpha1.FederatedScaledObject{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			if err == nil {
				Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
			}
		})

		It("should reconcile and track overflow cluster statuses", func() {
			controllerReconciler := &FederatedScaledObjectReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			var updated autoscalingv1alpha1.FederatedScaledObject
			Expect(k8sClient.Get(ctx, typeNamespacedName, &updated)).To(Succeed())
			Expect(updated.Status.OverflowClusters).To(HaveLen(2))
			Expect(updated.Status.OverflowClusters[0].Name).To(Equal("overflow-us-east"))
			Expect(updated.Status.OverflowClusters[1].Name).To(Equal("overflow-eu-west"))
			Expect(updated.Status.SpilloverActive).To(BeFalse())
		})
	})
})
