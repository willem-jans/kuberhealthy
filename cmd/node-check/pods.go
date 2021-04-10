// Copyright 2018 Comcast Cable Communications Management, LLC
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//     http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"strconv"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var ()

const (
	// Default port to use on the container
	checkContainerPort = 80

	// Default container probe values.
	defaultProbeFailureThreshold    = 5  // Number of consecutive failures for the probe to be considered failed (k8s default = 3).
	defaultProbeSuccessThreshold    = 1  // Number of consecutive successes for the probe to be considered successful after having failed (k8s default = 1).
	defaultProbeInitialDelaySeconds = 2  // Number of seconds after container has started before probes are initiated.
	defaultProbeTimeoutSeconds      = 2  // Number of seconds after which the probe times out (k8s default = 1).
	defaultProbePeriodSeconds       = 15 // How often to perform the probe (k8s default = 10).
)

// createPodSpecs returns a list of pods to be applied to the targeted nodes
// in the cluster.
// func createPodSpecs(ctx context.Context, nodes []corev1.Node) chan []corev1.Pod {
func createPodSpecs(nodes []corev1.Node) []corev1.Pod {
	podSpecs := make([]corev1.Pod, 0)

	for _, node := range nodes {
		podSpecs = append(podSpecs, createPodSpec(node))
	}

	return podSpecs
}

// createPodSpec returns a pod spec for the specified node.
func createPodSpec(node corev1.Node) corev1.Pod {
	// Make a pod.
	pod := corev1.Pod{}

	// Make a container for the pod.
	containers := make([]v1.Container, 0)
	container := createContainerConfig(checkImage)
	containers = append(containers, container)
	pod.Spec.Containers = containers

	// Set the pod nodename to the name of the node (schedules the pod on that node)
	pod.Spec.NodeName = node.GetName()

	// Make labels for pod and deployment.
	labels := make(map[string]string, 0)
	labels[defaultCheckLabelKey] = defaultCheckLabelValueBase + strconv.Itoa(int(now.Unix()))
	labels["source"] = "kuberhealthy"
	pod.Labels = labels

	// Set pod metadata.
	pod.Name = checkName + "-" + node.ObjectMeta.Name
	pod.Namespace = checkNamespace

	return pod
}

// createContainerConfig returns a kubernetes container struct.
func createContainerConfig(image string) v1.Container {

	// Set up a basic container port [default is 80 for HTTP].
	basicPort := corev1.ContainerPort{
		ContainerPort: checkContainerPort,
	}
	containerPorts := []corev1.ContainerPort{basicPort}

	// Make maps for resources.
	// Make and define a map for requests.
	requests := make(map[corev1.ResourceName]resource.Quantity, 0)
	requests[corev1.ResourceCPU] = *resource.NewMilliQuantity(int64(millicoreRequest), resource.DecimalSI)
	requests[corev1.ResourceMemory] = *resource.NewQuantity(int64(memoryRequest), resource.BinarySI)

	// Make and define a map for limits.
	limits := make(map[corev1.ResourceName]resource.Quantity, 0)
	limits[corev1.ResourceCPU] = *resource.NewMilliQuantity(int64(millicoreLimit), resource.DecimalSI)
	limits[corev1.ResourceMemory] = *resource.NewQuantity(int64(memoryLimit), resource.BinarySI)

	resources := v1.ResourceRequirements{
		Requests: requests,
		Limits:   limits,
	}

	// Make a TCP socket for the probe handler.
	tcpSocket := corev1.TCPSocketAction{
		Port: intstr.IntOrString{
			IntVal: checkContainerPort,
			StrVal: strconv.Itoa(int(checkContainerPort)),
		},
	}

	// Make a handler for the probes.
	handler := corev1.Handler{
		TCPSocket: &tcpSocket,
	}

	// Make liveness and readiness probes.
	// Make the liveness probe here.
	liveProbe := corev1.Probe{
		Handler:             handler,
		InitialDelaySeconds: defaultProbeInitialDelaySeconds,
		TimeoutSeconds:      defaultProbeTimeoutSeconds,
		PeriodSeconds:       defaultProbePeriodSeconds,
		SuccessThreshold:    defaultProbeSuccessThreshold,
		FailureThreshold:    defaultProbeFailureThreshold,
	}

	// Make the readiness probe here.
	readyProbe := corev1.Probe{
		Handler:             handler,
		InitialDelaySeconds: defaultProbeInitialDelaySeconds,
		TimeoutSeconds:      defaultProbeTimeoutSeconds,
		PeriodSeconds:       defaultProbePeriodSeconds,
		SuccessThreshold:    defaultProbeSuccessThreshold,
		FailureThreshold:    defaultProbeFailureThreshold,
	}

	// Create the container.
	return corev1.Container{
		Name:            defaultCheckName,
		Image:           image,
		ImagePullPolicy: v1.PullIfNotPresent,
		Ports:           containerPorts,
		Resources:       resources,
		LivenessProbe:   &liveProbe,
		ReadinessProbe:  &readyProbe,
	}
}
