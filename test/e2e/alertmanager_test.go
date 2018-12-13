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
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1beta2"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"

	"github.com/coreos/prometheus-operator/pkg/alertmanager"
	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	testFramework "github.com/coreos/prometheus-operator/test/framework"
	"github.com/golang/protobuf/proto"
)

func testAMCreateDeleteCluster(t *testing.T, ns testFramework.Namespaces) {
        var err error
	t.Parallel()

	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup(t)
	ctx.SetupPrometheusRBAC(t, ns, framework.KubeClient)

        a := framework.MakeBasicAlertmanager(ns, "test", 3)

	if a, err = framework.CreateAlertmanagerAndWaitUntilReady(a); err != nil {
		t.Fatal(err)
	}

	if err := framework.DeleteAlertmanagerAndWaitUntilGone(a); err != nil {
		t.Fatal(err)
	}
}

func testAMScaling(t *testing.T, ns testFramework.Namespaces) {
	t.Parallel()

	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup(t)
	ctx.SetupPrometheusRBAC(t, ns, framework.KubeClient)

        a := framework.MakeBasicAlertmanager(ns, "test", 3)

	a, err := framework.CreateAlertmanagerAndWaitUntilReady(a)
	if err != nil {
		t.Fatal(err)
	}

	a.Spec.Replicas = proto.Int32(5)
	a, err = framework.UpdateAlertmanagerAndWaitUntilReady(a)
	if err != nil {
		t.Fatal(err)
	}

	a.Spec.Replicas = proto.Int32(3)
	if _, err := framework.UpdateAlertmanagerAndWaitUntilReady(a); err != nil {
		t.Fatal(err)
	}
}

func testAMVersionMigration(t *testing.T, ns testFramework.Namespaces) {
	t.Parallel()

	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup(t)
	ctx.SetupPrometheusRBAC(t, ns, framework.KubeClient)

	name := "test"

	am := framework.MakeBasicAlertmanager(ns, name, 1)
	am.Spec.Version = "v0.14.0"
	am, err := framework.CreateAlertmanagerAndWaitUntilReady(am)
	if err != nil {
		t.Fatal(err)
	}

	am.Spec.Version = "v0.15.3"
	am, err = framework.UpdateAlertmanagerAndWaitUntilReady(am)
	if err != nil {
		t.Fatal(err)
	}

	am.Spec.Version = "v0.14.0"
	am, err = framework.UpdateAlertmanagerAndWaitUntilReady(am)
	if err != nil {
		t.Fatal(err)
	}
}

