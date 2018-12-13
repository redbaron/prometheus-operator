// Copyright 2016 The prometheus-operator Authors
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

package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/coreos/prometheus-operator/pkg/alertmanager"
	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/coreos/prometheus-operator/pkg/prometheus"
	testFramework "github.com/coreos/prometheus-operator/test/framework"

	"github.com/golang/protobuf/proto"
	"github.com/kylelemons/godebug/pretty"
	"github.com/pkg/errors"
)

func testPromCreateDeleteCluster(t *testing.T, ns testFramework.Namespaces) {
	t.Parallel()

	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup(t)
	ctx.SetupPrometheusRBAC(t, ns, framework.KubeClient)

	name := "test"

	prometheusCRD := framework.MakeBasicPrometheus(ns, name, name, 1)

	if _, err := framework.CreatePrometheusAndWaitUntilReady(prometheusCRD); err != nil {
		t.Fatal(err)
	}

	if err := framework.DeletePrometheusAndWaitUntilGone(prometheusCRD); err != nil {
		t.Fatal(err)
	}
}

func testPromScaleUpDownCluster(t *testing.T, ns testFramework.Namespaces) {
	t.Parallel()

	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup(t)
	ctx.SetupPrometheusRBAC(t, ns, framework.KubeClient)

	name := "test"

	p, err := framework.CreatePrometheusAndWaitUntilReady(framework.MakeBasicPrometheus(ns, name, name, 1))
	if err != nil {
		t.Fatal(err)
	}

	p.Spec.Replicas = proto.Int32(3)
	p, err = framework.UpdatePrometheusAndWaitUntilReady(p)
	if err != nil {
		t.Fatal(err)
	}

	p.Spec.Replicas = proto.Int32(2)
	p, err = framework.UpdatePrometheusAndWaitUntilReady(p)
	if err != nil {
		t.Fatal(err)
	}
}

func testPromNoServiceMonitorSelector(t *testing.T, ns testFramework.Namespaces) {
	t.Parallel()

	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup(t)
	ctx.SetupPrometheusRBAC(t, ns, framework.KubeClient)

	name := "test"
	p := framework.MakeBasicPrometheus(ns, name, name, 1)
	p.Spec.ServiceMonitorSelector = nil
	if _, err := framework.CreatePrometheusAndWaitUntilReady(p); err != nil {
		t.Fatal(err)
	}
}

func testPromVersionMigration(t *testing.T, ns testFramework.Namespaces) {
	t.Parallel()

	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup(t)
	ctx.SetupPrometheusRBAC(t, ns, framework.KubeClient)

	name := "test"
	startVersion := prometheus.CompatibilityMatrix[0]
	compatibilityMatrix := prometheus.CompatibilityMatrix[1:]

	p := framework.MakeBasicPrometheus(ns, name, name, 1)
	p.Spec.Version = startVersion
	p, err := framework.CreatePrometheusAndWaitUntilReady(p)
	if err != nil {
		t.Fatal(err)
	}

	for _, v := range compatibilityMatrix {
		p.Spec.Version = v
		p, err = framework.UpdatePrometheusAndWaitUntilReady(p)
		if err != nil {
			t.Fatal(err)
		}
		if err := framework.WaitForPrometheusRunImageAndReady(p); err != nil {
			t.Fatal(err)
		}
	}
}

func testPromResourceUpdate(t *testing.T, ns testFramework.Namespaces) {
	t.Parallel()

	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup(t)
	ctx.SetupPrometheusRBAC(t, ns, framework.KubeClient)

	name := "test"

	p := framework.MakeBasicPrometheus(ns, name, name, 1)

	p.Spec.Resources = v1.ResourceRequirements{
		Requests: v1.ResourceList{
			v1.ResourceMemory: resource.MustParse("100Mi"),
		},
	}
	p, err := framework.CreatePrometheusAndWaitUntilReady(p)
	if err != nil {
		t.Fatal(err)
	}

	pods, err := framework.KubeClient.Core().Pods(p.Namespace).List(prometheus.ListOptions(p.Name))
	if err != nil {
		t.Fatal(err)
	}
	res := pods.Items[0].Spec.Containers[0].Resources

	if !reflect.DeepEqual(res, p.Spec.Resources) {
		t.Fatalf("resources don't match. Has %#+v, want %#+v", res, p.Spec.Resources)
	}

	p.Spec.Resources = v1.ResourceRequirements{
		Requests: v1.ResourceList{
			v1.ResourceMemory: resource.MustParse("200Mi"),
		},
	}
	p, err = framework.MonClientV1.Prometheuses(p.Namespace).Update(p)
	if err != nil {
		t.Fatal(err)
	}

	err = wait.Poll(5*time.Second, 2*time.Minute, func() (bool, error) {
		pods, err := framework.KubeClient.Core().Pods(p.Namespace).List(prometheus.ListOptions(p.Name))
		if err != nil {
			return false, err
		}

		if len(pods.Items) != 1 {
			return false, nil
		}

		res = pods.Items[0].Spec.Containers[0].Resources
		if !reflect.DeepEqual(res, p.Spec.Resources) {
			return false, nil
		}

		return true, nil
	})

	if err != nil {
		t.Fatal(err)
	}
}

func testPromStorageUpdate(t *testing.T, ns testFramework.Namespaces) {
	t.Parallel()

	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup(t)
	ctx.SetupPrometheusRBAC(t, ns, framework.KubeClient)

	name := "test"

	p := framework.MakeBasicPrometheus(ns, name, name, 1)

	p, err := framework.CreatePrometheusAndWaitUntilReady(p)
	if err != nil {
		t.Fatal(err)
	}

	p.Spec.Storage = &monitoringv1.StorageSpec{
		VolumeClaimTemplate: v1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				CreationTimestamp: metav1.Now(),
			},
			Spec: v1.PersistentVolumeClaimSpec{
				AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
				Resources: v1.ResourceRequirements{
					Requests: v1.ResourceList{
						v1.ResourceStorage: resource.MustParse("200Mi"),
					},
				},
			},
		},
	}
	p, err = framework.MonClientV1.Prometheuses(p.Namespace).Update(p)
	if err != nil {
		t.Fatal(err)
	}

	err = wait.Poll(5*time.Second, 2*time.Minute, func() (bool, error) {
		pods, err := framework.KubeClient.Core().Pods(p.Namespace).List(prometheus.ListOptions(p.Name))
		if err != nil {
			return false, err
		}

		if len(pods.Items) != 1 {
			return false, nil
		}

		for _, volume := range pods.Items[0].Spec.Volumes {
			if volume.Name == "prometheus-"+name+"-db" && volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName != "" {
				return true, nil
			}
		}

		return false, nil
	})

	if err != nil {
		t.Fatal(err)
	}
}

