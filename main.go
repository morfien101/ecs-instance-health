package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/morfien101/ecs-instance-health/ec2metadatareader"
	"github.com/morfien101/ecs-instance-health/ecsmanager"
)

var (
	version = "0.0.1"

	helpBlurb = `
	Use this tool to determine if an EC2 instance is 'Active' in a ECS cluster.
	It can also be used to DRAIN an ECS member instance and wait will all tasks
	have been removed from the instance.

	Only a single action can be invoked in a single run.
	It will consume credentials from instance policies or ENV vars.
	There is no provision for manually feeding in credentials and never will be.
`

	versionFlag = flag.Bool("v", false, "Show the version")
	helpFlag    = flag.Bool("h", false, "Show the help menu")
	verboseFlag = flag.Bool("verbose", false, "Will log success statements as well as errors")

	instanceIDFlag                     = flag.String("i", "", "instance_id for the EC2 instance. If - is passed the instance ID is determined automatically from the metadata if available")
	clusterNameFlag                    = flag.String("c", "", "Name of the ECS cluster")
	drainingFlag                       = flag.Bool("drain", false, "Set the instance to DRAINING in it's ECS Cluster")
	isActiveFlag                       = flag.Bool("is-active", false, "Checks to see if the instance is 'Active' in it's ECS Cluster")
	waitFlag                           = flag.Bool("wait", false, "Use with -drain. This flag will cause the app to wait till there are 0 tasks running")
	maxWaitTimeFlag                    = flag.Uint("wait-timeout", 600, "Maximum wait time in seconds for draining to complete")
	checkIntervalFlag                  = flag.Uint("check-interval", 10, "Wait time in seconds between checks for running tasks")
	clusterInstanceIDCacheFilePathFlag = flag.String("cache-path", "ecs-instance-health-cache.cache", "Path to the cache file used to save the cluster instance id")
)

func main() {
	digestFlags()
	instanceID := ""
	if *instanceIDFlag == "-" {
		localInstanceID, err := ec2metadatareader.InstanceID()
		if err != nil {
			writeToStdErr(fmt.Sprintf("Could not determine instance id. Error: %s", err))
			os.Exit(1)
		}
		instanceID = localInstanceID
	} else {
		instanceID = *instanceIDFlag
	}

	ecsmanager.SetCachePath(*clusterInstanceIDCacheFilePathFlag)

	if *isActiveFlag {
		ok, state, err := ecsmanager.IsActive(*clusterNameFlag, instanceID)
		if err != nil {
			writeToStdErr(err.Error())
			os.Exit(1)
		}
		msg := fmt.Sprintf("instance %s is in state '%s'", instanceID, state)
		if !ok {
			writeToStdErr(msg)
			os.Exit(1)
		}
		verboseLog(msg)
		return
	} else if *drainingFlag {
		err := ecsmanager.DrainInstance(*clusterNameFlag, instanceID, *waitFlag, *checkIntervalFlag, *maxWaitTimeFlag)
		if err != nil {
			if _, ok := err.(ecsmanager.TimeoutError); ok {
				writeToStdErr(err.Error())
				return
			}
			writeToStdErr(err.Error())
			os.Exit(1)
		}
		return
	} else {
		fmt.Println("No action specified.")
		os.Exit(1)
	}

}

func digestFlags() {
	flag.Parse()
	// These 2 functions have the ability to exit the app
	showStopperFlags()
	validateActions()
}

func showStopperFlags() {
	if *helpFlag {
		fmt.Println(helpBlurb)
		flag.PrintDefaults()
		os.Exit(0)
	}

	if *versionFlag {
		fmt.Println(version)
		os.Exit(0)
	}
}

func validateActions() {
	errors := []string{}
	if err := validateRequiredVars(); err != nil {
		errors = append(errors, err.Error())
	}

	if *drainingFlag && *isActiveFlag {
		errors = append(errors, "-drain and -is-active can not be used together")
	}

	if len(errors) != 0 {
		fmt.Println(strings.Join(errors, "\n"))
		os.Exit(1)
	}
}

func validateRequiredVars() error {
	errors := []string{}
	if *drainingFlag || *isActiveFlag {
		if *instanceIDFlag == "" {
			errors = append(errors, "-i instance_id must be specified")
		}

		if *clusterNameFlag == "" {
			errors = append(errors, "-cluster must be supplied")
		}
	}

	if len(errors) != 0 {
		return fmt.Errorf("%s", strings.Join(errors, ","))
	}
	return nil
}

func writeToStdErr(s string) {
	fmt.Fprintln(os.Stderr, s)
}

func verboseLog(s string) {
	if *verboseFlag {
		fmt.Println(s)
	}
}
