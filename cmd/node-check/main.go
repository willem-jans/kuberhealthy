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
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	kh "github.com/Comcast/kuberhealthy/v2/pkg/checks/external/checkclient"
	"github.com/Comcast/kuberhealthy/v2/pkg/kubeClient"
	log "github.com/sirupsen/logrus"
	"k8s.io/client-go/kubernetes"
	v1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

var (
	// K8s config file for the client.
	kubeConfigFile = filepath.Join(os.Getenv("HOME"), ".kube", "config")

	// The name for the spawned check pods.
	checkNameEnv = os.Getenv("CHECK_NAME")
	checkName    string

	// Namespace the check deployment will be created in [default = kuberhealthy].
	checkNamespaceEnv = os.Getenv("CHECK_NAMESPACE")
	checkNamespace    string

	// // Toleration values for the deployment check
	// checkDeploymentTolerationsEnv = os.Getenv("TOLERATIONS")
	// checkDeploymentTolerations    []apiv1.Toleration

	// Node selectors for the deployment check
	checkNodeSelectorsEnv = os.Getenv("NODE_SELECTOR")
	// checkNodeSelectors    = make(map[string]string)

	// Deployment pod resource requests and limits.
	millicoreRequestEnv = os.Getenv("CHECK_POD_CPU_REQUEST")
	millicoreRequest    int

	millicoreLimitEnv = os.Getenv("CHECK_POD_CPU_LIMIT")
	millicoreLimit    int

	memoryRequestEnv = os.Getenv("CHECK_POD_MEM_REQUEST")
	memoryRequest    int

	memoryLimitEnv = os.Getenv("CHECK_POD_MEM_LIMIT")
	memoryLimit    int

	// Check time limit.
	checkTimeLimit time.Duration

	// // Additional container environment variables if a custom image is used for the deployment.
	// additionalEnvVarsEnv = os.Getenv("ADDITIONAL_ENV_VARS")
	// additionalEnvVars    = make(map[string]string)

	// Seconds allowed for the shutdown process to complete.
	shutdownGracePeriodEnv = os.Getenv("SHUTDOWN_GRACE_PERIOD")
	shutdownGracePeriod    time.Duration

	// Time object used for the check.
	now time.Time

	ctx       context.Context
	ctxCancel context.CancelFunc

	// Interrupt signal channels.
	signalChan chan os.Signal
	// doneChan   chan bool

	debugEnv = os.Getenv("DEBUG")
	debug    bool

	// K8s clients used for the check.
	client     *kubernetes.Clientset
	nodeClient v1.NodeInterface
	podClient  v1.PodInterface
)

const (
	// Default check pod name prefix.
	defaultCheckName = "node-check"

	// Default container name.
	defaultCheckContainerName = "pause"

	// Default namespace for the check to run in.
	defaultCheckNamespace = "kuberhealthy"

	defaultCheckTimeLimit      = time.Duration(time.Minute * 15)
	defaultShutdownGracePeriod = time.Duration(time.Second * 30) // grace period for the check to shutdown after receiving a shutdown signal
)

func init() {
	// Parse incoming debug settings.
	parseDebugSettings()

	// Parse all incoming input environment variables and crash if an error occurs
	// during parsing process.
	parseInputValues()

	// Allocate channels.
	signalChan = make(chan os.Signal, 3)
	// doneChan = make(chan bool)
}

func main() {

	// Create a timestamp reference for the set of pods deployed and those that should not be deployed
	now = time.Now()
	log.Debugln("Allowing this check", checkTimeLimit, "to finish.")
	ctx, ctxCancel = context.WithTimeout(context.Background(), checkTimeLimit)

	// Create a kubernetes client.
	var err error
	client, err = kubeClient.Create(kubeConfigFile)
	if err != nil {
		errorMessage := "failed to create a kubernetes client with error: " + err.Error()
		reportErrorsToKuberhealthy([]string{errorMessage})
		return
	}
	log.Infoln("Kubernetes client created.")

	// Create node client.
	nodeClient = client.CoreV1().Nodes()

	// Create pod client.
	podClient = client.CoreV1().Pods(checkNamespace)

	// Start listening to interrupts.
	go listenForInterrupts(ctx, ctxCancel)

	// Catch panics.
	var r interface{}
	defer func() {
		r = recover()
		if r != nil {
			log.Infoln("Recovered panic:", r)
			reportToKuberhealthy(false, []string{r.(string)})
		}
	}()

	// Start node check.
	runNodeCheck(ctx)
}

// listenForInterrupts watches the signal and done channels for termination.
func listenForInterrupts(ctx context.Context, cancel context.CancelFunc) {

	// Relay incoming OS interrupt signals to the signalChan.
	signal.Notify(signalChan, os.Interrupt, os.Kill, syscall.SIGTERM, syscall.SIGINT)
	sig := <-signalChan // This is a blocking operation -- the routine will stop here until there is something sent down the channel.
	log.Infoln("Received an interrupt signal from the signal channel.")
	log.Debugln("Signal received was:", sig.String())

	log.Debugln("Cancelling context.")
	cancel()
	log.Debugln("Shutting down.")

	select {
	case sig = <-signalChan:
		// If there is an interrupt signal, interrupt the run.
		log.Warnln("Received a second interrupt signal from the signal channel.")
		log.Debugln("Signal received was:", sig.String())
	case err := <-cleanUpAndWait(ctx):
		// If the clean up is complete, exit.
		log.Infoln("Received a complete signal, clean up completed.")
		if err != nil {
			log.Errorln("failed to clean up check resources properly:", err.Error())
		}
	case <-time.After(time.Duration(shutdownGracePeriod)):
		// Exit if the clean up took to long to provide a response.
		log.Infoln("Clean up took too long to complete and timed out.")
	}

	os.Exit(0)
}

// cleanUpAndWait cleans up things and returns a signal down the returned channel when completed.
func cleanUpAndWait(ctx context.Context) chan error {

	// // Watch for the clean up process to complete.
	// doneChan := make(chan error)
	// go func() {
	// 	doneChan <- cleanUp(ctx)
	// }()

	// return doneChan
	return nil
}

// reportErrorsToKuberhealthy reports the specified errors for this check run.
func reportErrorsToKuberhealthy(errs []string) {
	log.Errorln("Reporting errors to Kuberhealthy:", errs)
	reportToKuberhealthy(false, errs)
}

// reportOKToKuberhealthy reports that there were no errors on this check run to Kuberhealthy.
func reportOKToKuberhealthy() {
	log.Infoln("Reporting success to Kuberhealthy.")
	reportToKuberhealthy(true, []string{})
}

// reportToKuberhealthy reports the check status to Kuberhealthy.
func reportToKuberhealthy(ok bool, errs []string) {
	var err error
	if ok {
		err = kh.ReportSuccess()
		if err != nil {
			log.Fatalln("error reporting to kuberhealthy:", err.Error())
		}
		return
	}
	err = kh.ReportFailure(errs)
	if err != nil {
		log.Fatalln("error reporting to kuberhealthy:", err.Error())
	}
}
