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
	"fmt"
	"net/http"
	"slices"

	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/ontariosystems/ira-controller/internal/util"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/json"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// log is for logging in this package.
var (
	podlog                                                 = logf.Log.WithName("pod-resource")
	_                             webhook.AdmissionHandler = &podIraInjector{}
	CredentialHelperImage         string
	CredentialHelperCpuRequest    string
	CredentialHelperMemoryRequest string
	CredentialHelperCpuLimit      string
	CredentialHelperMemoryLimit   string
	SessionDuration               string
)

// +kubebuilder:webhook:path=/mutate-core-v1-pod,mutating=true,failurePolicy=fail,sideEffects=None,groups=core,resources=pods,verbs=create;update,versions=v1,name=mpod.kb.io,admissionReviewVersions=v1

// podIraInjector struct used to handle admission control for Kubernetes pods
type podIraInjector struct {
	Client  client.Client
	decoder admission.Decoder
}

// NewPodIraInjector initializes and returns a new pod injector to handle webhook calls
func NewPodIraInjector(client client.Client, scheme *runtime.Scheme) admission.Handler {
	return &podIraInjector{
		Client:  client,
		decoder: admission.NewDecoder(scheme),
	}
}

// Handle processes any required mutations to the incoming pod
func (p *podIraInjector) Handle(ctx context.Context, request admission.Request) admission.Response {

	pod := &v1.Pod{}

	err := p.decoder.Decode(request, pod)
	if err != nil {
		podlog.Error(err, "error occurred while decoding the admission request")
		return admission.Errored(http.StatusBadRequest, err)
	}

	podlog.Info("handling the pod CREATE/UPDATE event for", "pod name", pod.Name, "pod namespace", pod.Namespace, "pod generate name", pod.GenerateName)

	if !pod.DeletionTimestamp.IsZero() {
		podlog.Info("Skipping terminating pod")
		return admission.Allowed("pod terminating")
	}

	if slices.Contains([]v1.PodPhase{v1.PodFailed, v1.PodSucceeded}, pod.Status.Phase) {
		podlog.Info("Skipping finished pod")
		return admission.Allowed("pod finished")
	}

	if util.MapContains(pod.Annotations, "ira.ontsys.com/trust-anchor") && util.MapContains(pod.Annotations, "ira.ontsys.com/profile") && util.MapContains(pod.Annotations, "ira.ontsys.com/role") {
		if pod.Spec.Volumes == nil {
			pod.Spec.Volumes = make([]v1.Volume, 0)
		}
		secretName, _ := util.ControllerNameFromPod(pod)
		pod.Spec.Volumes = append(pod.Spec.Volumes, v1.Volume{
			Name: "ira-cert",
			VolumeSource: v1.VolumeSource{
				Secret: &v1.SecretVolumeSource{
					SecretName: util.GetCertName(pod.Annotations, secretName),
				},
			},
		})

		endpoint := "http://127.0.0.1:9911"
		if util.MapContains(pod.Annotations, "ira.ontsys.com/metadata-endpoint-trailing-slash") && pod.Annotations["ira.ontsys.com/metadata-endpoint-trailing-slash"] != "" {
			endpoint += "/"
		}

		for i, c := range pod.Spec.Containers {
			if c.Env == nil {
				c.Env = make([]v1.EnvVar, 0)
			}
			c.Env = append(c.Env, v1.EnvVar{
				Name:  "AWS_EC2_METADATA_SERVICE_ENDPOINT",
				Value: endpoint,
			})
			pod.Spec.Containers[i] = c
		}

		restartPolicyAlways := v1.ContainerRestartPolicyAlways
		resources := v1.ResourceRequirements{
			Limits:   v1.ResourceList{},
			Requests: v1.ResourceList{},
		}
		if CredentialHelperCpuRequest != "" {
			resources.Requests[v1.ResourceCPU] = resource.MustParse(CredentialHelperCpuRequest)
		}
		if CredentialHelperCpuLimit != "" {
			resources.Limits[v1.ResourceCPU] = resource.MustParse(CredentialHelperCpuLimit)
		}
		if CredentialHelperMemoryRequest != "" {
			resources.Requests[v1.ResourceMemory] = resource.MustParse(CredentialHelperMemoryRequest)
		}
		if CredentialHelperMemoryLimit != "" {
			resources.Limits[v1.ResourceMemory] = resource.MustParse(CredentialHelperMemoryLimit)
		}

		pod.Spec.InitContainers = append(pod.Spec.InitContainers, v1.Container{
			Name:    "ira",
			Image:   CredentialHelperImage,
			Command: []string{"aws_signing_helper"},
			Args: []string{
				"serve",
				"--certificate",
				"/ira-cert/tls.crt",
				"--private-key",
				"/ira-cert/tls.key",
				"--trust-anchor-arn",
				pod.Annotations["ira.ontsys.com/trust-anchor"],
				"--profile-arn",
				pod.Annotations["ira.ontsys.com/profile"],
				"--role-arn",
				pod.Annotations["ira.ontsys.com/role"],
				fmt.Sprintf("'--session-duration=%s'", SessionDuration),
			},
			RestartPolicy: &restartPolicyAlways,
			Resources:     resources,
			VolumeMounts: []v1.VolumeMount{
				{
					Name:      "ira-cert",
					MountPath: "/ira-cert",
				},
			},
		})
	}

	marshaledpod, err := json.Marshal(pod)

	if err != nil {
		return admission.Errored(http.StatusInternalServerError, err)
	}

	podlog.Info("Attempting to patch pod", "pod", pod.Name, "pod namespace", pod.Namespace, "pod generate name", pod.GenerateName)

	return admission.PatchResponseFromRaw(request.AdmissionRequest.Object.Raw, marshaledpod)
}

// InjectDecoder injects the decoder.
func (p *podIraInjector) InjectDecoder(d admission.Decoder) error {
	p.decoder = d
	return nil
}