func testPromReloadConfig(t *testing.T, ns testFramework.Namespaces) {
	t.Parallel()

	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup(t)
	ctx.SetupPrometheusRBAC(t, ns, framework.KubeClient)

	name := "test"
	p := framework.MakeBasicPrometheus(ns, name, name, 1)
	p.Spec.ServiceMonitorSelector = nil

	firstConfig := `
global:
  scrape_interval: 1m
scrape_configs:
  - job_name: testReloadConfig
    metrics_path: /metrics
    static_configs:
      - targets:
        - 111.111.111.111:9090
`

	cfg := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("prometheus-%s", name),
                        Namespace: p.Namespace,
		},
		Data: map[string][]byte{
			"prometheus.yaml": []byte(firstConfig),
			"configmaps.json": []byte("{}"),
		},
	}

	svc := framework.MakePrometheusService(p, "not-relevant", v1.ServiceTypeClusterIP)

	if _, err := framework.KubeClient.CoreV1().Secrets(cfg.Namespace).Create(cfg); err != nil {
		t.Fatal(err)
	}

	if _, err := framework.CreatePrometheusAndWaitUntilReady(p); err != nil {
		t.Fatal(err)
	}

	if finalizerFn, err := testFramework.CreateServiceAndWaitUntilReady(framework.KubeClient, *svc); err != nil {
		t.Fatal(err)
	} else {
		ctx.AddFinalizerFn(finalizerFn)
	}

	if err := framework.WaitForTargets(svc, 1); err != nil {
		t.Fatal(err)
	}

	secondConfig := `
global:
  scrape_interval: 1m
scrape_configs:
  - job_name: testReloadConfig
    metrics_path: /metrics
    static_configs:
      - targets:
        - 111.111.111.111:9090
        - 111.111.111.112:9090
`

	cfg, err := framework.KubeClient.CoreV1().Secrets(cfg.Namespace).Get(cfg.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(errors.Wrap(err, "could not retrieve previous secret"))
	}

	cfg.Data["prometheus.yaml"] = []byte(secondConfig)
	if _, err := framework.KubeClient.CoreV1().Secrets(cfg.Namespace).Update(cfg); err != nil {
		t.Fatal(err)
	}

	if err := framework.WaitForTargets(svc, 2); err != nil {
		t.Fatal(err)
	}
}

func testPromAdditionalScrapeConfig(t *testing.T, ns testFramework.Namespaces) {
	t.Parallel()

	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup(t)
	ctx.SetupPrometheusRBAC(t, ns, framework.KubeClient)

	prometheusName := "test"
	group := "additional-config-test"
	p := framework.MakeBasicPrometheus(ns, prometheusName, group, 1)
	svc := framework.MakePrometheusService(p, group, v1.ServiceTypeClusterIP)

	s := framework.MakeBasicServiceMonitor(group, ns.Rules[0])
	if _, err := framework.MonClientV1.ServiceMonitors(s.Namespace).Create(s); err != nil {
		t.Fatal("Creating ServiceMonitor failed: ", err)
	}

	additionalConfig := `
- job_name: "prometheus"
  static_configs:
  - targets: ["localhost:9090"]
`
	secret := v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "additional-scrape-configs",
                        Namespace: ns.Prometheus.Name,
		},
		Data: map[string][]byte{
			"prometheus-additional.yaml": []byte(additionalConfig),
		},
	}
	_, err := framework.KubeClient.CoreV1().Secrets(secret.Namespace).Create(&secret)
	if err != nil {
		t.Fatal(err)
	}

	p.Spec.AdditionalScrapeConfigs = &v1.SecretKeySelector{
		LocalObjectReference: v1.LocalObjectReference{
			Name: "additional-scrape-configs",
		},
		Key: "prometheus-additional.yaml",
	}
	if _, err := framework.CreatePrometheusAndWaitUntilReady(p); err != nil {
		t.Fatal(err)
	}

	if finalizerFn, err := testFramework.CreateServiceAndWaitUntilReady(framework.KubeClient, *svc); err != nil {
		t.Fatal(errors.Wrap(err, "creating prometheus service failed"))
	} else {
		ctx.AddFinalizerFn(finalizerFn)
	}

	// Wait for ServiceMonitor target, as well as additional-config target
	if err := framework.WaitForTargets(svc, 2); err != nil {
		t.Fatal(err)
	}
}

func testPromAdditionalAlertManagerConfig(t *testing.T, ns testFramework.Namespaces) {
	t.Parallel()

	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup(t)
	ctx.SetupPrometheusRBAC(t, ns, framework.KubeClient)

	prometheusName := "test"
	group := "additional-alert-config-test"
	p := framework.MakeBasicPrometheus(ns, prometheusName, group, 1)
	svc := framework.MakePrometheusService(p, group, v1.ServiceTypeClusterIP)

	s := framework.MakeBasicServiceMonitor(group, ns.Rules[0])
	if _, err := framework.MonClientV1.ServiceMonitors(s.Namespace).Create(s); err != nil {
		t.Fatal("Creating ServiceMonitor failed: ", err)
	}

	additionalConfig := `
- path_prefix: /
  scheme: http
  static_configs:
  - targets: ["localhost:9093"]
`
	secret := v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: "additional-alert-configs",
                        Namespace: ns.Prometheus.Name,
		},
		Data: map[string][]byte{
			"prometheus-additional.yaml": []byte(additionalConfig),
		},
	}
	_, err := framework.KubeClient.CoreV1().Secrets(secret.Namespace).Create(&secret)
	if err != nil {
		t.Fatal(err)
	}

	p.Spec.AdditionalAlertManagerConfigs = &v1.SecretKeySelector{
		LocalObjectReference: v1.LocalObjectReference{
			Name: "additional-alert-configs",
		},
		Key: "prometheus-additional.yaml",
	}
	if _, err := framework.CreatePrometheusAndWaitUntilReady(p); err != nil {
		t.Fatal(err)
	}

	if finalizerFn, err := testFramework.CreateServiceAndWaitUntilReady(framework.KubeClient, *svc); err != nil {
		t.Fatal(errors.Wrap(err, "creating prometheus service failed"))
	} else {
		ctx.AddFinalizerFn(finalizerFn)
	}

	// Wait for ServiceMonitor target
	if err := framework.WaitForTargets(svc, 1); err != nil {
		t.Fatal(err)
	}

	err = wait.Poll(time.Second, 5*time.Minute, func() (done bool, err error) {
		response, err := framework.QueryPrometheusSVC(svc, "/api/v1/alertmanagers", map[string]string{})
		if err != nil {
			return true, err
		}

		ra := prometheusAlertmanagerAPIResponse{}
		if err := json.NewDecoder(bytes.NewBuffer(response)).Decode(&ra); err != nil {
			return true, err
		}

		if ra.Status == "success" && len(ra.Data.ActiveAlertmanagers) == 1 {
			return true, nil
		}

		return false, nil
	})

	if err != nil {
		t.Fatal(errors.Wrap(err, "validating Prometheus Alertmanager configuration failed"))
	}
}

