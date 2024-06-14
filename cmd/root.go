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

package cmd

import (
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/ontariosystems/ira-controller/internal/util"

	"sigs.k8s.io/controller-runtime/pkg/manager"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"

	v1 "github.com/ontariosystems/ira-controller/api/v1"
	"github.com/ontariosystems/ira-controller/internal/controller"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

type rootFlags struct {
	enableHTTP2          bool
	enableLeaderElection bool
	generateCert         bool
	metricsAddr          string
	probeAddr            string
	secureMetrics        bool
}

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	// +kubebuilder:scaffold:scheme
}

func Execute() {
	mgr, code := configure(addFlags())
	if code != 0 {
		os.Exit(code)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func configure(f *rootFlags) (manager.Manager, int) {
	if v1.CredentialHelperImage == "" {
		setupLog.Error(errors.New("rolesanywhere-credential-helper image not provided"),
			"Please provide an image to use that contains the rolesanywhere-credential-helper")
		return nil, 1
	}

	issuerKinds := []string{cmv1.ClusterIssuerKind, cmv1.IssuerKind}
	if !slices.Contains(issuerKinds, controller.DefaultIssuerKind) {
		setupLog.Error(errors.New("invalid issuer kind"),
			fmt.Sprintf("Please provide a valid issuer kind (%s)", strings.Join(issuerKinds, ",")))
		return nil, 1
	}

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("disabling http/2")
		c.NextProtos = []string{"http/1.1"}
	}

	tlsOpts := []func(*tls.Config){}
	if !f.enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	webhookServer := webhook.NewServer(webhook.Options{
		TLSOpts: tlsOpts,
	})

	mgr, err := ctrl.NewManager(util.GetConfig(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress:   f.metricsAddr,
			SecureServing: f.secureMetrics,
			TLSOpts:       tlsOpts,
		},
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: f.probeAddr,
		LeaderElection:         f.enableLeaderElection,
		LeaderElectionID:       "fe237894.ontsys.com",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		return nil, 1
	}

	if os.Getenv("ENABLE_WEBHOOKS") != "false" {
		podIraInjector := v1.NewPodIraInjector(mgr.GetClient(), mgr.GetScheme())
		mgr.GetWebhookServer().Register("/mutate-core-v1-pod", &webhook.Admission{Handler: podIraInjector})
	}
	if f.generateCert {
		if err = (&controller.PodReconciler{
			Client: mgr.GetClient(),
			Scheme: mgr.GetScheme(),
		}).SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to create controller", "controller", "Pod")
			return nil, 1
		}
		// +kubebuilder:scaffold:builder
	}

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		return nil, 1
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		return nil, 1
	}
	return mgr, 0
}

func addFlags() *rootFlags {
	var f rootFlags
	flag.StringVar(&f.metricsAddr, "metrics-bind-address", "0", "The address the metric endpoint binds to. "+
		"Use the port :8080. If not set, it will be 0 in order to disable the metrics server")
	flag.StringVar(&f.probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&f.enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&f.secureMetrics, "metrics-secure", false,
		"If set the metrics endpoint is served securely")
	flag.BoolVar(&f.enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	flag.BoolVar(&f.generateCert, "generate-cert", false,
		"Use cert-manager to generate a certificate resource to use for authentication")
	flag.StringVar(&v1.CredentialHelperImage, "credential-helper-image", "",
		"The image to use for for the rolesanywhere-credential-helper")
	flag.StringVar(&v1.CredentialHelperCpuRequest, "credential-helper-cpu-request", "250m",
		"The CPU request for the credential-helper")
	flag.StringVar(&v1.CredentialHelperMemoryRequest, "credential-helper-memory-request", "64Mi",
		"The Memory request for the credential-helper")
	flag.StringVar(&v1.CredentialHelperCpuLimit, "credential-helper-cpu-limit", "",
		"The CPU limit for the credential-helper")
	flag.StringVar(&v1.CredentialHelperMemoryLimit, "credential-helper-memory-limit", "128Mi",
		"The Memory limit for the credential-helper")
	flag.StringVar(&v1.SessionDuration, "credential-helper-session-duration", "900",
		"The number of seconds for which the session is valid")
	flag.StringVar(&controller.DefaultIssuerKind, "default-issuer-kind", "ClusterIssuer",
		"The kind of the cert-manager issuer to use as a default when generating a certificate if one isn't specified")
	flag.StringVar(&controller.DefaultIssuerName, "default-issuer-name", "",
		"The name of the cert-manager issuer to use as a default when generating a certificate if one isn't specified")

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	return &f
}
