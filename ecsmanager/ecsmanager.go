package ecsmanager

import (
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ecs"
	"github.com/morfien101/ecs-instance-health/ec2metadatareader"
)

var (
	cacheFilePath = ""
)

func SetCachePath(path string) {
	cacheFilePath = path
}

func awsSession() (*session.Session, error) {
	region, ok := os.LookupEnv("AWS_REGION")
	if !ok {
		guess, err := ec2metadatareader.Region()
		if err != nil {
			return nil, err
		}
		region = guess
	}
	return session.NewSession(&aws.Config{Region: aws.String(region)})
}

func ecsSession(session *session.Session) *ecs.ECS {
	return ecs.New(session)
}

func newAWSSession() (*ecs.ECS, error) {
	basicSession, err := awsSession()
	if err != nil {
		return nil, err
	}
	ecsSession := ecsSession(basicSession)
	return ecsSession, nil
}

func getECSInstanceID(ecsSession *ecs.ECS, clusterName, instanceID string) (string, error) {
	makeFile := func() (string, error) {
		id, err := readInstanceIDFromAPI(ecsSession, clusterName, instanceID)
		if err != nil {
			return "", err
		}
		err = ioutil.WriteFile(cacheFilePath, []byte(id), 0640)
		if err != nil {
			return id, err
		}
		return id, nil
	}

	// Is the file there? If not make it
	_, err := os.Stat(cacheFilePath)
	if err != nil {
		return makeFile()
	}

	// File is there but can we read from it?
	bytes, err := ioutil.ReadFile(cacheFilePath)
	if err != nil {
		return makeFile()
	}
	return string(bytes), nil

}

func readInstanceIDFromAPI(ecsSession *ecs.ECS, clusterName, instanceID string) (string, error) {
	input := &ecs.ListContainerInstancesInput{
		Filter:  aws.String(fmt.Sprintf("attribute:EC2_INSTANCE_ID==%s", instanceID)),
		Cluster: aws.String(clusterName),
	}
	output, err := ecsSession.ListContainerInstances(input)
	if err != nil {
		return "", err
	}
	if len(output.ContainerInstanceArns) == 0 {
		return "", fmt.Errorf("No container instances found")
	}
	arnSplit := strings.Split(*output.ContainerInstanceArns[0], ":")
	instanceClusterID := strings.Split(arnSplit[len(arnSplit)-1], "/")
	return instanceClusterID[1], nil
}

func getCurrentState(ecsSession *ecs.ECS, cluster, instanceID string) (string, error) {

	output, err := ecsSession.DescribeContainerInstances(&ecs.DescribeContainerInstancesInput{
		Cluster: aws.String(cluster),
		ContainerInstances: []*string{
			aws.String(instanceID),
		},
	})
	if err != nil {
		return "", err
	}
	if len(output.ContainerInstances) == 0 {
		return "", fmt.Errorf("No container instances found")
	}

	return *output.ContainerInstances[0].Status, nil
}

func IsActive(cluster, ec2InstanceID string) (bool, string, error) {
	ecsSession, err := newAWSSession()
	if err != nil {
		return false, "", err
	}
	instanceClusterID, err := getECSInstanceID(ecsSession, cluster, ec2InstanceID)
	if err != nil {
		return false, "", err
	}

	currentState, err := getCurrentState(ecsSession, cluster, instanceClusterID)
	if err != nil {
		return false, "", err
	}

	if currentState != "ACTIVE" {
		return false, currentState, nil
	}
	return true, currentState, nil
}

func DrainInstance(cluster, ec2InstanceID string, wait bool, pollTime uint, timeout uint) error {
	ecsSession, err := newAWSSession()
	if err != nil {
		return err
	}
	instanceClusterID, err := getECSInstanceID(ecsSession, cluster, ec2InstanceID)
	if err != nil {
		return err
	}
	err = setDraining(ecsSession, cluster, instanceClusterID)
	if err != nil {
		return err
	}
	fmt.Printf("Instance %s has been set to DRAINING\n", ec2InstanceID)
	if wait {
		err := waitForTaskToDrain(ecsSession, cluster, ec2InstanceID, instanceClusterID, pollTime, timeout)
		if err != nil {
			return err
		}
	}

	return nil
}

func setDraining(ecsSession *ecs.ECS, cluster, instanceClusterID string) error {

	currentState, err := getCurrentState(ecsSession, cluster, instanceClusterID)
	if err != nil {
		return err
	}

	// If it is already draining we don't need to do anything.
	if currentState != "DRAINING" {
		_, err = ecsSession.UpdateContainerInstancesState(&ecs.UpdateContainerInstancesStateInput{
			Cluster: aws.String(cluster),
			Status:  aws.String("DRAINING"),
			ContainerInstances: []*string{
				aws.String(instanceClusterID),
			},
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func listRunningTasks(ecsSession *ecs.ECS, cluster, clusterInstanceID string) (int64, error) {
	output, err := ecsSession.DescribeContainerInstances(&ecs.DescribeContainerInstancesInput{
		Cluster: aws.String(cluster),
		ContainerInstances: []*string{
			aws.String(clusterInstanceID),
		},
	})
	if err != nil {
		return 0, err
	}
	if len(output.ContainerInstances) == 0 {
		return 0, fmt.Errorf("No container instance found")
	}
	return *output.ContainerInstances[0].RunningTasksCount, nil
}

// TimeoutError signals that the error relates to a timeout and is not strictly a breaking error.
type TimeoutError error

func waitForTaskToDrain(ecsSession *ecs.ECS, cluster, ec2InstanceID, clusterInstanceID string, pollTime uint, timeout uint) error {
	pollTicker := time.NewTicker(time.Second * time.Duration(int64(pollTime)))
	var timeoutTicker *time.Ticker
	if timeout == 0 {
		timeoutTicker = &time.Ticker{
			C: make(<-chan time.Time, 1),
		}
	} else {
		timeoutTicker = time.NewTicker(time.Second * time.Duration(int64(timeout)))
	}

	// Shutdown tickers at the end of the run
	defer func() {
		pollTicker.Stop()
		timeoutTicker.Stop()
	}()

	stillRunningTasks := func() (bool, error) {
		runningTasks, err := listRunningTasks(ecsSession, cluster, clusterInstanceID)
		if err != nil {
			return true, err
		}
		if runningTasks != 0 {
			fmt.Printf("Instance %s still has %d tasks running\n", ec2InstanceID, runningTasks)
			return false, nil
		}
		fmt.Printf("Instance %s has %d tasks running\n", ec2InstanceID, runningTasks)
		return true, nil
	}

	// Run here once to see if we need to start the loop for waiting
	done, err := stillRunningTasks()
	if err != nil {
		return err
	}
	if done {
		return nil
	}

	//Wait for the timeout or for 0 containers.
	for {
		select {
		case <-timeoutTicker.C:
			return TimeoutError(fmt.Errorf("Timeout reached waiting for tasks to drain"))
		case <-pollTicker.C:
			done, err := stillRunningTasks()
			if err != nil {
				return err
			}
			if done {
				return nil
			}
			continue
		}
	}
}