func testPromReloadRules(t *testing.T, ns testFramework.Namespaces) {
	t.Parallel()

	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup(t)
	ctx.SetupPrometheusRBAC(t, ns, framework.KubeClient)

	name := "test"
	firtAlertName := "firstAlert"
	secondAlertName := "secondAlert"

	ruleFile, err := framework.MakeAndCreateFiringRule(*ns.Rules[0], name, firtAlertName)
	if err != nil {
		t.Fatal(err)
	}

	p := framework.MakeBasicPrometheus(ns, name, name, 1)
	p.Spec.EvaluationInterval = "1s"
	p, err = framework.CreatePrometheusAndWaitUntilReady(p)
	if err != nil {
		t.Fatal(err)
	}

	pSVC := framework.MakePrometheusService(p, "not-relevant", v1.ServiceTypeClusterIP)
	if finalizerFn, err := testFramework.CreateServiceAndWaitUntilReady(framework.KubeClient, *pSVC); err != nil {
		t.Fatal(errors.Wrap(err, "creating Prometheus service failed"))
	} else {
		ctx.AddFinalizerFn(finalizerFn)
	}

	err = framework.WaitForPrometheusFiringAlert(pSVC, firtAlertName)
	if err != nil {
		t.Fatal(err)
	}

	ruleFile.Spec.Groups = []monitoringv1.RuleGroup{
		{
			Name: "my-alerting-group",
			Rules: []monitoringv1.Rule{
				{
					Alert: secondAlertName,
					Expr:  intstr.FromString("vector(1)"),
				},
			},
		},
	}
	ruleFile, err = framework.UpdateRule(ruleFile)
	if err != nil {
		t.Fatal(err)
	}

	err = framework.WaitForPrometheusFiringAlert(pSVC, secondAlertName)
	if err != nil {
		t.Fatal(err)
	}
}

