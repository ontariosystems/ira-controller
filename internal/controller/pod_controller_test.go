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

package controller

import (
	"context"
	"fmt"
	"time"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("Pod Controller", func() {
	var buffer *gbytes.Buffer
	t := true
	BeforeEach(func() {
		buffer = gbytes.NewBuffer()
		GinkgoWriter.TeeTo(buffer)
	})
	AfterEach(func() {
		GinkgoWriter.ClearTeeWriters()
	})
	Context("When reconciling a resource", func() {
		Context("where the resource is a stand-alone pod", func() {
			Context("with a non-existing resource", func() {
				It("should not find the resource", func() {
					_, err := forceReconcile("no-exist")
					Expect(err).ToNot(HaveOccurred())
					Expect(buffer).To(gbytes.Say("Could not find Pod"))
				})
			})

			Context("and it's owner isn't found", func() {
				It("should not find the owner", func() {
					ctx := context.Background()
					pod := &v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "no-ower",
							Namespace: "default",
							OwnerReferences: []metav1.OwnerReference{
								{
									APIVersion: "apps/v1",
									Controller: &t,
									Kind:       "Deployment",
									Name:       "no-exist",
									UID:        types.UID(uuid.New().String()),
								},
							},
						},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Name:  "my-container",
									Image: "my-image",
								},
							},
						},
					}
					Expect(k8sClient.Create(ctx, pod)).To(Succeed())

					Eventually(func() *gbytes.Buffer {
						return buffer
					}, 5*time.Second, 25*time.Millisecond).Should(gbytes.Say("Owner not found"))
				})
			})

			Context("with a resource that is being deleted", func() {
				It("should skip the resource", func() {
					ctx := context.Background()
					pod := &v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "deleting",
							Namespace: "default",
						},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Name:  "my-container",
									Image: "my-image",
									Lifecycle: &v1.Lifecycle{
										PreStop: &v1.LifecycleHandler{
											Exec: &v1.ExecAction{
												Command: []string{"sleep", "3"},
											},
										},
									},
								},
							},
						},
					}
					Expect(k8sClient.Create(ctx, pod)).To(Succeed())
					go func(ctx context.Context, pod *v1.Pod) {
						time.Sleep(2 * time.Second)
						Expect(k8sClient.Delete(ctx, pod)).To(Succeed())
					}(ctx, pod)

					Eventually(func() *gbytes.Buffer {
						return buffer
					}, 5*time.Second, 25*time.Millisecond).Should(gbytes.Say("Skipping terminating pod"))
				})
			})

			Context("without IRA annotations", func() {
				It("should skip the resource", func() {
					ctx := context.Background()
					pod := &v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "unannotated",
							Namespace: "default",
						},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Name:  "my-container",
									Image: "my-image",
								},
							},
						},
					}
					Expect(k8sClient.Create(ctx, pod)).To(Succeed())

					Eventually(func() *gbytes.Buffer {
						return buffer
					}, 5*time.Second, 25*time.Millisecond).Should(gbytes.Say("Skipping unannotated resource"))
				})
			})

			Context("with IRA annotations", func() {
				Context("when the certificate doesn't exist", func() {
					Context("using the generated certificate name", func() {
						It("should create the certificate", func() {
							ctx := context.Background()
							pod := &v1.Pod{
								ObjectMeta: metav1.ObjectMeta{
									Annotations: map[string]string{
										"ira.ontsys.com/trust-anchor": "ta",
										"ira.ontsys.com/profile":      "p",
										"ira.ontsys.com/role":         "c",
									},
									Name:      "annotated",
									Namespace: "default",
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "my-container",
											Image: "my-image",
										},
									},
								},
							}
							Expect(k8sClient.Create(ctx, pod)).To(Succeed())

							Eventually(func() *gbytes.Buffer {
								return buffer
							}, 5*time.Second, 25*time.Millisecond).Should(gbytes.Say("Cert doesn't exist: creating"))

							certificate := &cmv1.Certificate{}
							Eventually(func() bool {
								err := k8sClient.Get(ctx, types.NamespacedName{
									Namespace: "default",
									Name:      "annotated-ira",
								}, certificate)
								return err == nil
							}, 10*time.Second, 25*time.Millisecond).Should(BeTrue())
							Expect(certificate.Name).To(Equal("annotated-ira"))
							Expect(certificate.OwnerReferences[0].Kind).To(Equal("Pod"))
							Expect(certificate.OwnerReferences[0].Name).To(Equal("annotated"))
						})
						Context("when certificate issuer annotations are provided", func() {
							It("should use the certificate issuer", func() {
								ctx := context.Background()
								pod := &v1.Pod{
									ObjectMeta: metav1.ObjectMeta{
										Annotations: map[string]string{
											"ira.ontsys.com/trust-anchor": "ta",
											"ira.ontsys.com/profile":      "p",
											"ira.ontsys.com/role":         "c",
											"ira.ontsys.com/issuer-kind":  "Issuer",
											"ira.ontsys.com/issuer-name":  "i",
										},
										Name:      "issuer",
										Namespace: "default",
									},
									Spec: v1.PodSpec{
										Containers: []v1.Container{
											{
												Name:  "my-container",
												Image: "my-image",
											},
										},
									},
								}
								Expect(k8sClient.Create(ctx, pod)).To(Succeed())

								Eventually(func() *gbytes.Buffer {
									return buffer
								}, 5*time.Second, 25*time.Millisecond).Should(gbytes.Say("Cert doesn't exist: creating"))

								certificate := &cmv1.Certificate{}
								Eventually(func() bool {
									err := k8sClient.Get(ctx, types.NamespacedName{
										Namespace: "default",
										Name:      "issuer-ira",
									}, certificate)
									return err == nil
								}, 10*time.Second, 25*time.Millisecond).Should(BeTrue())
								Expect(certificate.Name).To(Equal("issuer-ira"))
								Expect(certificate.OwnerReferences[0].Kind).To(Equal("Pod"))
								Expect(certificate.OwnerReferences[0].Name).To(Equal("issuer"))
								Expect(certificate.Spec.IssuerRef.Kind).To(Equal(cmv1.IssuerKind))
								Expect(certificate.Spec.IssuerRef.Name).To(Equal("i"))
							})
						})
					})
					Context("using a provided certificate name", func() {
						It("should create the certificate", func() {
							ctx := context.Background()
							pod := &v1.Pod{
								ObjectMeta: metav1.ObjectMeta{
									Annotations: map[string]string{
										"ira.ontsys.com/trust-anchor": "ta",
										"ira.ontsys.com/profile":      "p",
										"ira.ontsys.com/role":         "c",
										"ira.ontsys.com/cert":         "cert-name",
									},
									Name:      "named-cert",
									Namespace: "default",
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "my-container",
											Image: "my-image",
										},
									},
								},
							}
							Expect(k8sClient.Create(ctx, pod)).To(Succeed())

							Eventually(func() *gbytes.Buffer {
								return buffer
							}, 5*time.Second, 25*time.Millisecond).Should(gbytes.Say("Cert doesn't exist: creating"))

							certificate := &cmv1.Certificate{}
							Eventually(func() bool {
								err := k8sClient.Get(ctx, types.NamespacedName{
									Namespace: "default",
									Name:      "cert-name",
								}, certificate)
								return err == nil
							}, 10*time.Second, 25*time.Millisecond).Should(BeTrue())
							Expect(certificate.Name).To(Equal("cert-name"))
							Expect(certificate.OwnerReferences[0].Kind).To(Equal("Pod"))
							Expect(certificate.OwnerReferences[0].Name).To(Equal("named-cert"))

						})
					})
					Context("when the pod name is too long for the common name", func() {
						It("should truncate the common name to 64 characters", func() {
							ctx := context.Background()
							pod := &v1.Pod{
								ObjectMeta: metav1.ObjectMeta{
									Annotations: map[string]string{
										"ira.ontsys.com/trust-anchor": "ta",
										"ira.ontsys.com/profile":      "p",
										"ira.ontsys.com/role":         "c",
									},
									Name:      "this-is-a-really-long-pod-name-that-will-cause-a-failure-when-creating-the-cert",
									Namespace: "default",
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "my-container",
											Image: "my-image",
										},
									},
								},
							}
							Expect(k8sClient.Create(ctx, pod)).To(Succeed())

							Eventually(func() *gbytes.Buffer {
								return buffer
							}, 5*time.Second, 25*time.Millisecond).Should(gbytes.Say("Cert doesn't exist: creating"))

							certificate := &cmv1.Certificate{}
							Eventually(func() bool {
								err := k8sClient.Get(ctx, types.NamespacedName{
									Namespace: "default",
									Name:      "this-is-a-really-long-pod-name-that-will-cause-a-failure-when-creating-the-cert-ira",
								}, certificate)
								return err == nil
							}, 10*time.Second, 25*time.Millisecond).Should(BeTrue())
							Expect(certificate.Name).To(Equal("this-is-a-really-long-pod-name-that-will-cause-a-failure-when-creating-the-cert-ira"))
							Expect(certificate.Spec.CommonName).To(Equal("default/this-is-a-really-long-pod-name-that-will-cause-a-failure"))
							Expect(certificate.OwnerReferences[0].Kind).To(Equal("Pod"))
							Expect(certificate.OwnerReferences[0].Name).To(Equal("this-is-a-really-long-pod-name-that-will-cause-a-failure-when-creating-the-cert"))

						})
					})
				})

				Context("when the certificate does exist", func() {
					It("should update the certificate", func() {
						ctx := context.Background()
						cert := &cmv1.Certificate{
							ObjectMeta: metav1.ObjectMeta{
								Name:      "existing-cert-ira",
								Namespace: "default",
							},
							Spec: cmv1.CertificateSpec{
								CommonName: fmt.Sprintf("IRA exists: %s/%s ", "default", "existing-cert"),
								IssuerRef: cmmeta.ObjectReference{
									Name:  "cluster-ira-ca",
									Kind:  cmv1.ClusterIssuerKind,
									Group: "cert-manager.io",
								},
								SecretName: "existing-cert-ira",
								PrivateKey: &cmv1.CertificatePrivateKey{
									Algorithm: cmv1.RSAKeyAlgorithm,
									Size:      8192,
								},
							},
						}
						Expect(k8sClient.Create(ctx, cert)).To(Succeed())

						pod := &v1.Pod{
							ObjectMeta: metav1.ObjectMeta{
								Annotations: map[string]string{
									"ira.ontsys.com/trust-anchor": "ta",
									"ira.ontsys.com/profile":      "p",
									"ira.ontsys.com/role":         "c",
								},
								Name:      "existing-cert",
								Namespace: "default",
							},
							Spec: v1.PodSpec{
								Containers: []v1.Container{
									{
										Name:  "my-container",
										Image: "my-image",
									},
								},
							},
						}
						Expect(k8sClient.Create(ctx, pod)).To(Succeed())

						Eventually(func() *gbytes.Buffer {
							return buffer
						}, 5*time.Second, 25*time.Millisecond).Should(gbytes.Say("Found certificate"))

						certificate := &cmv1.Certificate{}
						Eventually(func() bool {
							err := k8sClient.Get(ctx, types.NamespacedName{
								Namespace: "default",
								Name:      "existing-cert-ira",
							}, certificate)
							return err == nil
						}, 10*time.Second, 25*time.Millisecond).Should(BeTrue())
						Expect(certificate.Name).To(Equal("existing-cert-ira"))
						Expect(certificate.OwnerReferences[0].Kind).To(Equal("Pod"))
						Expect(certificate.OwnerReferences[0].Name).To(Equal("existing-cert"))
						Expect(certificate.Spec.CommonName).To(Equal(fmt.Sprintf("%s/%s", "default", "existing-cert")))
					})
				})
			})
		})

		Context("where the controlling resource is a deployment", func() {
			Context("with IRA annotations in the template spec", func() {
				It("should create a certificate owned by the deployment", func() {
					ctx := context.Background()
					deployment := appsv1.Deployment{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "deploy",
							Namespace: "default",
							UID:       types.UID(uuid.New().String()),
						},
						Spec: appsv1.DeploymentSpec{
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"app": "my-app",
								},
							},
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Annotations: map[string]string{
										"ira.ontsys.com/trust-anchor": "ta",
										"ira.ontsys.com/profile":      "p",
										"ira.ontsys.com/role":         "c",
									},
									Labels: map[string]string{
										"app": "my-app",
									},
									Name:      "existing-cert",
									Namespace: "default",
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "my-container",
											Image: "my-image",
										},
									},
								},
							},
						},
					}
					Expect(k8sClient.Create(ctx, &deployment)).To(Succeed())

					replicaSet := appsv1.ReplicaSet{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "deploy-76b849fb6c",
							Namespace: "default",
							OwnerReferences: []metav1.OwnerReference{
								{
									APIVersion: "apps/v1",
									Controller: &t,
									Kind:       "Deployment",
									Name:       deployment.Name,
									UID:        deployment.UID,
								},
							},
							UID: types.UID(uuid.New().String()),
						},
						Spec: appsv1.ReplicaSetSpec{
							Selector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"app": "my-app",
								},
							},
							Template: v1.PodTemplateSpec{
								ObjectMeta: metav1.ObjectMeta{
									Annotations: map[string]string{
										"ira.ontsys.com/trust-anchor": "ta",
										"ira.ontsys.com/profile":      "p",
										"ira.ontsys.com/role":         "c",
									},
									Labels: map[string]string{
										"app": "my-app",
									},
									Name:      "existing-cert",
									Namespace: "default",
								},
								Spec: v1.PodSpec{
									Containers: []v1.Container{
										{
											Name:  "my-container",
											Image: "my-image",
										},
									},
								},
							},
						},
					}
					Expect(k8sClient.Create(ctx, &replicaSet)).To(Succeed())

					pod := &v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"ira.ontsys.com/trust-anchor": "ta",
								"ira.ontsys.com/profile":      "p",
								"ira.ontsys.com/role":         "c",
							},
							Labels: map[string]string{
								"app": "my-app",
							},
							Name:      "deploy-76b849fb6c-dkmgf",
							Namespace: "default",
							OwnerReferences: []metav1.OwnerReference{
								{
									APIVersion: "apps/v1",
									Controller: &t,
									Kind:       "ReplicaSet",
									Name:       replicaSet.Name,
									UID:        replicaSet.UID,
								},
							},
							UID: types.UID(uuid.New().String()),
						},
						Spec: v1.PodSpec{
							Containers: []v1.Container{
								{
									Name:  "my-container",
									Image: "my-image",
								},
							},
						},
					}
					Expect(k8sClient.Create(ctx, pod)).To(Succeed())

					Eventually(func() *gbytes.Buffer {
						return buffer
					}, 5*time.Second, 25*time.Millisecond).Should(gbytes.Say("Cert doesn't exist: creating"))

					certificate := &cmv1.Certificate{}
					Eventually(func() bool {
						err := k8sClient.Get(ctx, types.NamespacedName{
							Namespace: "default",
							Name:      "deploy-deployment-ira",
						}, certificate)
						return err == nil
					}, 10*time.Second, 25*time.Millisecond).Should(BeTrue())
					Expect(certificate.Name).To(Equal("deploy-deployment-ira"))
					Expect(certificate.OwnerReferences[0].Kind).To(Equal("Deployment"))
					Expect(certificate.OwnerReferences[0].Name).To(Equal("deploy"))
				})
			})
		})
	})
})

func forceReconcile(podName string) (ctrl.Result, error) {
	reconciler := &PodReconciler{
		Client: k8sClient,
	}

	return reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "default",
			Name:      podName,
		},
	})
}