func testAMStorageUpdate(t *testing.T, ns testFramework.Namespaces) {
	t.Parallel()

	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup(t)

	name := "test"

	am := framework.MakeBasicAlertmanager(ns, name, 1)

	am, err := framework.CreateAlertmanagerAndWaitUntilReady(am)
	if err != nil {
		t.Fatal(err)
	}

	am.Spec.Storage = &monitoringv1.StorageSpec{
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

	am, err = framework.MonClientV1.Alertmanagers(am.Namespace).Update(am)
	if err != nil {
		t.Fatal(err)
	}

	err = wait.Poll(5*time.Second, 2*time.Minute, func() (bool, error) {
		pods, err := framework.KubeClient.Core().Pods(am.Namespace).List(alertmanager.ListOptions(name))
		if err != nil {
			return false, err
		}

		if len(pods.Items) != 1 {
			return false, nil
		}

		for _, volume := range pods.Items[0].Spec.Volumes {
			if volume.Name == "alertmanager-"+name+"-db" && volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName != "" {
				return true, nil
			}
		}

		return false, nil
	})

	if err != nil {
		t.Fatal(err)
	}
}

func testAMExposingWithKubernetesAPI(t *testing.T, ns testFramework.Namespaces) {
	t.Parallel()

	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup(t)
	ctx.SetupPrometheusRBAC(t, ns, framework.KubeClient)

	alertmanager := framework.MakeBasicAlertmanager(ns, "test-alertmanager", 1)
	alertmanagerService := framework.MakeAlertmanagerService(alertmanager, "alertmanager-service", v1.ServiceTypeClusterIP)

	if _, err := framework.CreateAlertmanagerAndWaitUntilReady(alertmanager); err != nil {
		t.Fatal(err)
	}

	if _, err := testFramework.CreateServiceAndWaitUntilReady(framework.KubeClient, *alertmanagerService); err != nil {
		t.Fatal(err)
	}

	proxyGet := framework.KubeClient.CoreV1().Services(alertmanager.Namespace).ProxyGet
	request := proxyGet("", alertmanagerService.Name, "web", "/", make(map[string]string))
	_, err := request.DoRaw()
	if err != nil {
		t.Fatal(err)
	}
}

func testAMMeshInitialization(t *testing.T, _ testFramework.Namespaces) {
	t.Parallel()

	// Starting with Alertmanager v0.15.0 hashicorp/memberlist is used for HA.
	// Make sure both memberlist as well as mesh (< 0.15.0) work
	amVersions := []string{"v0.14.0", "v0.15.3"}

	for _, v := range amVersions {
		version := v
		t.Run(
			fmt.Sprintf("amVersion%v", strings.Replace(version, ".", "-", -1)),
			func(t *testing.T) {
				t.Parallel()
				ctx := framework.NewTestCtx(t)
				defer ctx.Cleanup(t)
				ns := &v1.Namespace{}
                                nss := ctx.NewNamespaces(t, framework.KubeClient, testFramework.NamespacesPlan{testFramework.Namespaces{ns, ns, ns, []*v1.Namespace{ns}}})
				ctx.SetupPrometheusRBAC(t, nss, framework.KubeClient)

				amClusterSize := 3
				alertmanager := framework.MakeBasicAlertmanager(nss, "test", int32(amClusterSize))
				alertmanager.Spec.Version = version
				alertmanagerService := framework.MakeAlertmanagerService(alertmanager, "alertmanager-service", v1.ServiceTypeClusterIP)

				if _, err := framework.CreateAlertmanagerAndWaitUntilReady(alertmanager); err != nil {
					t.Fatal(err)
				}

				if _, err := testFramework.CreateServiceAndWaitUntilReady(framework.KubeClient, *alertmanagerService); err != nil {
					t.Fatal(err)
				}

				for i := 0; i < amClusterSize; i++ {
					name := "alertmanager-" + alertmanager.Name + "-" + strconv.Itoa(i)
					if err := framework.WaitForAlertmanagerInitializedMesh(alertmanager.Namespace, name, amClusterSize); err != nil {
						t.Fatal(err)
					}
				}
			},
		)
	}
}

func testAMClusterGossipSilences(t *testing.T, ns testFramework.Namespaces) {
	t.Parallel()
	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup(t)
	ctx.SetupPrometheusRBAC(t, ns, framework.KubeClient)

	amClusterSize := 3
	alertmanager := framework.MakeBasicAlertmanager(ns, "test", int32(amClusterSize))

	if _, err := framework.CreateAlertmanagerAndWaitUntilReady(alertmanager); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < amClusterSize; i++ {
		name := "alertmanager-" + alertmanager.Name + "-" + strconv.Itoa(i)
		if err := framework.WaitForAlertmanagerInitializedMesh(alertmanager.Namespace, name, amClusterSize); err != nil {
			t.Fatal(err)
		}
	}

	silId, err := framework.CreateSilence(alertmanager.Namespace, "alertmanager-test-0")
	if err != nil {
		t.Fatalf("failed to create silence: %v", err)
	}

	for i := 0; i < amClusterSize; i++ {
		err = wait.Poll(time.Second, framework.DefaultTimeout, func() (bool, error) {
			silences, err := framework.GetSilences(alertmanager.Namespace, "alertmanager-"+alertmanager.Name+"-"+strconv.Itoa(i))
			if err != nil {
				return false, err
			}

			if len(silences) != 1 {
				return false, nil
			}

			if silences[0].ID != silId {
				return false, errors.Errorf("expected silence id on alertmanager %v to match id of created silence '%v' but got %v", i, silId, silences[0].ID)
			}
			return true, nil
		})
		if err != nil {
			t.Fatalf("could not retrieve created silence on alertmanager %v: %v", i, err)
		}
	}
}

func testAMReloadConfig(t *testing.T, ns testFramework.Namespaces) {
	t.Parallel()

	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup(t)
	ctx.SetupPrometheusRBAC(t, ns, framework.KubeClient)

	alertmanager := framework.MakeBasicAlertmanager(ns, "reload-config", 1)

	firstConfig := `
global:
  resolve_timeout: 5m
route:
  group_by: ['job']
  group_wait: 30s
  group_interval: 5m
  repeat_interval: 12h
  receiver: 'webhook'
receivers:
- name: 'webhook'
  webhook_configs:
  - url: 'http://firstConfigWebHook:30500/'
`
	secondConfig := `
global:
  resolve_timeout: 5m
route:
  group_by: ['job']
  group_wait: 30s
  group_interval: 5m
  repeat_interval: 12h
  receiver: 'webhook'
receivers:
- name: 'webhook'
  webhook_configs:
  - url: 'http://secondConfigWebHook:30500/'
`

	cfg := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("alertmanager-%s", alertmanager.Name),
			Namespace: alertmanager.Namespace,
		},
		Data: map[string][]byte{
			"alertmanager.yaml": []byte(firstConfig),
		},
	}

	if _, err := framework.CreateAlertmanagerAndWaitUntilReady(alertmanager); err != nil {
		t.Fatal(err)
	}

	if _, err := framework.KubeClient.CoreV1().Secrets(cfg.Namespace).Update(cfg); err != nil {
		t.Fatal(err)
	}

	firstExpectedString := "firstConfigWebHook"
	if err := framework.WaitForAlertmanagerConfigToContainString(alertmanager.Namespace, alertmanager.Name, firstExpectedString); err != nil {
		t.Fatal(errors.Wrap(err, "failed to wait for first expected config"))
	}
	cfg.Data["alertmanager.yaml"] = []byte(secondConfig)

	if _, err := framework.KubeClient.CoreV1().Secrets(cfg.Namespace).Update(cfg); err != nil {
		t.Fatal(err)
	}

	secondExpectedString := "secondConfigWebHook"

	if err := framework.WaitForAlertmanagerConfigToContainString(alertmanager.Namespace, alertmanager.Name, secondExpectedString); err != nil {
		t.Fatal(errors.Wrap(err, "failed to wait for second expected config"))
	}
}