func testPromMultiplePrometheusRulesSameNS(t *testing.T, ns testFramework.Namespaces) {
	t.Parallel()

	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup(t)
	ctx.SetupPrometheusRBAC(t, ns, framework.KubeClient)

	name := "test"
	alertNames := []string{"first-alert", "second-alert"}

	for _, alertName := range alertNames {
		_, err := framework.MakeAndCreateFiringRule(*ns.Rules[0], alertName, alertName)
		if err != nil {
			t.Fatal(err)
		}
	}

	p := framework.MakeBasicPrometheus(ns, name, name, 1)
	p.Spec.EvaluationInterval = "1s"
	p, err := framework.CreatePrometheusAndWaitUntilReady(p)
	if err != nil {
		t.Fatal(err)
	}

	pSVC := framework.MakePrometheusService(p, "not-relevant", v1.ServiceTypeClusterIP)
	if finalizerFn, err := testFramework.CreateServiceAndWaitUntilReady(framework.KubeClient, *pSVC); err != nil {
		t.Fatal(errors.Wrap(err, "creating Prometheus service failed"))
	} else {
		ctx.AddFinalizerFn(finalizerFn)
	}

	for _, alertName := range alertNames {
		err := framework.WaitForPrometheusFiringAlert(pSVC, alertName)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func testPromMultiplePrometheusRulesDifferentNS(t *testing.T, ns testFramework.Namespaces) {
        t.Skip("asess this one, it should be covered just by calling it with 2 different ns.Rule")
	t.Parallel()

/*
	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup(t)
	alertNSOne := ctx.CreateNamespace(t, framework.KubeClient)
        ns.Rules = append(ns.Rules, ctx.CreateNamespace(t, framework.KubeClient))
	ctx.SetupPrometheusRBAC(t, ns, framework.KubeClient)

	name := "test"
	ruleFiles := []struct {
		alertName string
		ns        string
	}{{"first-alert", ns.Rules[0]}, {"second-alert", ns.Rules[1]}}

	ruleFilesNamespaceSelector := map[string]string{"prometheus": ctx.ID}

	for _, file := range ruleFiles {
		err := testFramework.AddLabelsToNamespace(framework.KubeClient, file.ns, ruleFilesNamespaceSelector)
		if err != nil {
			t.Fatal(err)
		}
	}

	for _, file := range ruleFiles {
		_, err := framework.MakeAndCreateFiringRule(*file.ns, file.alertName, file.alertName)
		if err != nil {
			t.Fatal(err)
		}
	}

	p := framework.MakeBasicPrometheus(ns, name, name, 1)
	p.Spec.EvaluationInterval = "1s"
	p.Spec.RuleNamespaceSelector = &metav1.LabelSelector{
		MatchLabels: ruleFilesNamespaceSelector,
	}
	p, err := framework.CreatePrometheusAndWaitUntilReady(p)
	if err != nil {
		t.Fatal(err)
	}

	pSVC := framework.MakePrometheusService(p, "not-relevant", v1.ServiceTypeClusterIP)
	if finalizerFn, err := testFramework.CreateServiceAndWaitUntilReady(framework.KubeClient, *pSVC); err != nil {
		t.Fatal(errors.Wrap(err, "creating Prometheus service failed"))
	} else {
		ctx.AddFinalizerFn(finalizerFn)
	}

	for _, file := range ruleFiles {
		err := framework.WaitForPrometheusFiringAlert(pSVC, file.alertName)
		if err != nil {
			t.Fatal(err)
		}
	}
*/
}

func testPromRulesExceedingConfigMapLimit(t *testing.T, ns testFramework.Namespaces) {
	t.Parallel()

	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup(t)
	ctx.SetupPrometheusRBAC(t, ns, framework.KubeClient)

	prometheusRules := []*monitoringv1.PrometheusRule{}
	for i := 0; i < 2; i++ {
		rule := generateHugePrometheusRule(*ns.Rules[0], strconv.Itoa(i))
		rule, err := framework.CreateRule(rule)
		if err != nil {
			t.Fatal(err)
		}
		prometheusRules = append(prometheusRules, rule)
	}

	name := "test"

	p := framework.MakeBasicPrometheus(ns, name, name, 1)
	p.Spec.EvaluationInterval = "1s"
	p, err := framework.CreatePrometheusAndWaitUntilReady(p)
	if err != nil {
		t.Fatal(err)
	}

	defer func() {
		if t.Failed() {
			if err := framework.PrintPodLogs(p.Namespace, "prometheus-"+p.Name+"-0"); err != nil {
				t.Fatal(err)
			}
		}
	}()

	pSVC := framework.MakePrometheusService(p, "not-relevant", v1.ServiceTypeClusterIP)
	if finalizerFn, err := testFramework.CreateServiceAndWaitUntilReady(framework.KubeClient, *pSVC); err != nil {
		t.Fatal(errors.Wrap(err, "creating Prometheus service failed"))
	} else {
		ctx.AddFinalizerFn(finalizerFn)
	}

	for i := range prometheusRules {
		_, err := framework.WaitForConfigMapExist(p.Namespace, "prometheus-"+p.Name+"-rulefiles-"+strconv.Itoa(i))
		if err != nil {
			t.Fatal(err)
		}
	}

	// Make sure both rule files ended up in the Prometheus Pod
	for i := range prometheusRules {
		err := framework.WaitForPrometheusFiringAlert(pSVC, "my-alert-"+strconv.Itoa(i))
		if err != nil {
			t.Fatal(err)
		}
	}

	err = framework.DeleteRule(prometheusRules[1])
	if err != nil {
		t.Fatal(err)
	}

	_, err = framework.WaitForConfigMapExist(p.Namespace, "prometheus-"+p.Name+"-rulefiles-0")
	if err != nil {
		t.Fatal(err)
	}
	err = framework.WaitForConfigMapNotExist(p.Namespace, "prometheus-"+p.Name+"-rulefiles-1")
	if err != nil {
		t.Fatal(err)
	}

	err = framework.WaitForPrometheusFiringAlert(pSVC, "my-alert-0")
	if err != nil {
		t.Fatal(err)
	}
}

// generateHugePrometheusRule returns a Prometheus rule instance that would fill
// more than half of the space of a Kubernetes ConfigMap.
func generateHugePrometheusRule(ns v1.Namespace, identifier string) *monitoringv1.PrometheusRule {
	alertName := "my-alert"
	groups := []monitoringv1.RuleGroup{
		{
			Name:  alertName,
			Rules: []monitoringv1.Rule{},
		},
	}
	// One rule marshaled as yaml is ~34 bytes long, the max is ~524288 bytes.
	for i := 0; i < 12000; i++ {
		groups[0].Rules = append(groups[0].Rules, monitoringv1.Rule{
			Alert: alertName + "-" + identifier,
			Expr:  intstr.FromString("vector(1)"),
		})
	}
	rule := framework.MakeBasicRule(ns, "prometheus-rule-"+identifier, groups)

	return rule
}

// Make sure the Prometheus operator only updates the Prometheus config secret
// and the Prometheus rules configmap on relevant changes
func testPromOnlyUpdatedOnRelevantChanges(t *testing.T, ns testFramework.Namespaces) {
	t.Parallel()

	testCTX := framework.NewTestCtx(t)
	defer testCTX.Cleanup(t)
	testCTX.SetupPrometheusRBAC(t, ns, framework.KubeClient)

	name := "test"
	prometheus := framework.MakeBasicPrometheus(ns, name, name, 1)

	// Adding an annotation to Prometheus lead to high CPU usage in the past
	// updating the Prometheus StatefulSet in a loop (See
	// https://github.com/coreos/prometheus-operator/issues/1659). Added here to
	// prevent a regression.
	prometheus.Annotations["test-annotation"] = "test-value"

	ctx, cancel := context.WithCancel(context.Background())

	type versionedResource interface {
		GetResourceVersion() string
	}

	resourceDefinitions := []struct {
		Name               string
		Getter             func(prometheusName string) (versionedResource, error)
		Versions           map[string]interface{}
		MaxExpectedChanges int
	}{
		{
			Name: "prometheus",
			Getter: func(prometheusName string) (versionedResource, error) {
				return framework.
					MonClientV1.
					Prometheuses(prometheus.Namespace).
					Get(prometheusName, metav1.GetOptions{})
			},
			MaxExpectedChanges: 1,
		},
		{
			Name: "rulesConfigMap",
			Getter: func(prometheusName string) (versionedResource, error) {
				return framework.
					KubeClient.
					CoreV1().
					ConfigMaps(prometheus.Namespace).
					Get("prometheus-"+prometheusName+"-rulefiles-0", metav1.GetOptions{})
			},
			// The Prometheus Operator first creates the ConfigMap for the
			// given Prometheus stateful set and then updates it with the matching
			// Prometheus rules.
			MaxExpectedChanges: 2,
		},
		{
			Name: "configurationSecret",
			Getter: func(prometheusName string) (versionedResource, error) {
				return framework.
					KubeClient.
					CoreV1().
					Secrets(prometheus.Namespace).
					Get("prometheus-"+prometheusName, metav1.GetOptions{})
			},
			MaxExpectedChanges: 2,
		},
		{
			Name: "statefulset",
			Getter: func(prometheusName string) (versionedResource, error) {
				return framework.
					KubeClient.
					AppsV1().
					StatefulSets(prometheus.Namespace).
					Get("prometheus-"+prometheusName, metav1.GetOptions{})
			},
			// First is the creation of the StatefulSet itself, following is the
			// update of e.g. the ReadyReplicas status field
			MaxExpectedChanges: 3,
		},
		{
			Name: "service-operated",
			Getter: func(prometheusName string) (versionedResource, error) {
				return framework.
					KubeClient.
					CoreV1().
					Services(prometheus.Namespace).
					Get("prometheus-operated", metav1.GetOptions{})
			},
			MaxExpectedChanges: 1,
		},
		{
			Name: "serviceMonitor",
			Getter: func(prometheusName string) (versionedResource, error) {
				return framework.
					MonClientV1.
					ServiceMonitors(ns.Rules[0].Name).
					Get(prometheusName, metav1.GetOptions{})
			},
			MaxExpectedChanges: 1,
		},
	}

	// Init Versions maps
	for i := range resourceDefinitions {
		resourceDefinitions[i].Versions = map[string]interface{}{}
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				time.Sleep(10 * time.Millisecond)

				for i, resourceDef := range resourceDefinitions {
					resource, err := resourceDef.Getter(prometheus.Name)
					if apierrors.IsNotFound(err) {
						continue
					}
					if err != nil {
						cancel()
						t.Fatal(err)
					}

					resourceDefinitions[i].Versions[resource.GetResourceVersion()] = resource
				}
			}
		}
	}()

	alertName := "my-alert"
	if _, err := framework.MakeAndCreateFiringRule(*ns.Rules[0], "my-prometheus-rule", alertName); err != nil {
		t.Fatal(err)
	}

	prometheus, err := framework.CreatePrometheusAndWaitUntilReady(prometheus)
	if err != nil {
		t.Fatal(err)
	}

	pSVC := framework.MakePrometheusService(prometheus, name, v1.ServiceTypeClusterIP)
	if finalizerFn, err := testFramework.CreateServiceAndWaitUntilReady(framework.KubeClient, *pSVC); err != nil {
		t.Fatal(errors.Wrap(err, "creating Prometheus service failed"))
	} else {
		testCTX.AddFinalizerFn(finalizerFn)
	}

	s := framework.MakeBasicServiceMonitor(name, ns.Rules[0])
	if _, err := framework.MonClientV1.ServiceMonitors(s.Namespace).Create(s); err != nil {
		t.Fatal("Creating ServiceMonitor failed: ", err)
	}

	err = framework.WaitForPrometheusFiringAlert(pSVC, alertName)
	if err != nil {
		t.Fatal(err)
	}

	err = isDiscoveryWorking(pSVC, prometheus.Name)
	if err != nil {
		t.Fatal(errors.Wrap(err, "validating Prometheus target discovery failed"))
	}

	if err := framework.DeletePrometheusAndWaitUntilGone(prometheus); err != nil {
		t.Fatal(err)
	}

	cancel()

	for _, resource := range resourceDefinitions {
		if len(resource.Versions) > resource.MaxExpectedChanges || len(resource.Versions) < 1 {
			var previous interface{}
			for _, version := range resource.Versions {
				if previous == nil {
					previous = version
					continue
				}
				fmt.Println(pretty.Compare(previous, version))
				previous = version
			}

			t.Fatalf(
				"expected resource %v to be created/updated %v times, but saw %v instead",
				resource.Name,
				resource.MaxExpectedChanges,
				len(resource.Versions),
			)
		}
	}
}

