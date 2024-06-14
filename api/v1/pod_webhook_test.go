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

package v1

import (
	"context"
	"time"

	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Pod Webhook", func() {
	var buffer *gbytes.Buffer
	BeforeEach(func() {
		buffer = gbytes.NewBuffer()
		GinkgoWriter.TeeTo(buffer)
		CredentialHelperImage = "test-image:latest"
		CredentialHelperCpuRequest = "250m"
		CredentialHelperMemoryRequest = "64Mi"
		CredentialHelperMemoryLimit = "128Mi"
		SessionDuration = "900"
	})
	AfterEach(func() {
		GinkgoWriter.ClearTeeWriters()
		CredentialHelperCpuLimit = ""
	})

	Context("When creating Pod under Mutating Webhook", func() {
		Context("with a pod that is finished", func() {
			It("should skip the resource", func() {
				ctx := context.Background()
				pod := &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "finished",
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
					Status: v1.PodStatus{Phase: v1.PodSucceeded},
				}
				Expect(k8sClient.Create(ctx, pod)).To(Succeed())

				Eventually(func() *gbytes.Buffer {
					return buffer
				}, 10*time.Second, 25*time.Millisecond).Should(gbytes.Say("Skipping finished pod"))
			})
		})
		Context("with IRA annotations", func() {
			It("should mutate the pod", func() {
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
				}, 5*time.Second, 25*time.Millisecond).Should(gbytes.Say("Attempting to patch pod"))

				mutatedPod := &v1.Pod{}
				Eventually(func() bool {
					err := k8sClient.Get(ctx, types.NamespacedName{
						Namespace: "default",
						Name:      "annotated",
					}, mutatedPod)
					return err == nil
				}, 10*time.Second, 25*time.Millisecond).Should(BeTrue())
				Expect(mutatedPod.Spec.Volumes).To(ContainElement(HaveField("Name", Equal("ira-cert"))))
				Expect(mutatedPod.Spec.Volumes).To(ContainElement(HaveField("VolumeSource.Secret.SecretName", "annotated-ira")))
				Expect(mutatedPod.Spec.Containers).To(HaveExactElements(HaveField("Env", ContainElement(v1.EnvVar{
					Name:  "AWS_EC2_METADATA_SERVICE_ENDPOINT",
					Value: "http://127.0.0.1:9911/",
				}))))
				Expect(mutatedPod.Spec.InitContainers).To(ContainElement(HaveField("Name", Equal("ira"))))
				Expect(mutatedPod.Spec.InitContainers).To(ContainElement(HaveField("VolumeMounts", ContainElement(v1.VolumeMount{
					Name:      "ira-cert",
					MountPath: "/ira-cert",
				}))))
				Expect(mutatedPod.Spec.InitContainers).To(ContainElement(HaveField("Args", ContainElements("ta", "p", "c"))))
				Expect(mutatedPod.Spec.InitContainers).To(ContainElement(HaveField("Resources", v1.ResourceRequirements{
					Limits: v1.ResourceList{
						v1.ResourceMemory: resource.MustParse("128Mi"),
					},
					Requests: v1.ResourceList{
						v1.ResourceCPU:    resource.MustParse("250m"),
						v1.ResourceMemory: resource.MustParse("64Mi"),
					},
				})))
				Expect(mutatedPod.Spec.InitContainers).To(Not(ContainElement(HaveField("Resources", v1.ResourceRequirements{
					Limits: v1.ResourceList{
						v1.ResourceCPU: resource.MustParse("500m"),
					},
				}))))
			})
			Context("when a CPU limit is provided", func() {
				It("should use the CPU limit", func() {
					CredentialHelperCpuLimit = "500m"
					ctx := context.Background()
					pod := &v1.Pod{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"ira.ontsys.com/trust-anchor": "ta",
								"ira.ontsys.com/profile":      "p",
								"ira.ontsys.com/role":         "c",
							},
							Name:      "limited",
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
					}, 5*time.Second, 25*time.Millisecond).Should(gbytes.Say("Attempting to patch pod"))

					mutatedPod := &v1.Pod{}
					Eventually(func() bool {
						err := k8sClient.Get(ctx, types.NamespacedName{
							Namespace: "default",
							Name:      "limited",
						}, mutatedPod)
						return err == nil
					}, 10*time.Second, 25*time.Millisecond).Should(BeTrue())
					Expect(mutatedPod.Spec.InitContainers).To(ContainElement(HaveField("Resources", v1.ResourceRequirements{
						Limits: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("500m"),
							v1.ResourceMemory: resource.MustParse("128Mi"),
						},
						Requests: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("250m"),
							v1.ResourceMemory: resource.MustParse("64Mi"),
						},
					})))
				})
			})
			Context("using a provided certificate name", func() {
				It("should mutate the pod using the provided certificate name", func() {
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
					}, 5*time.Second, 25*time.Millisecond).Should(gbytes.Say("Attempting to patch pod"))

					mutatedPod := &v1.Pod{}
					Eventually(func() bool {
						err := k8sClient.Get(ctx, types.NamespacedName{
							Namespace: "default",
							Name:      "named-cert",
						}, mutatedPod)
						return err == nil
					}, 10*time.Second, 25*time.Millisecond).Should(BeTrue())
					Expect(mutatedPod.Spec.Volumes).To(ContainElement(HaveField("VolumeSource.Secret.SecretName", "cert-name")))
				})
			})
		})
	})
})
