// Copyright 2018 The prometheus-operator Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package v1

import (
	"reflect"
	"testing"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/kylelemons/godebug/pretty"
)

// TestPrometheusUnstructuredTimestamps ensures that a Prometheus with many
// default values can be converted into an Unstructured which would be valid to
// POST (this is primarily to ensure that creationTimestamp is omitted).
func TestPrometheusUnstructuredTimestamps(t *testing.T) {
	p := &Prometheus{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test",
		},
		Spec: PrometheusSpec{
			Storage: &StorageSpec{
				VolumeClaimTemplate: v1.PersistentVolumeClaim{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test",
					},
					Spec: v1.PersistentVolumeClaimSpec{},
				},
			},
		},
	}

	actual, err := UnstructuredFromPrometheus(p)
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	expected := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"kind":       "Prometheus",
			"apiVersion": "monitoring.coreos.com/v1",
			"metadata": map[string]interface{}{
				"name": "test",
			},
			"spec": map[string]interface{}{
				"resources": map[string]interface{}{},
				"storage": map[string]interface{}{
					"volumeClaimTemplate": map[string]interface{}{
						"metadata": map[string]interface{}{
							"name": "test",
						},
						"spec": map[string]interface{}{
							"dataSource": nil,
							"resources":  map[string]interface{}{},
						},
						"status": map[string]interface{}{},
					},
				},
			},
		},
	}

	if e, a := expected.Object, actual.Object; !reflect.DeepEqual(e, a) {
		t.Fatal(pretty.Compare(e, a))
	}
}
