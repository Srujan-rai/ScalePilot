package controller

import (
	"context"
	"testing"

	admissionv1 "k8s.io/api/admission/v1"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

func newFakeAdmissionRequest(namespace string, operation admissionv1.Operation) admission.Request {
	return admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			Namespace: namespace,
			Operation: operation,
		},
	}
}

func TestBreachBlockWebhook_isScaleUpBlocked_NoBudgets(t *testing.T) {
	w := &BreachBlockWebhook{}
	blocked, _ := w.isScaleUpBlocked(context.Background(), newFakeAdmissionRequest("", admissionv1.Update))
	if blocked {
		t.Error("should not block when namespace is empty")
	}
}

func TestBreachBlockWebhook_isScaleUpBlocked_NonUpdateOperation(t *testing.T) {
	w := &BreachBlockWebhook{}
	blocked, _ := w.isScaleUpBlocked(context.Background(), newFakeAdmissionRequest("prod", admissionv1.Create))
	if blocked {
		t.Error("should not block CREATE operations")
	}
}
