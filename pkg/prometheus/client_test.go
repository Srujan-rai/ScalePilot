package prometheus

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"context"

	"github.com/prometheus/common/model"
)

func TestNewClient_EmptyAddress(t *testing.T) {
	_, err := NewClient("")
	if err == nil {
		t.Error("expected error for empty address")
	}
}

func TestNewClient_ValidAddress(t *testing.T) {
	q, err := NewClient("http://prometheus:9090")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if q == nil {
		t.Error("expected non-nil querier")
	}
}

func TestParseMatrix_ValidMatrix(t *testing.T) {
	now := model.TimeFromUnix(time.Now().Unix())
	matrix := model.Matrix{
		&model.SampleStream{
			Metric: model.Metric{"__name__": "test"},
			Values: []model.SamplePair{
				{Timestamp: now, Value: 10.5},
				{Timestamp: now + 60000, Value: 20.3},
			},
		},
	}

	points, err := parseMatrix(matrix)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(points) != 2 {
		t.Fatalf("expected 2 points, got %d", len(points))
	}
	if points[0].Value != 10.5 {
		t.Errorf("expected first value 10.5, got %f", points[0].Value)
	}
	if points[1].Value != 20.3 {
		t.Errorf("expected second value 20.3, got %f", points[1].Value)
	}
}

func TestParseMatrix_MultiSeries(t *testing.T) {
	now := model.TimeFromUnix(time.Now().Unix())
	matrix := model.Matrix{
		&model.SampleStream{
			Metric: model.Metric{"instance": "a"},
			Values: []model.SamplePair{
				{Timestamp: now, Value: 1.0},
			},
		},
		&model.SampleStream{
			Metric: model.Metric{"instance": "b"},
			Values: []model.SamplePair{
				{Timestamp: now, Value: 2.0},
				{Timestamp: now + 60000, Value: 3.0},
			},
		},
	}

	points, err := parseMatrix(matrix)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(points) != 3 {
		t.Fatalf("expected 3 points (flattened), got %d", len(points))
	}
}

func TestParseMatrix_EmptyMatrix(t *testing.T) {
	matrix := model.Matrix{}
	_, err := parseMatrix(matrix)
	if err == nil {
		t.Error("expected error for empty matrix")
	}
}

func TestParseMatrix_WrongType(t *testing.T) {
	scalar := &model.Scalar{Value: 42, Timestamp: model.TimeFromUnix(time.Now().Unix())}
	_, err := parseMatrix(scalar)
	if err == nil {
		t.Error("expected error for non-matrix type")
	}
}

func TestParseScalar_Scalar(t *testing.T) {
	scalar := &model.Scalar{Value: 42.5, Timestamp: model.TimeFromUnix(time.Now().Unix())}
	val, err := parseScalar(scalar)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 42.5 {
		t.Errorf("expected 42.5, got %f", val)
	}
}

func TestParseScalar_Vector(t *testing.T) {
	vec := model.Vector{
		&model.Sample{
			Metric:    model.Metric{"__name__": "test"},
			Value:     99.9,
			Timestamp: model.TimeFromUnix(time.Now().Unix()),
		},
	}
	val, err := parseScalar(vec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != 99.9 {
		t.Errorf("expected 99.9, got %f", val)
	}
}

func TestParseScalar_EmptyVector(t *testing.T) {
	vec := model.Vector{}
	_, err := parseScalar(vec)
	if err == nil {
		t.Error("expected error for empty vector")
	}
}

func TestParseScalar_UnsupportedType(t *testing.T) {
	matrix := model.Matrix{}
	_, err := parseScalar(matrix)
	if err == nil {
		t.Error("expected error for unsupported type")
	}
}

func TestClient_RangeQuery_Integration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query_range" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		resp := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"resultType": "matrix",
				"result": []map[string]interface{}{
					{
						"metric": map[string]string{"__name__": "cpu"},
						"values": [][]interface{}{
							{float64(time.Now().Unix()), "5.5"},
							{float64(time.Now().Unix() + 60), "6.5"},
						},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	q, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	now := time.Now()
	result, err := q.RangeQuery(context.Background(), "cpu_usage", now.Add(-time.Hour), now, time.Minute)
	if err != nil {
		t.Fatalf("RangeQuery error: %v", err)
	}

	if len(result.DataPoints) != 2 {
		t.Errorf("expected 2 data points, got %d", len(result.DataPoints))
	}
	if result.Query != "cpu_usage" {
		t.Errorf("expected query 'cpu_usage', got %q", result.Query)
	}
}

func TestClient_InstantQuery_Integration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/query" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}

		resp := map[string]interface{}{
			"status": "success",
			"data": map[string]interface{}{
				"resultType": "vector",
				"result": []map[string]interface{}{
					{
						"metric": map[string]string{"__name__": "queue_depth"},
						"value":  []interface{}{float64(time.Now().Unix()), "42.5"},
					},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	q, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	val, err := q.InstantQuery(context.Background(), "queue_depth")
	if err != nil {
		t.Fatalf("InstantQuery error: %v", err)
	}

	if val != 42.5 {
		t.Errorf("expected 42.5, got %f", val)
	}
}

func TestClient_InstantQuery_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprint(w, "internal error")
	}))
	defer server.Close()

	q, err := NewClient(server.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = q.InstantQuery(context.Background(), "up")
	if err == nil {
		t.Error("expected error for server error")
	}
}