func testPromWhenDeleteCRDCleanUpViaOwnerRef(t *testing.T, ns testFramework.Namespaces) {
	t.Parallel()

	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup(t)
	ctx.SetupPrometheusRBAC(t, ns, framework.KubeClient)

	name := "test"

	p := framework.MakeBasicPrometheus(ns, name, name, 1)
	p, err := framework.CreatePrometheusAndWaitUntilReady(p)
	if err != nil {
		t.Fatal(err)
	}

	configMapName := fmt.Sprintf("prometheus-%v-rulefiles-0", p.Name)

	_, err = framework.WaitForConfigMapExist(p.Namespace, configMapName)
	if err != nil {
		t.Fatal(err)
	}

	// Waits for Prometheus pods to vanish
	err = framework.DeletePrometheusAndWaitUntilGone(p)
	if err != nil {
		t.Fatal(err)
	}

	err = framework.WaitForConfigMapNotExist(p.Namespace, configMapName)
	if err != nil {
		t.Fatal(err)
	}
}

func testPromDiscovery(t *testing.T, ns testFramework.Namespaces) {
	t.Parallel()

	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup(t)
	ctx.SetupPrometheusRBAC(t, ns, framework.KubeClient)

	prometheusName := "test"
	group := "servicediscovery-test"
	p := framework.MakeBasicPrometheus(ns, prometheusName, group, 1)
	svc := framework.MakePrometheusService(p, group, v1.ServiceTypeClusterIP)

	s := framework.MakeBasicServiceMonitor(group, ns.Rules[0])
	if _, err := framework.MonClientV1.ServiceMonitors(s.Namespace).Create(s); err != nil {
		t.Fatal("Creating ServiceMonitor failed: ", err)
	}

	p, err := framework.CreatePrometheusAndWaitUntilReady(p)
	if err != nil {
		t.Fatal(err)
	}

	if finalizerFn, err := testFramework.CreateServiceAndWaitUntilReady(framework.KubeClient, *svc); err != nil {
		t.Fatal(errors.Wrap(err, "creating prometheus service failed"))
	} else {
		ctx.AddFinalizerFn(finalizerFn)
	}

	_, err = framework.KubeClient.CoreV1().Secrets(p.Namespace).Get(fmt.Sprintf("prometheus-%s", prometheusName), metav1.GetOptions{})
	if err != nil {
		t.Fatal("Generated Secret could not be retrieved: ", err)
	}

	err = isDiscoveryWorking(svc, prometheusName)
	if err != nil {
		t.Fatal(errors.Wrap(err, "validating Prometheus target discovery failed"))
	}
}

func testPromAlertmanagerDiscovery(t *testing.T, ns testFramework.Namespaces) {
	t.Parallel()

	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup(t)
	ctx.SetupPrometheusRBAC(t, ns, framework.KubeClient)

	prometheusName := "test"
	alertmanagerName := "test"
	group := "servicediscovery-test"
	p := framework.MakeBasicPrometheus(ns, prometheusName, group, 1)
        am := framework.MakeBasicAlertmanager(ns, alertmanagerName, 3)
	svc := framework.MakePrometheusService(p, group, v1.ServiceTypeClusterIP)
	amsvc := framework.MakeAlertmanagerService(am, group, v1.ServiceTypeClusterIP)

	framework.AddAlertingToPrometheus(p, am)
	p, err := framework.CreatePrometheusAndWaitUntilReady(p)
	if err != nil {
		t.Fatal(err)
	}

	if finalizerFn, err := testFramework.CreateServiceAndWaitUntilReady(framework.KubeClient, *svc); err != nil {
		t.Fatal(errors.Wrap(err, "creating Prometheus service failed"))
	} else {
		ctx.AddFinalizerFn(finalizerFn)
	}

	s := framework.MakeBasicServiceMonitor(group, ns.Rules[0])
	if _, err := framework.MonClientV1.ServiceMonitors(s.Namespace).Create(s); err != nil {
		t.Fatalf("Creating ServiceMonitor failed: %v", err)
	}

	_, err = framework.KubeClient.CoreV1().Secrets(p.Namespace).Get(fmt.Sprintf("prometheus-%s", prometheusName), metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Generated Secret could not be retrieved: %v", err)
	}

	if _, err := framework.CreateAlertmanagerAndWaitUntilReady(am); err != nil {
		t.Fatal(err)
	}

	if _, err := testFramework.CreateServiceAndWaitUntilReady(framework.KubeClient, *amsvc); err != nil {
		t.Fatal(errors.Wrap(err, "creating Alertmanager service failed"))
	}

	err = wait.Poll(time.Second, 18*time.Minute, isAlertmanagerDiscoveryWorking(svc, alertmanagerName))
	if err != nil {
		t.Fatal(errors.Wrap(err, "validating Prometheus Alertmanager discovery failed"))
	}
}

