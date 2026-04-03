package controller

import (
	"context"
	"fmt"

	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	autoscalingv1alpha1 "github.com/srujan-rai/scalepilot/api/v1alpha1"
)

// BreachBlockWebhook is a validating admission handler that rejects scale-up
// operations when a ScalingBudget with BreachAction=Block is breached in the
// target namespace. This implements the "Block" enforcement path.
type BreachBlockWebhook struct {
	Client  client.Client
	decoder *admission.Decoder
}

// NewBreachBlockWebhook creates the admission handler and initializes its decoder.
func NewBreachBlockWebhook(c client.Client, scheme *runtime.Scheme) *BreachBlockWebhook {
	return &BreachBlockWebhook{
		Client:  c,
		decoder: admission.NewDecoder(scheme),
	}
}

// Handle processes admission requests for Deployments and HPAs, blocking
// scale-ups in namespaces that have a breached Block-action budget.
func (w *BreachBlockWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	logger := log.FromContext(ctx).WithName("breach-block-webhook")

	blocked, reason := w.isScaleUpBlocked(ctx, req)
	if blocked {
		logger.Info("blocked scale-up due to budget breach",
			"namespace", req.Namespace,
			"resource", fmt.Sprintf("%s/%s", req.Resource.Resource, req.Name),
			"reason", reason)
		return admission.Denied(reason)
	}

	return admission.Allowed("")
}

func (w *BreachBlockWebhook) isScaleUpBlocked(ctx context.Context, req admission.Request) (bool, string) {
	ns := req.Namespace
	if ns == "" {
		return false, ""
	}

	// Only intercept updates (scale changes).
	if req.Operation != admissionv1.Update {
		return false, ""
	}

	// Check if there's an active budget breach with Block action in this namespace.
	var budgets autoscalingv1alpha1.ScalingBudgetList
	if err := w.Client.List(ctx, &budgets, client.InNamespace(ns)); err != nil {
		return false, ""
	}

	var breachedBudget *autoscalingv1alpha1.ScalingBudget
	for i := range budgets.Items {
		b := &budgets.Items[i]
		if b.Spec.BreachAction == autoscalingv1alpha1.BreachActionBlock &&
			b.Status.Breached &&
			b.Spec.Namespace == ns {
			breachedBudget = b
			break
		}
	}

	if breachedBudget == nil {
		return false, ""
	}

	// Check if this is actually a scale-up operation.
	isScaleUp, err := w.detectScaleUp(req)
	if err != nil || !isScaleUp {
		return false, ""
	}

	return true, fmt.Sprintf(
		"scale-up blocked: ScalingBudget %q is breached (spend: $%.2f, ceiling: $%.2f). "+
			"BreachAction is Block — resolve the budget breach before scaling up.",
		breachedBudget.Name,
		float64(breachedBudget.Status.CurrentSpendMillidollars)/1000.0,
		float64(breachedBudget.Spec.CeilingMillidollars)/1000.0,
	)
}

func (w *BreachBlockWebhook) detectScaleUp(req admission.Request) (bool, error) {
	switch req.Resource.Resource {
	case "deployments":
		return w.detectDeploymentScaleUp(req)
	case "horizontalpodautoscalers":
		return w.detectHPAScaleUp(req)
	default:
		return false, nil
	}
}

func (w *BreachBlockWebhook) detectDeploymentScaleUp(req admission.Request) (bool, error) {
	newDeploy := &appsv1.Deployment{}
	if err := w.decoder.Decode(req, newDeploy); err != nil {
		return false, fmt.Errorf("decoding new deployment: %w", err)
	}

	oldDeploy := &appsv1.Deployment{}
	if err := w.decoder.DecodeRaw(req.OldObject, oldDeploy); err != nil {
		return false, fmt.Errorf("decoding old deployment: %w", err)
	}

	oldReplicas := int32(1)
	if oldDeploy.Spec.Replicas != nil {
		oldReplicas = *oldDeploy.Spec.Replicas
	}
	newReplicas := int32(1)
	if newDeploy.Spec.Replicas != nil {
		newReplicas = *newDeploy.Spec.Replicas
	}

	return newReplicas > oldReplicas, nil
}

func (w *BreachBlockWebhook) detectHPAScaleUp(req admission.Request) (bool, error) {
	newHPA := &autoscalingv1.HorizontalPodAutoscaler{}
	if err := w.decoder.Decode(req, newHPA); err != nil {
		return false, fmt.Errorf("decoding new HPA: %w", err)
	}

	oldHPA := &autoscalingv1.HorizontalPodAutoscaler{}
	if err := w.decoder.DecodeRaw(req.OldObject, oldHPA); err != nil {
		return false, fmt.Errorf("decoding old HPA: %w", err)
	}

	oldMin := int32(1)
	if oldHPA.Spec.MinReplicas != nil {
		oldMin = *oldHPA.Spec.MinReplicas
	}
	newMin := int32(1)
	if newHPA.Spec.MinReplicas != nil {
		newMin = *newHPA.Spec.MinReplicas
	}

	// Only treat minReplicas increases as scale-up; maxReplicas changes do not
	// immediately add pods (and raising max is often needed during incidents).
	return newMin > oldMin, nil
}

// InjectDecoder sets up the admission decoder - implements admission.DecoderInjector.
func (w *BreachBlockWebhook) InjectDecoder(d *admission.Decoder) error {
	w.decoder = d
	return nil
}

// Ensure BreachBlockWebhook implements admission.Handler.
var _ admission.Handler = (*BreachBlockWebhook)(nil)
