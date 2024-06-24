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
	"fmt"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	cmclient "github.com/cert-manager/cert-manager/pkg/client/clientset/versioned"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// GetCertName returns the name that should be used for the certificate/secret based on an annotation on the pod or the root owner of the pod's name
func GetCertName(podAnnotations map[string]string, resourceControllerName string) (certName string) {
	if MapContains(podAnnotations, "ira.ontsys.com/cert") {
		certName = podAnnotations["ira.ontsys.com/cert"]
	} else {
		certName = fmt.Sprintf("%s-ira", resourceControllerName)
	}
	return
}

// GenerateCertificate creates/updates a certificate resource to be used for authentication
func GenerateCertificate(ctx context.Context, annotations map[string]string, name string, namespace string, ownerReference *metav1.OwnerReference, issuerKind string, issuerName string) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	if MapContains(annotations, "ira.ontsys.com/trust-anchor") && MapContains(annotations, "ira.ontsys.com/profile") && MapContains(annotations, "ira.ontsys.com/role") {
		log.Info("Found resource with annotations", "controller name", name)
		certName := GetCertName(annotations, name)
		config := GetConfig()
		clientset, err := cmclient.NewForConfig(config)
		if err != nil {
			return reconcile.Result{}, err
		}
		cmClient := clientset.CertmanagerV1().Certificates(namespace)

		certificate := &cmv1.Certificate{
			ObjectMeta: metav1.ObjectMeta{
				Name:      certName,
				Namespace: namespace,
			},
			Spec: cmv1.CertificateSpec{
				CommonName: getCommonName(name, namespace),
				IssuerRef: cmmeta.ObjectReference{
					Name:  issuerName,
					Kind:  issuerKind,
					Group: "cert-manager.io",
				},
				SecretName: certName,
				PrivateKey: &cmv1.CertificatePrivateKey{
					Algorithm: cmv1.RSAKeyAlgorithm,
					Size:      8192,
				},
			},
		}

		if ownerReference != nil {
			certificate.ObjectMeta.OwnerReferences = []metav1.OwnerReference{*ownerReference}
		}

		foundCertificate, err := cmClient.Get(ctx, certName, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			log.Info("Cert doesn't exist: creating", "error", err)
			if _, err := cmClient.Create(ctx, certificate, metav1.CreateOptions{}); err != nil {
				return reconcile.Result{}, err
			}
		} else if err != nil {
			return reconcile.Result{}, err
		} else {
			log.Info("Found certificate", "calculated cert name", certName, "found cert", foundCertificate)
			certificate.ObjectMeta.SetResourceVersion(foundCertificate.ObjectMeta.GetResourceVersion())

			if _, err := cmClient.Update(ctx, certificate, metav1.UpdateOptions{}); err != nil {
				return reconcile.Result{}, err
			}
		}
	} else {
		log.Info("Skipping unannotated resource")
	}
	return ctrl.Result{}, nil
}

func getCommonName(name string, namespace string) string {
	commonName := fmt.Sprintf("%s/%s", namespace, name)
	if len(commonName) < 65 {
		return commonName
	}
	return commonName[:64]
}
