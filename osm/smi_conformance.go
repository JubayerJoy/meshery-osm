package osm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/layer5io/learn-layer5/smi-conformance/conformance"
	"github.com/layer5io/meshery-osm/meshes"
	"github.com/sirupsen/logrus"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConformanceResponse holds the response object of the test
type ConformanceResponse struct {
	Tests    string                       `json:"tests,omitempty"`
	Failures string                       `json:"failures,omitempty"`
	Results  []*SingleConformanceResponse `json:"results,omitempty"`
}

// Failure is the failure response object
type Failure struct {
	Text    string `json:"text,omitempty"`
	Message string `json:"message,omitempty"`
}

// SingleConformanceResponse holds the result of one particular test case
type SingleConformanceResponse struct {
	Name       string   `json:"name,omitempty"`
	Time       string   `json:"time,omitempty"`
	Assertions string   `json:"assertions,omitempty"`
	Failure    *Failure `json:"failure,omitempty"`
}

// installConformanceTool installs the smi conformance tool
func (iClient *Client) installConformanceTool(req *meshes.ApplyRuleRequest) error {
	Executable, err := exec.LookPath("./scripts/create_smi.sh")
	if err != nil {
		return err
	}

	cmd := &exec.Cmd{
		Path:   Executable,
		Args:   []string{Executable},
		Stdout: os.Stdout,
		Stderr: os.Stdout,
	}

	err = cmd.Start()
	if err != nil {
		return err
	}
	err = cmd.Wait()
	if err != nil {
		return err
	}

	iClient.eventChan <- &meshes.EventsResponse{
		OperationId: req.OperationId,
		EventType:   meshes.EventType_INFO,
		Summary:     "SMI tool installed successfully",
		Details:     " ",
	}

	logrus.Debugf("Waiting for resources to be created.......")
	time.Sleep(10 * time.Second) // Required for all the resources to be created

	return nil
}

// deleteConformanceTool deletes the smi conformance tool
func (iClient *Client) deleteConformanceTool(req *meshes.ApplyRuleRequest) error {
	Executable, err := exec.LookPath("./scripts/delete_smi.sh")
	if err != nil {
		return err
	}

	cmd := &exec.Cmd{
		Path:   Executable,
		Args:   []string{Executable},
		Stdout: os.Stdout,
		Stderr: os.Stdout,
	}

	err = cmd.Start()
	if err != nil {
		return err
	}
	err = cmd.Wait()
	if err != nil {
		return err
	}

	iClient.eventChan <- &meshes.EventsResponse{
		OperationId: req.OperationId,
		EventType:   meshes.EventType_INFO,
		Summary:     "SMI tool deleted successfully",
		Details:     " ",
	}

	return nil
}

// startConformanceTool initiates the connection
func (iClient *Client) startConformanceTool(ctx context.Context) error {
	svc, err := iClient.k8sClientset.CoreV1().Services("meshery").Get(ctx, "smi-conformance", metav1.GetOptions{})
	if err != nil {
		return errors.New("Unable to get service: " + err.Error())
	}
	iClient.smiAddress = fmt.Sprintf("%s:%d", svc.Status.LoadBalancer.Ingress[0].IP, svc.Spec.Ports[0].Port)
	return nil
}

// runConformanceTest runs the conformance test
func (iClient *Client) runConformanceTest(adaptorname string, arReq *meshes.ApplyRuleRequest) error {
	annotations := make(map[string]string, 0)
	labels := map[string]string{
		"openservicemesh.io/monitored-by": "osm",
	}
	// err := json.Unmarshal([]byte(arReq.CustomBody), &annotations)
	// if err != nil {
	// 	logrus.Error(err)
	// 	return errors.Wrapf(err, "Error unmarshaling annotation body.")
	// }

	cClient, err := conformance.CreateClient(context.TODO(), iClient.smiAddress)
	if err != nil {
		logrus.Error(err)
		iClient.eventChan <- &meshes.EventsResponse{
			OperationId: arReq.OperationId,
			EventType:   meshes.EventType_ERROR,
			Summary:     "Error creating a smi conformance tool client.",
			Details:     err.Error(),
		}
		return err
	}
	logrus.Debugf("created client for smi conformance tool: %s", adaptorname)

	result, err := cClient.CClient.RunTest(context.TODO(), &conformance.Request{
		Annotations: annotations,
		Labels:      labels,
		Meshname:    adaptorname,
	})
	if err != nil {
		logrus.Error(err)
		iClient.eventChan <- &meshes.EventsResponse{
			OperationId: arReq.OperationId,
			EventType:   meshes.EventType_ERROR,
			Summary:     "Test failed",
			Details:     err.Error(),
		}
		return err
	}
	logrus.Debugf("Tests ran successfully for smi conformance tool!!")

	response := ConformanceResponse{
		Tests:    result.Tests,
		Failures: result.Failures,
		Results:  make([]*SingleConformanceResponse, 0),
	}

	if result == nil {
		iClient.eventChan <- &meshes.EventsResponse{
			OperationId: arReq.OperationId,
			EventType:   meshes.EventType_ERROR,
			Summary:     "SMI tool connection crashed!",
			Details:     "Smi-conformance tool unreachable",
		}
		return err
	}

	for _, res := range result.SingleTestResult {
		response.Results = append(response.Results, &SingleConformanceResponse{
			Name:       res.Name,
			Time:       res.Time,
			Assertions: res.Assertions,
			Failure: &Failure{
				Text:    res.Failure.Test,
				Message: res.Failure.Message,
			},
		})
	}

	logrus.Debugf(fmt.Sprintf("Tests Results: %+v", response))
	jsondata, _ := json.Marshal(response)

	iClient.eventChan <- &meshes.EventsResponse{
		OperationId: arReq.OperationId,
		EventType:   meshes.EventType_INFO,
		Summary:     "Test completed successfully",
		Details:     fmt.Sprintf("Tests Results: %s\n", string(jsondata)),
	}

	return nil
}
