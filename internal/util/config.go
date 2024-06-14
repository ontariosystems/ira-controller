package util

import "sigs.k8s.io/controller-runtime/pkg/client/config"

var GetConfig = config.GetConfigOrDie