func testPromExposingWithKubernetesAPI(t *testing.T, ns testFramework.Namespaces) {
	t.Parallel()

	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup(t)
	ctx.SetupPrometheusRBAC(t, ns, framework.KubeClient)

	basicPrometheus := framework.MakeBasicPrometheus(ns, "basic-prometheus", "test-group", 1)
	service := framework.MakePrometheusService(basicPrometheus, "test-group", v1.ServiceTypeClusterIP)

	if _, err := framework.CreatePrometheusAndWaitUntilReady(basicPrometheus); err != nil {
		t.Fatal("Creating prometheus failed: ", err)
	}

	if _, err := testFramework.CreateServiceAndWaitUntilReady(framework.KubeClient, *service); err != nil {
		t.Fatal("Creating prometheus service failed: ", err)
	}

	ProxyGet := framework.KubeClient.CoreV1().Services(service.Namespace).ProxyGet
	request := ProxyGet("", service.Name, "web", "/metrics", make(map[string]string))
	_, err := request.DoRaw()
	if err != nil {
		t.Fatal(err)
	}
}

func testPromDiscoverTargetPort(t *testing.T, ns testFramework.Namespaces) {
	t.Parallel()

	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup(t)
	ctx.SetupPrometheusRBAC(t, ns, framework.KubeClient)

	prometheusName := "test"
	group := "servicediscovery-test"
	p := framework.MakeBasicPrometheus(ns, prometheusName, group, 1)
	svc := framework.MakePrometheusService(p, group, v1.ServiceTypeClusterIP)

	targetPort := intstr.FromInt(9090)
	if _, err := framework.MonClientV1.ServiceMonitors(ns.Rules[0].Name).Create(&monitoringv1.ServiceMonitor{
		ObjectMeta: metav1.ObjectMeta{
			Name: prometheusName,
			Labels: map[string]string{
				"group": group,
			},
		},
		Spec: monitoringv1.ServiceMonitorSpec{
			Selector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"group": group,
				},
			},
			Endpoints: []monitoringv1.Endpoint{
				{
					TargetPort: &targetPort,
					Interval:   "30s",
				},
			},
		},
	}); err != nil {
		t.Fatal("Creating ServiceMonitor failed: ", err)
	}

	if _, err := framework.CreatePrometheusAndWaitUntilReady(p); err != nil {
		t.Fatal(err)
	}

	if finalizerFn, err := testFramework.CreateServiceAndWaitUntilReady(framework.KubeClient, *svc); err != nil {
		t.Fatal(errors.Wrap(err, "creating prometheus service failed"))
	} else {
		ctx.AddFinalizerFn(finalizerFn)
	}

	_, err := framework.KubeClient.CoreV1().Secrets(p.Namespace).Get(fmt.Sprintf("prometheus-%s", prometheusName), metav1.GetOptions{})
	if err != nil {
		t.Fatal("Generated Secret could not be retrieved: ", err)
	}

	err = isDiscoveryWorking(svc, prometheusName)
	if err != nil {
		t.Fatal(errors.Wrap(err, "validating Prometheus target discovery failed"))
	}
}

/* TODO: reimplement it just by varying values in testFramework.Namespaces
   Plan:
   - framework.MakeBasicPrometheus should add Smon namespace selector for every rules
     Namespace which differs from Prometheus namespace
   - Change calls to MakeBasicServiceMonitor to pass ns.Rules[0]
func testPromOpMatchPromAndServMonInDiffNSs(t *testing.T, ns testFramework.Namespaces) {
	t.Parallel()

	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup(t)
	serviceMonitorNSName := ctx.CreateNamespace(t, framework.KubeClient)
	ctx.SetupPrometheusRBAC(t, ns, framework.KubeClient)

	if err := testFramework.AddLabelsToNamespace(
		framework.KubeClient,
		serviceMonitorNSName,
		map[string]string{"team": "frontend"},
	); err != nil {
		t.Fatal(err)
	}

	group := "sample-app"

	prometheusJobName := serviceMonitorNSName + "/" + group

	prometheusName := "test"
	svc := framework.MakePrometheusService(prometheusName, group, v1.ServiceTypeClusterIP)

	s := framework.MakeBasicServiceMonitor(group, serviceMonitorNSName)

	if _, err := framework.MonClientV1.ServiceMonitors(serviceMonitorNSName).Create(s); err != nil {
		t.Fatal("Creating ServiceMonitor failed: ", err)
	}

	p := framework.MakeBasicPrometheus(prometheusNSName, prometheusName, group, 1)
	p.Spec.ServiceMonitorNamespaceSelector = &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"team": "frontend",
		},
	}
	if _, err := framework.CreatePrometheusAndWaitUntilReady(prometheusNSName, p); err != nil {
		t.Fatal(err)
	}

	if finalizerFn, err := testFramework.CreateServiceAndWaitUntilReady(framework.KubeClient, prometheusNSName, svc); err != nil {
		t.Fatal(errors.Wrap(err, "creating prometheus service failed"))
	} else {
		ctx.AddFinalizerFn(finalizerFn)
	}

	resp, err := framework.QueryPrometheusSVC(prometheusNSName, svc.Name, "/api/v1/status/config", map[string]string{})
	if err != nil {
		t.Fatal(err)
	}

	if strings.Count(string(resp), prometheusJobName) != 1 {
		t.Fatalf("expected Prometheus operator to configure Prometheus in ns '%v' to scrape the service monitor in ns '%v'", prometheusNSName, serviceMonitorNSName)
	}
}
*/

