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
	"context"

	log "github.com/sirupsen/logrus"

	corev1 "k8s.io/api/core/v1"
)

var ()

const ()

// createPodSpecs returns a list of pods to be applied to the targeted nodes
// in the cluster.
func createPodSpecs(ctx context.Context, nodes []corev1.Node) chan []corev1.Pod {

	podSpecsCompleted := make(chan []corev1.Pod)

	podSpecList := make([]corev1.Pod, 0)

	go func() {
		log.Infoln("Creating a list of pod specifications for the qualifying targeted nodes.")

		defer close(podSpecsCompleted)

		// Create a pod specification for each node in the list.
		for _, node := range nodes {
			// Skip unschedulable nodes
			if node.Spec.Unschedulable {

				continue
			}

			// // Look for nodes with valid selectors
			// for specifiedKey, specifiedValue := range checkNodeSelectors {
			// 	var nodeLabelValue string
			// 	var ok bool

			// 	// Skip the node if one of the label keys is missing
			// 	if nodeLabelValue, ok = node.ObjectMeta.Labels[specifiedKey]; !ok {
			// 		continue
			// 	}

			// 	// Skip the node if the value on the label does not match
			// 	if specifiedValue != nodeLabelValue {
			// 		continue
			// 	}

			// 	// If the node passes all the node selectors and labels, create a
			// 	// specification for it and add it to the list
			// 	podSpecList = append(podSpecList, createPodSpec(node))
			// }
			// podSpecList = append(podSpecList, createPodSpec(node))
		}

		// Return this list of pod specifications
		podSpecsCompleted <- podSpecList
		return
	}()

	return podSpecsCompleted
}

// // createPodSpec returns a pod spec for the specified node.
// func createPodSpec(node corev1.Node) corev1.Pod {
// 	pod := corev1.Pod{}

// 	pod.Labels = node.ObjectMeta.Labels
// 	pod.Name = checkName + "-" + node.ObjectMeta.Name
// 	pod.Namespace = checkNamespace

// 	// Make a container for the node.
// 	container := createContainerConfig(image)
// 	pod.Spec.Containers =
// }
