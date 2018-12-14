// Copyright 2017 The prometheus-operator Authors
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

package framework

import (
	"fmt"
	"testing"

	"github.com/pkg/errors"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)


type Namespaces struct {
   Operator *v1.Namespace
   Prometheus *v1.Namespace
   Alertmanager *v1.Namespace
   Rules []*v1.Namespace
}

type NamespacesPlan struct {
   Namespaces
}

func CreateNamespace(kubeClient kubernetes.Interface, name string) (*v1.Namespace, error) {
	namespace, err := kubeClient.Core().Namespaces().Create(&v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	})
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("failed to create namespace with name %v", name))
	}
	return namespace, nil
}

func (ctx TestCtx) NewNamespaces(t *testing.T, kubeClient kubernetes.Interface, plan NamespacesPlan) Namespaces {
       created := make(map[*v1.Namespace]struct{})  // same pointer Namespace used in plan multiple times will be created just once
       var ns Namespaces = plan.Namespaces
  
       for _, nsPlanned := range  append([]*v1.Namespace{plan.Operator, plan.Prometheus, plan.Alertmanager}, plan.Rules...) {
             if _, ok := created[nsPlanned]; ok {
                 continue
             }
             *nsPlanned = *ctx.CreateNamespace(t, kubeClient)  //this updates content of the pointer inside result ns
             created[nsPlanned] = struct{}{}
       }

       for _, ruleNs := range ns.Rules {
            if err := AddLabelsToNamespace(kubeClient, ruleNs, map[string]string{"for-prometheus": ns.Prometheus.Name}); err != nil {
                 t.Fatal(err)
            }
       }
       return ns
}

func (ctx *TestCtx) CreateNamespace(t *testing.T, kubeClient kubernetes.Interface) *v1.Namespace {
        var ns *v1.Namespace
        var err error

	if ns, err = CreateNamespace(kubeClient, ctx.GetObjID()); err != nil {
		t.Fatal(err)
	}

	namespaceFinalizerFn := func() error {
		return DeleteNamespace(kubeClient, ns.Name)
	}

	ctx.AddFinalizerFn(namespaceFinalizerFn)

	return ns
}

func DeleteNamespace(kubeClient kubernetes.Interface, name string) error {
	return kubeClient.Core().Namespaces().Delete(name, nil)
}

func AddLabelsToNamespace(kubeClient kubernetes.Interface, ns *v1.Namespace, additionalLabels map[string]string) error {
	n, err := kubeClient.CoreV1().Namespaces().Get(ns.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}

	if n.Labels == nil {
		n.Labels = map[string]string{}
	}

	for k, v := range additionalLabels {
		n.Labels[k] = v
	}

	n, err = kubeClient.CoreV1().Namespaces().Update(n)
	if err != nil {
		return err
	}

        *ns = *n
	return nil
}