func testThanos(t *testing.T, ns testFramework.Namespaces) {
	t.Parallel()

	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup(t)
	ctx.SetupPrometheusRBAC(t, ns, framework.KubeClient)

	peerServiceName := "thanos-peers"
	querierServiceName := "thanos-querier"
	basicPrometheus := framework.MakeBasicPrometheus(ns, "basic-prometheus", "test-group", 1)
	peerServiceDNS := fmt.Sprintf("%s.%s.svc:10900", peerServiceName, basicPrometheus.Namespace)
	version := "v0.1.0"
	basicPrometheus.Spec.Thanos = &monitoringv1.ThanosSpec{
		Peers:   &peerServiceDNS,
		Version: &version,
	}
	basicPrometheus.Spec.PodMetadata = &metav1.ObjectMeta{
		CreationTimestamp: metav1.Now(),
		Labels: map[string]string{
			"thanos-peer": "true",
		},
	}
	replicas := int32(2)
	basicPrometheus.Spec.Replicas = &replicas
	pservice := framework.MakePrometheusService(basicPrometheus, "test-group", v1.ServiceTypeClusterIP)
	tservice := framework.MakeThanosService(peerServiceName)
	qservice := framework.MakeThanosQuerierService(querierServiceName)
	s := framework.MakeBasicServiceMonitor("test-group", ns.Rules[0])
	thanosQuerier, err := testFramework.MakeDeployment("../../example/thanos/querier-deployment.yaml")
	if err != nil {
		t.Fatal("Making deployment failed: ", err)
	}
	querierArgs := []string{
		"query",
		"--log.level=debug",
		"--query.replica-label=prometheus_replica",
		fmt.Sprintf("--cluster.peers=%s", peerServiceDNS),
	}
	log.Println("setting up querier with args: ", querierArgs)
	thanosQuerier.Spec.Template.Spec.Containers[0].Args = querierArgs

	if err := testFramework.CreateDeployment(framework.KubeClient, basicPrometheus.Namespace, thanosQuerier); err != nil {
		t.Fatal("Creating Thanos querier failed: ", err)
	}

	if _, err := testFramework.CreateServiceAndWaitUntilReady(framework.KubeClient, *qservice); err != nil {
		t.Fatal("Creating Thanos querier service failed: ", err)
	}

	if _, err := framework.MonClientV1.ServiceMonitors(s.Namespace).Create(s); err != nil {
		t.Fatal("Creating ServiceMonitor failed: ", err)
	}

	if _, err := framework.KubeClient.CoreV1().Services(pservice.Namespace).Create(pservice); err != nil {
		t.Fatal("Creating prometheus service failed: ", err)
	}

	if _, err := framework.MonClientV1.Prometheuses(basicPrometheus.Namespace).Create(basicPrometheus); err != nil {
		t.Fatal("Creating prometheus failed: ", err)
	}

	if _, err := framework.KubeClient.CoreV1().Services(tservice.Namespace).Create(tservice); err != nil {
		t.Fatal("Creating prometheus service failed: ", err)
	}

	err = wait.Poll(5*time.Second, 5*time.Minute, func() (bool, error) {
		proxyGet := framework.KubeClient.CoreV1().Services(qservice.Namespace).ProxyGet
		request := proxyGet("http", qservice.Name, "http-query", "/api/v1/query", map[string]string{"query": "prometheus_build_info", "dedup": "false"})
		b, err := request.DoRaw()
		if err != nil {
			log.Println(fmt.Sprintf("Error performing request against Thanos querier: %v\n\nretrying...", err))
			return false, nil
		}

		d := struct {
			Data struct {
				Result []map[string]interface{} `json:"result"`
			} `json:"data"`
		}{}

		err = json.Unmarshal(b, &d)
		if err != nil {
			return false, err
		}

		result := len(d.Data.Result)
		// We're expecting 4 results as we are requesting the
		// `prometheus_build_info` metric, which is collected for both
		// Prometheus replicas by both replicas.
		expected := 4
		if result != expected {
			log.Printf("Unexpected number of results from query. Got %d, expected %d. retrying...\n", result, expected)
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		t.Fatal("Failed to get correct result from Thanos querier: ", err)
	}
}

func testPromGetBasicAuthSecret(t *testing.T, ns testFramework.Namespaces) {
	t.Parallel()

	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup(t)
	ctx.SetupPrometheusRBAC(t, ns, framework.KubeClient)

	name := "test"

	prometheusCRD := framework.MakeBasicPrometheus(ns, name, name, 1)

	if _, err := framework.CreatePrometheusAndWaitUntilReady(prometheusCRD); err != nil {
		t.Fatal(err)
	}
	testNamespace := ns.Rules[0]

	simple, err := testFramework.MakeDeployment("../../test/framework/ressources/basic-auth-app-deployment.yaml")
	if err != nil {
		t.Fatal(err)
	}

	if err := testFramework.CreateDeployment(framework.KubeClient, testNamespace.Name, simple); err != nil {
		t.Fatal("Creating simple basic auth app failed: ", err)
	}

	authSecret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
                        Namespace: testNamespace.Name,
		},
		Data: map[string][]byte{
			"user":     []byte("user"),
			"password": []byte("pass"),
		},
	}

	if _, err := framework.KubeClient.CoreV1().Secrets(authSecret.Namespace).Create(authSecret); err != nil {
		t.Fatal(err)
	}

	svc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
                        Namespace: testNamespace.Name,
			Labels: map[string]string{
				"group": name,
			},
		},
		Spec: v1.ServiceSpec{
			Type: v1.ServiceTypeLoadBalancer,
			Ports: []v1.ServicePort{
				v1.ServicePort{
					Name: "web",
					Port: 8080,
				},
			},
			Selector: map[string]string{
				"group": name,
			},
		},
	}

	sm := framework.MakeBasicServiceMonitor(name, testNamespace)
	sm.Spec.Endpoints[0].BasicAuth = &monitoringv1.BasicAuth{
		Username: v1.SecretKeySelector{
			LocalObjectReference: v1.LocalObjectReference{
				Name: name,
			},
			Key: "user",
		},
		Password: v1.SecretKeySelector{
			LocalObjectReference: v1.LocalObjectReference{
				Name: name,
			},
			Key: "password",
		},
	}

	if finalizerFn, err := testFramework.CreateServiceAndWaitUntilReady(framework.KubeClient, *svc); err != nil {
		t.Fatal(err)
	} else {
		ctx.AddFinalizerFn(finalizerFn)
	}

	if _, err := framework.MonClientV1.ServiceMonitors(sm.Namespace).Create(sm); err != nil {
		t.Fatal("Creating ServiceMonitor failed: ", err)
	}

	if err := framework.WaitForTargetsPrometheus(prometheusCRD, 1); err != nil {
		t.Fatal(err)
	}

}

