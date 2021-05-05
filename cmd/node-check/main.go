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

	// The image that the deployment will use
	checkImageEnv = os.Getenv("CHECK_IMAGE")
	checkImage    string

	// Node selectors for the deployment check
	checkNodeSelectorsEnv = os.Getenv("NODE_SELECTOR")

	// Pod time to live (how long to wait after a pod is running before deleting it). [In seconds]
	podTTLSecondsEnv = os.Getenv("CHECK_TTL_SECONDS")
	podTTLSeconds    time.Duration

	// Pod resource requests and limits.
	millicoreRequestEnv = os.Getenv("CHECK_POD_CPU_REQUEST")
	millicoreRequest    int

	millicoreLimitEnv = os.Getenv("CHECK_POD_CPU_LIMIT")
	millicoreLimit    int

	memoryRequestEnv = os.Getenv("CHECK_POD_MEM_REQUEST")
	memoryRequest    int

	memoryLimitEnv = os.Getenv("CHECK_POD_MEM_LIMIT")
	memoryLimit    int

	// Boolean flag to determine whether the check runs concurrently
	concurrentEnv = os.Getenv("CONCURRENT")
	concurrent    = true

	// Check time limit.
	checkTimeLimit time.Duration

	// Check workers interlude maximum in seconds.
	// Used to prevent DDOSing your own cluster.
	workerInterludeEnv = os.Getenv("CHECK_WORKER_INTERLUDE")
	workerInterlude    int64

	// Seconds allowed for the shutdown process to complete.
	shutdownGracePeriodEnv = os.Getenv("SHUTDOWN_GRACE_PERIOD")
	shutdownGracePeriod    time.Duration

	// Time object used for the check.
	now time.Time

	ctx       context.Context
	ctxCancel context.CancelFunc

	// Interrupt signal channels.
	signalChan chan os.Signal

	debugEnv = os.Getenv("DEBUG")
	debug    bool

	// K8s clients used for the check.
	client     *kubernetes.Clientset
	nodeClient v1.NodeInterface
	podClient  v1.PodInterface
)

const (
	// Default check pod name prefix.
	defaultCheckName = "node"

	// Default check pod labels.
	defaultCheckLabelKey       = "node-timestamp"
	defaultCheckLabelValueBase = "unix-"

	// Default namespace for the check to run in.
	defaultCheckNamespace = "kuberhealthy"

	// Default container resource requests values.
	defaultMillicoreRequest = 30               // Calculated in decimal SI units (15 = 15m cpu).
	defaultMillicoreLimit   = 50               // Calculated in decimal SI units (75 = 75m cpu).
	defaultMemoryRequest    = 10 * 1024 * 1024 // Calculated in binary SI units (20 * 1024^2 = 20Mi memory).
	defaultMemoryLimit      = 30 * 1024 * 1024 // Calculated in binary SI units (75 * 1024^2 = 75Mi memory).

	// Default check pod image.
	defaultCheckImage = "gcr.io/google-containers/pause:3.1"

	// Default times.
	defaultWorkerInterlude     = 1
	defaultPodTTLSeconds       = time.Duration(time.Second * 5)
	defaultCheckTimeLimit      = time.Duration(time.Hour)
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
	sig := <-signalChan
	log.Infoln("Received an interrupt signal from the signal channel.")
	log.Debugln("Signal received was:", sig.String())

	log.Debugln("Cancelling context.")
	cancel()

	select {
	case sig = <-signalChan:
		// If there is an interrupt signal, interrupt the run.
		log.Warnln("Received a second interrupt signal from the signal channel.")
		log.Debugln("Signal received was:", sig.String())
	case <-time.After(time.Duration(shutdownGracePeriod)):
		// Exit if the clean up took to long to provide a response.
		log.Infoln("Clean up took too long to complete and timed out.")
	}

	os.Exit(0)
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