func testAMZeroDowntimeRollingDeployment(t *testing.T, ns testFramework.Namespaces) {
	t.Parallel()

	ctx := framework.NewTestCtx(t)
	defer ctx.Cleanup(t)
	ctx.SetupPrometheusRBAC(t, ns, framework.KubeClient)

	whReplicas := int32(1)
	whdpl := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "alertmanager-webhook",
                        Namespace: ns.Alertmanager.Name,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &whReplicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "alertmanager-webhook",
				},
			},
			Template: v1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "alertmanager-webhook",
					},
				},
				Spec: v1.PodSpec{
					Containers: []v1.Container{
						{
							Name:  "webhook-server",
							Image: "quay.io/coreos/prometheus-alertmanager-test-webhook",
							Ports: []v1.ContainerPort{
								{
									Name:          "web",
									ContainerPort: 5001,
								},
							},
						},
					},
				},
			},
		},
	}
	whsvc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "alertmanager-webhook",
			Namespace: ns.Alertmanager.Name,
		},
		Spec: v1.ServiceSpec{
			Type: v1.ServiceTypeClusterIP,
			Ports: []v1.ServicePort{
				{
					Name:       "web",
					Port:       5001,
					TargetPort: intstr.FromString("web"),
				},
			},
			Selector: map[string]string{
				"app": "alertmanager-webhook",
			},
		},
	}
	if err := testFramework.CreateDeployment(framework.KubeClient, whdpl.Namespace, whdpl); err != nil {
		t.Fatal(err)
	}
	if _, err := testFramework.CreateServiceAndWaitUntilReady(framework.KubeClient, *whsvc); err != nil {
		t.Fatal(err)
	}
	err := testFramework.WaitForPodsReady(framework.KubeClient, whdpl.Namespace, time.Minute*5, 1,
		metav1.ListOptions{
			LabelSelector: fields.SelectorFromSet(fields.Set(map[string]string{
				"app": "alertmanager-webhook",
			})).String(),
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	alertmanager := framework.MakeBasicAlertmanager(ns, "rolling-deploy", 3)
	alertmanager.Spec.Version = "v0.13.0"
	amsvc := framework.MakeAlertmanagerService(alertmanager, "test", v1.ServiceTypeClusterIP)
	amcfg := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("alertmanager-%s", alertmanager.Name),
                        Namespace: alertmanager.Namespace,
		},
		Data: map[string][]byte{
			"alertmanager.yaml": []byte(fmt.Sprintf(`
global:
  resolve_timeout: 5m

route:
  group_by: ['alertname']
  group_wait: 10s
  group_interval: 10s
  repeat_interval: 1h
  receiver: 'webhook'
receivers:
- name: 'webhook'
  webhook_configs:
  - url: 'http://%s.%s.svc:5001/'
inhibit_rules:
  - source_match:
      severity: 'critical'
    target_match:
      severity: 'warning'
    equal: ['alertname', 'dev', 'instance']
`, whsvc.Name, ns)),
		},
	}

	if _, err := framework.KubeClient.CoreV1().Secrets(amcfg.Namespace).Create(amcfg); err != nil {
		t.Fatal(err)
	}
	alertmanager, err = framework.MonClientV1.Alertmanagers(alertmanager.Namespace).Create(alertmanager)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testFramework.CreateServiceAndWaitUntilReady(framework.KubeClient, *amsvc); err != nil {
		t.Fatal(err)
	}

	// Send alert to each Alertmanager
	for i := 0; i < int(*alertmanager.Spec.Replicas); i++ {
		replica := i
		done := make(chan struct{})
		errc := make(chan error, 1)

		defer func() {
			close(done)
			select {
			case err := <-errc:
				t.Fatal(errors.Wrapf(err, "sending alert to alertmanager %v", replica))
			default:
				return
			}
		}()

		go func() {
			ticker := time.NewTicker(100 * time.Millisecond)
			start := time.Now()
			failures := 0
			for {
				select {
				case <-ticker.C:
					err := framework.SendAlertToAlertmanager(
						alertmanager.Namespace,
						"alertmanager-rolling-deploy-"+strconv.Itoa(replica),
						start,
					)
					if err != nil {
						failures++
						// Allow 50 (~5 Seconds) failures during Alertmanager rolling update.
						if failures > 50 {
							errc <- err
							return
						}
					}
				case <-done:
					return
				}

			}
		}()
	}

	// Wait for alert to propagate
	time.Sleep(10 * time.Second)

	opts := metav1.ListOptions{
		LabelSelector: whdpl.Spec.Selector.String(),
        }
	pl, err := framework.KubeClient.Core().Pods(whdpl.Namespace).List(opts)
	if err != nil {
		t.Fatal(err)
	}

	if len(pl.Items) != 1 {
		t.Fatalf("Expected one webhook pod, but got %d", len(pl.Items))
	}

        pod := pl.Items[0]
	logs, err := testFramework.GetLogs(framework.KubeClient, pod.Namespace, pod.Name, "webhook-server")
	if err != nil {
		t.Fatal(err)
	}

	c := strings.Count(logs, "Alertmanager Notification Payload Received")
	if c != 1 {
		t.Fatalf("One notification expected, but %d received.\n\n%s", c, logs)
	}

	alertmanager.Spec.Version = "v0.14.0"
	alertmanager, err = framework.MonClientV1.Alertmanagers(alertmanager.Namespace).Update(alertmanager)
	if err != nil {
		t.Fatal(err)
	}

	time.Sleep(time.Minute)

	logs, err = testFramework.GetLogs(framework.KubeClient, pod.Namespace, pod.Name, "webhook-server")
	if err != nil {
		t.Fatal(err)
	}

	c = strings.Count(logs, "Alertmanager Notification Payload Received")
	if c != 1 {
		t.Fatalf("Only one notification expected, but %d received after rolling update of Alertmanager cluster.\n\n%s", c, logs)
	}
}
