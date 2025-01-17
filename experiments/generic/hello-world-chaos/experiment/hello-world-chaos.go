package experiment

import (
	"os"

	"github.com/litmuschaos/chaos-operator/api/litmuschaos/v1alpha1"
	clients "github.com/litmuschaos/litmus-go/pkg/clients"
    litmusLIB "github.com/litmuschaos/litmus-go/chaoslib/litmus/hello-world-chaos/lib"
	"github.com/litmuschaos/litmus-go/pkg/events"
	"github.com/litmuschaos/litmus-go/pkg/log"
	experimentEnv "github.com/litmuschaos/litmus-go/pkg/generic/hello-world-chaos/environment"
	experimentTypes "github.com/litmuschaos/litmus-go/pkg/generic/hello-world-chaos/types"
	"github.com/litmuschaos/litmus-go/pkg/probe"
	"github.com/litmuschaos/litmus-go/pkg/result"
	"github.com/litmuschaos/litmus-go/pkg/status"
	"github.com/litmuschaos/litmus-go/pkg/types"
	"github.com/litmuschaos/litmus-go/pkg/utils/common"
	"github.com/sirupsen/logrus"
)

// Experiment contains steps to inject chaos
func Experiment(clients clients.ClientSets){

	experimentsDetails := experimentTypes.ExperimentDetails{}
	resultDetails := types.ResultDetails{}
	eventsDetails := types.EventDetails{}
	chaosDetails := types.ChaosDetails{}
	
	//Fetching all the ENV passed from the runner pod
	log.Infof("[PreReq]: Getting the ENV for the %v experiment", os.Getenv("EXPERIMENT_NAME"))
	experimentEnv.GetENV(&experimentsDetails)

	// Initialize the chaos attributes
	types.InitialiseChaosVariables(&chaosDetails)
	
	// Initialize Chaos Result Parameters
	types.SetResultAttributes(&resultDetails, chaosDetails)

	if experimentsDetails.EngineName != "" {
		// Initialize the probe details. Bail out upon error, as we haven't entered exp business logic yet
		if err := probe.InitializeProbesInChaosResultDetails(&chaosDetails, clients, &resultDetails); err != nil {
			log.Errorf("Unable to initialize the probes, err: %v", err)
			return
		}
	}

	//Updating the chaos result in the beginning of experiment
	log.Infof("[PreReq]: Updating the chaos result of %v experiment (SOT)", experimentsDetails.ExperimentName)
	if err := result.ChaosResult(&chaosDetails, clients, &resultDetails, "SOT");err != nil {
		log.Errorf("Unable to Create the Chaos Result, err: %v", err)
		result.RecordAfterFailure(&chaosDetails, &resultDetails, err, clients, &eventsDetails)
		return
	}

	// Set the chaos result uid
	result.SetResultUID(&resultDetails, clients, &chaosDetails)

	// generating the event in chaosresult to marked the verdict as awaited
	msg := "experiment: " + experimentsDetails.ExperimentName + ", Result: Awaited"
	types.SetResultEventAttributes(&eventsDetails, types.AwaitedVerdict, msg, "Normal", &resultDetails)
	events.GenerateEvents(&eventsDetails, clients, &chaosDetails, "ChaosResult")

	//DISPLAY THE APP INFORMATION
	log.InfoWithValues("[Info]: The application information is as follows", logrus.Fields{
		"Namespace": experimentsDetails.AppNS,
		"Label":     experimentsDetails.AppLabel,
		"Chaos Duration":    experimentsDetails.ChaosDuration,
	})

	// Calling AbortWatcher go routine, it will continuously watch for the abort signal and generate the required events and result
	go common.AbortWatcher(experimentsDetails.ExperimentName, clients, &resultDetails, &chaosDetails, &eventsDetails)

    // @TODO: user PRE-CHAOS-CHECK
    // ADD A PRE-CHAOS CHECK OF YOUR CHOICE HERE
    // POD STATUS CHECKS FOR THE APPLICATION UNDER TEST AND AUXILIARY APPLICATIONS ARE ADDED BY DEFAULT 

	//PRE-CHAOS APPLICATION STATUS CHECK
	if chaosDetails.DefaultHealthCheck {
		log.Info("[Status]: Verify that the AUT (Application Under Test) is running (pre-chaos)")
		if err := status.AUTStatusCheck(clients, &chaosDetails); err != nil {
                log.Errorf("Application status check failed, err: %v", err)
        		types.SetEngineEventAttributes(&eventsDetails, types.PreChaosCheck, "AUT: Not Running", "Warning", &chaosDetails)
        		events.GenerateEvents(&eventsDetails, clients, &chaosDetails, "ChaosEngine")
        		result.RecordAfterFailure(&chaosDetails, &resultDetails, err, clients, &eventsDetails)
        		return
        }
	}


	if experimentsDetails.EngineName != "" {
		// marking AUT as running, as we already checked the status of application under test
		msg := "AUT: Running"

		// run the probes in the pre-chaos check
		if len(resultDetails.ProbeDetails) != 0 {

			if err := probe.RunProbes(&chaosDetails, clients, &resultDetails, "PreChaos", &eventsDetails);err != nil {
				log.Errorf("Probe Failed, err: %v", err)
				msg := "AUT: Running, Probes: Unsuccessful"
				types.SetEngineEventAttributes(&eventsDetails, types.PreChaosCheck, msg, "Warning", &chaosDetails)
				events.GenerateEvents(&eventsDetails, clients, &chaosDetails, "ChaosEngine")
				result.RecordAfterFailure(&chaosDetails, &resultDetails, err, clients, &eventsDetails)
				return
			}
			msg = "AUT: Running, Probes: Successful"
		}
		// generating the events for the pre-chaos check
		types.SetEngineEventAttributes(&eventsDetails, types.PreChaosCheck, msg, "Normal", &chaosDetails)
		events.GenerateEvents(&eventsDetails, clients, &chaosDetails, "ChaosEngine")
	}

    // INVOKE THE CHAOSLIB OF YOUR CHOICE HERE, WHICH WILL CONTAIN 
	// THE BUSINESS LOGIC OF THE ACTUAL CHAOS
    // IT CAN BE A NEW CHAOSLIB YOU HAVE CREATED SPECIALLY FOR THIS EXPERIMENT OR ANY EXISTING ONE 
    // @TODO: user INVOKE-CHAOSLIB
   
	chaosDetails.Phase = types.ChaosInjectPhase
    if err := litmusLIB.PrepareChaos(&experimentsDetails, clients, &resultDetails, &eventsDetails, &chaosDetails); err != nil {
        log.Errorf("Chaos injection failed, err: %v", err)
        result.RecordAfterFailure(&chaosDetails, &resultDetails, err, clients, &eventsDetails)
        return
    }

	log.Infof("[Confirmation]: %v chaos has been injected successfully", experimentsDetails.ExperimentName)
	resultDetails.Verdict = v1alpha1.ResultVerdictPassed
	chaosDetails.Phase = types.PostChaosPhase
	
    // @TODO: user POST-CHAOS-CHECK
	// ADD A POST-CHAOS CHECK OF YOUR CHOICE HERE
    // POD STATUS CHECKS FOR THE APPLICATION UNDER TEST AND AUXILIARY APPLICATIONS ARE ADDED BY DEFAULT 

	//POST-CHAOS APPLICATION STATUS CHECK
	if chaosDetails.DefaultHealthCheck {
		log.Info("[Status]: Verify that the AUT (Application Under Test) is running (post-chaos)")
		if err := status.AUTStatusCheck(clients, &chaosDetails); err != nil {
			log.Errorf("Application status check failed, err: %v", err)
			types.SetEngineEventAttributes(&eventsDetails, types.PostChaosCheck, "AUT: Not Running", "Warning", &chaosDetails)
			events.GenerateEvents(&eventsDetails, clients, &chaosDetails, "ChaosEngine")
			result.RecordAfterFailure(&chaosDetails, &resultDetails, err, clients, &eventsDetails)
			return
		}
	}


	if experimentsDetails.EngineName != "" {
		// marking AUT as running, as we already checked the status of application under test
		msg := "AUT: Running"

		// run the probes in the post-chaos check
		if len(resultDetails.ProbeDetails) != 0 {
			if err := probe.RunProbes(&chaosDetails, clients, &resultDetails, "PostChaos", &eventsDetails);err != nil {
				log.Errorf("Probes Failed, err: %v", err)
				msg := "AUT: Running, Probes: Unsuccessful"
				types.SetEngineEventAttributes(&eventsDetails, types.PostChaosCheck, msg, "Warning", &chaosDetails)
				events.GenerateEvents(&eventsDetails, clients, &chaosDetails, "ChaosEngine")
				result.RecordAfterFailure(&chaosDetails, &resultDetails, err, clients, &eventsDetails)
				return
			}
			msg = "AUT: Running, Probes: Successful"
		}

		// generating post chaos event
		types.SetEngineEventAttributes(&eventsDetails, types.PostChaosCheck, msg, "Normal", &chaosDetails)
		events.GenerateEvents(&eventsDetails, clients, &chaosDetails, "ChaosEngine")
	}


	//Updating the chaosResult in the end of experiment
	log.Infof("[The End]: Updating the chaos result of %v experiment (EOT)", experimentsDetails.ExperimentName)
	if err := result.ChaosResult(&chaosDetails, clients, &resultDetails, "EOT");err != nil {
		log.Errorf("Unable to Update the Chaos Result, err: %v", err)
		result.RecordAfterFailure(&chaosDetails, &resultDetails, err, clients, &eventsDetails)
		return
	}

	// generating the event in chaosresult to marked the verdict as pass/fail
	msg = "experiment: " + experimentsDetails.ExperimentName + ", Result: " + string(resultDetails.Verdict)
	reason := types.PassVerdict
	eventType := "Normal"
	if resultDetails.Verdict != "Pass" {
		reason = types.FailVerdict
		eventType = "Warning"
	}
	types.SetResultEventAttributes(&eventsDetails, reason, msg, eventType, &resultDetails)
	events.GenerateEvents(&eventsDetails, clients, &chaosDetails, "ChaosResult")

	if experimentsDetails.EngineName != "" {
		msg := experimentsDetails.ExperimentName + " experiment has been " + string(resultDetails.Verdict) + "ed"
		types.SetEngineEventAttributes(&eventsDetails, types.Summary, msg, "Normal", &chaosDetails)
		events.GenerateEvents(&eventsDetails, clients, &chaosDetails, "ChaosEngine")
	}
}