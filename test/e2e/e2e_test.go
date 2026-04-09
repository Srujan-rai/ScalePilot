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

package e2e

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/srujan-rai/scalepilot/test/utils"
)

const (
	namespace    = "scalepilot-system"
	testNS       = "scalepilot-e2e-test"
	projectimage = "example.com/scalepilot:v0.0.1"
)

var _ = Describe("controller", Ordered, func() {
	BeforeAll(func() {
		By("installing prometheus operator")
		Expect(utils.InstallPrometheusOperator()).To(Succeed())

		By("installing the cert-manager")
		Expect(utils.InstallCertManager()).To(Succeed())

		By("creating manager namespace")
		cmd := exec.Command("kubectl", "create", "ns", namespace)
		_, _ = utils.Run(cmd)

		By("creating test namespace")
		cmd = exec.Command("kubectl", "create", "ns", testNS)
		_, _ = utils.Run(cmd)
	})

	AfterAll(func() {
		By("uninstalling the Prometheus manager bundle")
		utils.UninstallPrometheusOperator()

		By("uninstalling the cert-manager bundle")
		utils.UninstallCertManager()

		By("removing test namespace")
		cmd := exec.Command("kubectl", "delete", "ns", testNS, "--ignore-not-found")
		_, _ = utils.Run(cmd)

		By("removing manager namespace")
		cmd = exec.Command("kubectl", "delete", "ns", namespace)
		_, _ = utils.Run(cmd)
	})

	Context("Operator Deployment", func() {
		It("should run successfully", func() {
			var controllerPodName string
			var err error

			By("building the manager(Operator) image")
			cmd := exec.Command("make", "docker-build", fmt.Sprintf("IMG=%s", projectimage))
			_, err = utils.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())

			By("loading the the manager(Operator) image on Kind")
			err = utils.LoadImageToKindClusterWithName(projectimage)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())

			By("installing CRDs")
			cmd = exec.Command("make", "install")
			_, err = utils.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())

			By("deploying the controller-manager")
			cmd = exec.Command("make", "deploy", fmt.Sprintf("IMG=%s", projectimage))
			_, err = utils.Run(cmd)
			ExpectWithOffset(1, err).NotTo(HaveOccurred())

			By("validating that the controller-manager pod is running as expected")
			verifyControllerUp := func() error {
				cmd = exec.Command("kubectl", "get",
					"pods", "-l", "control-plane=controller-manager",
					"-o", "go-template={{ range .items }}"+
						"{{ if not .metadata.deletionTimestamp }}"+
						"{{ .metadata.name }}"+
						"{{ \"\\n\" }}{{ end }}{{ end }}",
					"-n", namespace,
				)

				podOutput, err := utils.Run(cmd)
				ExpectWithOffset(2, err).NotTo(HaveOccurred())
				podNames := utils.GetNonEmptyLines(string(podOutput))
				if len(podNames) != 1 {
					return fmt.Errorf("expect 1 controller pods running, but got %d", len(podNames))
				}
				controllerPodName = podNames[0]
				ExpectWithOffset(2, controllerPodName).Should(ContainSubstring("controller-manager"))

				cmd = exec.Command("kubectl", "get",
					"pods", controllerPodName, "-o", "jsonpath={.status.phase}",
					"-n", namespace,
				)
				status, err := utils.Run(cmd)
				ExpectWithOffset(2, err).NotTo(HaveOccurred())
				if string(status) != "Running" {
					return fmt.Errorf("controller pod in %s status", status)
				}
				return nil
			}
			EventuallyWithOffset(1, verifyControllerUp, time.Minute, time.Second).Should(Succeed())
		})
	})

	Context("ClusterScaleProfile CRD", func() {
		const profileName = "e2e-test-profile"

		AfterEach(func() {
			cmd := exec.Command("kubectl", "delete", "clusterscaleprofile", profileName, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("should create and reconcile a ClusterScaleProfile", func() {
			manifest := fmt.Sprintf(`
apiVersion: autoscaling.scalepilot.io/v1alpha1
kind: ClusterScaleProfile
metadata:
  name: %s
spec:
  maxSurgePercent: 30
  defaultCooldownSeconds: 90
  enableGlobalDryRun: false
`, profileName)

			By("applying the ClusterScaleProfile manifest")
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = stringReader(manifest)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying the profile is reconciled with status updates")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "clusterscaleprofile", profileName,
					"-o", "jsonpath={.status.lastReconcileTime}")
				output, err := utils.Run(cmd)
				if err != nil {
					return false
				}
				return len(string(output)) > 0
			}, 30*time.Second, 2*time.Second).Should(BeTrue())

			By("verifying blackout is not active")
			cmd = exec.Command("kubectl", "get", "clusterscaleprofile", profileName,
				"-o", "jsonpath={.status.activeBlackout}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(output)).To(Equal("false"))
		})
	})

	Context("ScalingBudget CRD", func() {
		const budgetName = "e2e-test-budget"

		AfterEach(func() {
			cmd := exec.Command("kubectl", "delete", "scalingbudget", budgetName, "-n", testNS, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("should create and reconcile a ScalingBudget", func() {
			manifest := fmt.Sprintf(`
apiVersion: autoscaling.scalepilot.io/v1alpha1
kind: ScalingBudget
metadata:
  name: %s
  namespace: %s
spec:
  namespace: %s
  ceilingMillidollars: 500000
  cloudCost:
    provider: AWS
    credentialsSecretRef:
      name: dummy-aws-creds
      namespace: %s
  breachAction: Delay
  warningThresholdPercent: 80
  pollIntervalMinutes: 5
`, budgetName, testNS, testNS, testNS)

			By("applying the ScalingBudget manifest")
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = stringReader(manifest)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying the budget is reconciled")
			Eventually(func() bool {
				cmd := exec.Command("kubectl", "get", "scalingbudget", budgetName,
					"-n", testNS, "-o", "jsonpath={.status.lastCheckedAt}")
				output, err := utils.Run(cmd)
				if err != nil {
					return false
				}
				return len(string(output)) > 0
			}, 30*time.Second, 2*time.Second).Should(BeTrue())

			By("verifying utilization is 0 without real cloud costs")
			cmd = exec.Command("kubectl", "get", "scalingbudget", budgetName,
				"-n", testNS, "-o", "jsonpath={.status.utilizationPercent}")
			output, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(output)).To(Equal("0"))
		})
	})

	Context("ForecastPolicy CRD", func() {
		const policyName = "e2e-test-forecast"

		AfterEach(func() {
			cmd := exec.Command("kubectl", "delete", "forecastpolicy", policyName, "-n", testNS, "--ignore-not-found")
			_, _ = utils.Run(cmd)
		})

		It("should create a ForecastPolicy and set ModelNotReady condition", func() {
			manifest := fmt.Sprintf(`
apiVersion: autoscaling.scalepilot.io/v1alpha1
kind: ForecastPolicy
metadata:
  name: %s
  namespace: %s
spec:
  targetDeployment:
    name: web-app
  targetHPA:
    name: web-app-hpa
  metricSource:
    address: "http://prometheus:9090"
    query: "rate(http_requests_total[5m])"
    historyDuration: "7d"
  algorithm: ARIMA
  arimaParams:
    p: 2
    d: 1
    q: 1
  leadTimeMinutes: 5
  retrainIntervalMinutes: 30
  targetMetricValuePerReplica: "10"
  dryRun: true
`, policyName, testNS)

			By("applying the ForecastPolicy manifest")
			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = stringReader(manifest)
			_, err := utils.Run(cmd)
			Expect(err).NotTo(HaveOccurred())

			By("verifying the policy sets Error condition to ModelNotReady")
			Eventually(func() string {
				cmd := exec.Command("kubectl", "get", "forecastpolicy", policyName,
					"-n", testNS, "-o", "jsonpath={.status.conditions}")
				output, err := utils.Run(cmd)
				if err != nil {
					return ""
				}

				var conditions []map[string]interface{}
				if err := json.Unmarshal(output, &conditions); err != nil {
					return ""
				}

				for _, c := range conditions {
					if c["type"] == "Error" {
						reason, _ := c["reason"].(string)
						return reason
					}
				}
				return ""
			}, 30*time.Second, 2*time.Second).Should(Equal("ModelNotReady"))
		})
	})

	Context("CRD Validation", func() {
		It("should reject invalid ScalingBudget with warning threshold >= 100", func() {
			manifest := fmt.Sprintf(`
apiVersion: autoscaling.scalepilot.io/v1alpha1
kind: ScalingBudget
metadata:
  name: invalid-budget
  namespace: %s
spec:
  namespace: test
  ceilingMillidollars: 100000
  cloudCost:
    provider: AWS
    credentialsSecretRef:
      name: creds
      namespace: default
  breachAction: Block
  warningThresholdPercent: 101
  pollIntervalMinutes: 5
`, testNS)

			cmd := exec.Command("kubectl", "apply", "-f", "-")
			cmd.Stdin = stringReader(manifest)
			_, err := utils.Run(cmd)
			Expect(err).To(HaveOccurred(), "expected validation to reject warningThresholdPercent > 100")
		})
	})
})

type stringReaderType struct {
	s string
	i int
}

func (r *stringReaderType) Read(p []byte) (n int, err error) {
	if r.i >= len(r.s) {
		return 0, fmt.Errorf("EOF")
	}
	n = copy(p, r.s[r.i:])
	r.i += n
	return n, nil
}

func stringReader(s string) *stringReaderType {
	return &stringReaderType{s: s}
}
