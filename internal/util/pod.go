/*
Copyright 2024 Ontario Systems.

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

package util

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var plog = logf.Log.WithName("pod utils")

// ControllerNameFromPod given a pod will return the root controller from the owner references
func ControllerNameFromPod(pod *v1.Pod) (string, *metav1.OwnerReference) {
	c, err := client.New(GetConfig(), client.Options{})
	if err != nil {
		panic(errors.New("could not get client"))
	}
	owner := getRootOwner(client.NewNamespacedClient(c, pod.Namespace), pod.OwnerReferences)
	if owner != nil {
		return fmt.Sprintf("%s-%s", owner.Name, strings.ToLower(owner.Kind)), owner
	}
	return pod.Name, nil
}

func getRootOwner(c client.Client, owners []metav1.OwnerReference) *metav1.OwnerReference {
	for _, owner := range owners {
		plog.Info("Processing owner reference", "owner", owner)
		if *owner.Controller && slices.Contains([]string{"CronJob", "DaemonSet", "Deployment", "Job", "ReplicaSet", "StatefulSet"}, owner.Kind) {
			u := &unstructured.Unstructured{}
			u.SetGroupVersionKind(schema.FromAPIVersionAndKind(owner.APIVersion, owner.Kind))
			if err := c.Get(context.Background(), client.ObjectKey{
				Name: owner.Name,
			}, u); k8serrors.IsNotFound(err) {
				plog.Info("Owner not found", "owner", owner.Name)
				return nil
			} else if err != nil {
				panic(errors.New("could not get owner"))
			}
			parent := getRootOwner(c, u.GetOwnerReferences())
			if parent == nil {
				return &owner
			} else {
				return parent
			}
		}
	}
	return nil
}