// testOperatorNSScope tests the multi namespace feature of the Prometheus Operator.
// It checks whether it ignores rules that are not in the watched namespaces of the
// Prometheus Operator. The Prometheus Operator internally treats watching a
// single namespace different than watching multiple namespaces, hence the two
// sub-tests.
func testOperatorNSScope(t *testing.T, ns testFramework.Namespaces) {
	name := "test"
	firtAlertName := "firstAlert"
	secondAlertName := "secondAlert"

	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup(t)

	arbitraryNS := ctx.CreateNamespace(t, framework.KubeClient)
	ctx.SetupPrometheusRBAC(t, ns, framework.KubeClient)


        // attach labels as for normal Rules namespace, but we expect ServiceMonitors to be not picked up there
        err := testFramework.AddLabelsToNamespace(framework.KubeClient, arbitraryNS.Name, map[string]string{"for-prometheus": ns.Rules[0].Labels["for-prometheus"]})
        if err != nil {
                t.Fatal(err)
        }

	// Prometheus Operator only watches prometheusNS and ruleNS, not arbitraryNS.
	err = framework.CreatePrometheusOperator(ns, *opImage)
	if err != nil {
		t.Fatal(err)
	}

	ruleDef := []struct {
		NS        v1.Namespace    
		AlertName string
	}{{*arbitraryNS, secondAlertName}, {*ns.Rules[0], firtAlertName}}

	for _, r := range ruleDef {
		_, err := framework.MakeAndCreateFiringRule(r.NS, name, r.AlertName)
		if err != nil {
			t.Fatal(err)
		}
	}

	p := framework.MakeBasicPrometheus(ns, name, name, 1)
	p.Spec.EvaluationInterval = "1s"
	p, err = framework.CreatePrometheusAndWaitUntilReady(p)
	if err != nil {
		t.Fatal(err)
	}

	pSVC := framework.MakePrometheusService(p, "not-relevant", v1.ServiceTypeClusterIP)
	if finalizerFn, err := testFramework.CreateServiceAndWaitUntilReady(framework.KubeClient, *pSVC); err != nil {
		t.Fatal(errors.Wrap(err, "creating Prometheus service failed"))
	} else {
		ctx.AddFinalizerFn(finalizerFn)
	}

	err = framework.WaitForPrometheusFiringAlert(pSVC, firtAlertName)
	if err != nil {
		t.Fatal(err)
	}

	firing, err := framework.CheckPrometheusFiringAlert(pSVC, secondAlertName)
	if err != nil && !strings.Contains(err.Error(), "expected 1 query result but got 0") {
		t.Fatal(err)
	}

	if firing {
		t.Fatalf("expected alert %q not to fire", secondAlertName)
	}
}

func isDiscoveryWorking(svc *v1.Service, prometheusName string) error {
	var loopErr error

	err := wait.Poll(time.Second, 5*framework.DefaultTimeout, func() (bool, error) {
		pods, loopErr := framework.KubeClient.CoreV1().Pods(svc.Namespace).List(prometheus.ListOptions(prometheusName))
		if loopErr != nil {
			return false, loopErr
		}
		if 1 != len(pods.Items) {
			return false, nil
		}
		podIP := pods.Items[0].Status.PodIP
		expectedTargets := []string{fmt.Sprintf("http://%s:9090/metrics", podIP)}

		activeTargets, loopErr := framework.GetActiveTargets(svc)
		if loopErr != nil {
			return false, loopErr
		}

		if loopErr = assertExpectedTargets(activeTargets, expectedTargets); loopErr != nil {
			return false, nil
		}

		working, loopErr := basicQueryWorking(svc)
		if loopErr != nil {
			return false, loopErr
		}
		if !working {
			return false, nil
		}

		return true, nil
	})

	if err != nil {
		return fmt.Errorf("waiting for Prometheus to discover targets failed: %v: %v", err, loopErr)
	}

	return nil
}

type resultVector struct {
	Metric map[string]string `json:"metric"`
	Value  []interface{}     `json:"value"`
}

type queryResult struct {
	ResultType string          `json:"resultType"`
	Result     []*resultVector `json:"result"`
}

type prometheusQueryAPIResponse struct {
	Status string       `json:"status"`
	Data   *queryResult `json:"data"`
}

func basicQueryWorking(svc *v1.Service) (bool, error) {
	response, err := framework.QueryPrometheusSVC(svc, "/api/v1/query", map[string]string{"query": "up"})
	if err != nil {
		return false, err
	}

	rq := prometheusQueryAPIResponse{}
	if err := json.NewDecoder(bytes.NewBuffer(response)).Decode(&rq); err != nil {
		return false, err
	}

	if rq.Status != "success" && rq.Data.Result[0].Value[1] == "1" {
		log.Printf("Query Response not successful.")
		return false, nil
	}

	return true, nil
}

func isAlertmanagerDiscoveryWorking(svc *v1.Service, alertmanagerName string) func() (bool, error) {
	return func() (bool, error) {
		pods, err := framework.KubeClient.CoreV1().Pods(svc.Namespace).List(alertmanager.ListOptions(alertmanagerName))
		if err != nil {
			return false, err
		}
		if 3 != len(pods.Items) {
			return false, nil
		}
		expectedAlertmanagerTargets := []string{}
		for _, p := range pods.Items {
			expectedAlertmanagerTargets = append(expectedAlertmanagerTargets, fmt.Sprintf("http://%s:9093/api/v1/alerts", p.Status.PodIP))
		}

		response, err := framework.QueryPrometheusSVC(svc, "/api/v1/alertmanagers", map[string]string{})
		if err != nil {
			return false, err
		}

		ra := prometheusAlertmanagerAPIResponse{}
		if err := json.NewDecoder(bytes.NewBuffer(response)).Decode(&ra); err != nil {
			return false, err
		}

		if assertExpectedAlertmanagerTargets(ra.Data.ActiveAlertmanagers, expectedAlertmanagerTargets) {
			return true, nil
		}

		return false, nil
	}
}

func assertExpectedTargets(targets []*testFramework.Target, expectedTargets []string) error {
	existingTargets := []string{}

	for _, t := range targets {
		existingTargets = append(existingTargets, t.ScrapeURL)
	}

	sort.Strings(expectedTargets)
	sort.Strings(existingTargets)

	if !reflect.DeepEqual(expectedTargets, existingTargets) {
		return fmt.Errorf(
			"expected targets %q but got %q", strings.Join(expectedTargets, ","),
			strings.Join(existingTargets, ","),
		)
	}

	return nil
}

func assertExpectedAlertmanagerTargets(ams []*alertmanagerTarget, expectedTargets []string) bool {
	log.Printf("Expected Alertmanager Targets: %#+v\n", expectedTargets)

	existingTargets := []string{}

	for _, am := range ams {
		existingTargets = append(existingTargets, am.URL)
	}

	sort.Strings(expectedTargets)
	sort.Strings(existingTargets)

	if !reflect.DeepEqual(expectedTargets, existingTargets) {
		log.Printf("Existing Alertmanager Targets: %#+v\n", existingTargets)
		return false
	}

	return true
}

type alertmanagerTarget struct {
	URL string `json:"url"`
}

type alertmanagerDiscovery struct {
	ActiveAlertmanagers []*alertmanagerTarget `json:"activeAlertmanagers"`
}

type prometheusAlertmanagerAPIResponse struct {
	Status string                 `json:"status"`
	Data   *alertmanagerDiscovery `json:"data"`
}
