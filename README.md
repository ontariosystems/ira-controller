# ira-controller
[![Latest Release](https://img.shields.io/github/release/ontariosystems/ira-controller.svg)](https://github.com/ontariosystems/ira-controller/releases)
[![CLA assistant](https://cla-assistant.io/readme/badge/ontariosystems/ira-controller)](https://cla-assistant.io/ontariosystems/ira-controller)
[![Build Status](https://github.com/ontariosystems/ira-controller/actions/workflows/build.yml/badge.svg)](https://github.com/ontariosystems/ira-controller/actions/workflows/build.yml)
[![codecov](https://codecov.io/gh/ontariosystems/ira-controller/graph/badge.svg?token=BKCN24MEUK)](https://codecov.io/gh/ontariosystems/ira-controller)
[![Go Report Card](https://goreportcard.com/badge/github.com/ontariosystems/ira-controller)](https://goreportcard.com/report/github.com/ontariosystems/ira-controller)
[![GoDoc](https://godoc.org/github.com/ontariosystems/ira-controller?status.svg)](https://godoc.org/github.com/ontariosystems/ira-controller)

`ira-controler` is a mutating webhook and pod controller for Kubernetes, facilitating the use of [aws/rolesanywhere-credential-helper](https://github.com/aws/rolesanywhere-credential-helper) as a sidecar for your application that needs to use [IAM Roles Anywhere](https://docs.aws.amazon.com/rolesanywhere/latest/APIReference/Welcome.html) to access AWS resources.  

## Description
Before you begin using this project you should be familiar with the [concepts](https://docs.aws.amazon.com/rolesanywhere/latest/userguide/introduction.html) of IAM Roles Anywhere and have configured the required AWS resources.

### Mutating Webhook
The mutating webhook provided by this project is watching the creation/updating of pods and will inject a rolesanywhere credential helper sidecar if the required annotations are provided.
This sidecar will be configured based on the following annotations on the pod.

| Annotation                  | Description                                                                                                                                                                                                                                                                                     |
|-----------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| ira.ontsys.com/trust-anchor | The ARN of the IAM Roles Anywhere trust anchor to use for obtaining credentials.                                                                                                                                                                                                                |
| ira.ontsys.com/profile      | The ARN of the IAM Roles Anywhere profile to use for obtaining credentials.  This profile must contain the IAM role specified in `ira.ontsys.com/role`                                                                                                                                          |
| ira.ontsys.com/role         | The ARN of the IAM role to be assumed to gain credentials.                                                                                                                                                                                                                                      |
| ira.ontsys.com/cert         | Either the name of a TLS secret containing a certificate that was issued by the CA configured in trust anchor or when used in conjunction with the controller the optional name to use to create a [cert-manager](https://cert-manager.io/) certificate that will be created by the controller. |

### Pod Controller
The pod controller is optional and if desired must be turned on using the `--generate-cert` command-line flag.
Once enabled the controller will trigger based on the same annotations as the webhook and create a [certificate resource](https://cert-manager.io/docs/usage/certificate/).
The CA behind the cert-manager issuer needs to be the CA that is configured in the trust anchor in order to successfully obtain credentials.
The TLS secret that is generated from this certificate will then be the one used to by the webhook for authentication.

| Annotation                 | Description                                                                                                                                                             |
|----------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| ira.ontsys.com/issuer-kind | The kind of cert-manager issuer that should be used to issue the certificate. If not provided the value of `--default-issuer-kind` will be used.                        |
| ira.ontsys.com/issuer-name | The name of the issuer that should be used to issue the certificate. If not provided the value of `--default-issuer-name` will be used.                                 |
| ira.ontsys.com/cert        | The optional name to use when creating a cert-manager certificate. If not provided the certificate name will be generated based on the controlling resource of the pod. |

**NOTE:** If neither the `ira.ontsys.com/issuer-name` annotation or the `--default-issuer-name` command line flag are provided then the certificate will fail to be created.

## Getting Started

### Prerequisites
- go version v1.22.0+
- docker version 26.1+.
- kubectl version v1.29.0+.
- helm version v3.15.2+.
- Access to a Kubernetes v1.29.0+ cluster.

This project may work on older versions but has not been tested.

### Command Line Options
The command line is self documenting.  For a full list of options run `ira-controller --help`.

The options that start with `credential-helper` are all related to the rolesanywhere-credential-helper that is being injected.
The `--credential-helper-image` flag is required as the project currently doesn't publish an official image.
They have a [GitHub issue](https://github.com/aws/rolesanywhere-credential-helper/issues/51) for discussing the possibility of adding one.

## To Deploy on the cluster
### Install with helm

Add the ira-controller Helm repository:
```sh
helm repo add ira-controller https://ontariosystems.github.io/ira-controller
helm repo update
```

Then install a release using the chart.  The [charts default values file](charts/ira-controller/values.yaml) provides some commented out examples for setting some of the values.  There are several required values, but helm should fail with messages that indicate which value is missing.
```sh
helm upgrade --install --namespace ira-controller-system ira-controller \
    ira-controller/ira-controller --values <<name_of_your_values_file>>.yaml
```

### Build an image and deploy using kustomize

**Build and push your image to the location specified by `IMG`:**

```sh
make docker-build docker-push IMG=<some-registry>/ira-controller:tag
```

**NOTE:** This image ought to be published in the personal registry you specified.
And it is required to have access to pull the image from the working environment.
Make sure you have the proper permission to the registry if the above commands don’t work.

**Deploy the Manager to the cluster with the image specified by `IMG`:**

```sh
make deploy IMG=<some-registry>/ira-controller:tag
```

**UnDeploy the controller from the cluster:**

```sh
make undeploy
```

## Project Distribution

Following are the steps to build the installer and distribute this project to users.

1. Build the installer for the image built and published in the registry:

```sh
make build-installer IMG=<some-registry>/ira-controller:tag
```

NOTE: The makefile target mentioned above generates an 'install.yaml'
file in the dist directory. This file contains all the resources built
with Kustomize, which are necessary to install this project without
its dependencies.

2. Using the installer

Users can just run kubectl apply -f <URL for YAML BUNDLE> to install the project, i.e.:

```sh
kubectl apply -f dist/install.yaml
```

## Contributing
View our [contributing policy](CONTRIBUTING.md).


## Additional Information
**NOTE:** Run `make help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

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

